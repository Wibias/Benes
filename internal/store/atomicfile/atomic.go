package atomicfile

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"time"
)

var ErrTooLarge = errors.New("file exceeds configured byte limit")

var sequence atomic.Uint64

type Options struct {
	Mode   os.FileMode
	Harden func(string) error
}

type ResidualError struct {
	TempPath string
	Cause    error
}

func (e *ResidualError) Error() string {
	return fmt.Sprintf("atomic write left a scrubbed temporary file at %s: %v", e.TempPath, e.Cause)
}

func (e *ResidualError) Unwrap() error { return e.Cause }

type SecretResidualError struct {
	TempPath string
	Cause    error
}

func (e *SecretResidualError) Error() string {
	return fmt.Sprintf("atomic write could not scrub secret-bearing temporary file at %s: %v", e.TempPath, e.Cause)
}

func (e *SecretResidualError) Unwrap() error { return e.Cause }

type DurabilityError struct {
	Path  string
	Cause error
}

func (e *DurabilityError) Error() string {
	return fmt.Sprintf("atomic write published %s but durability sync failed: %v", e.Path, e.Cause)
}

func (e *DurabilityError) Unwrap() error { return e.Cause }

type ops struct {
	openTemp     func(string, int, os.FileMode) (*os.File, error)
	rename       func(string, string) error
	remove       func(string) error
	openDir      func(string) (*os.File, error)
	lstat        func(string) (os.FileInfo, error)
	evalSymlinks func(string) (string, error)
}

var defaultOps = ops{
	openTemp:     os.OpenFile,
	rename:       os.Rename,
	remove:       os.Remove,
	openDir:      os.Open,
	lstat:        os.Lstat,
	evalSymlinks: filepath.EvalSymlinks,
}

func Write(path string, content []byte, options Options) error {
	return writeWithOps(path, content, options, defaultOps)
}

func writeWithOps(path string, content []byte, options Options, ioOps ops) error {
	mode := options.Mode
	if mode == 0 {
		mode = 0o600
	}
	target, err := resolveWriteTarget(path, ioOps)
	if err != nil {
		return err
	}
	parent := filepath.Dir(target)
	temp := fmt.Sprintf("%s.benes.%d.%d.tmp", target, os.Getpid(), sequence.Add(1))

	file, err := ioOps.openTemp(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return fmt.Errorf("create atomic temp: %w", err)
	}
	published := false
	cleanup := func(cause error) error {
		_ = file.Close()
		scrubErr := os.Truncate(temp, 0)
		removeErr := retryRemove(temp, ioOps.remove)
		if removeErr == nil || errors.Is(removeErr, fs.ErrNotExist) {
			return cause
		}
		if scrubErr != nil && !errors.Is(scrubErr, fs.ErrNotExist) {
			return &SecretResidualError{TempPath: temp, Cause: cause}
		}
		return &ResidualError{TempPath: temp, Cause: cause}
	}
	defer func() {
		if !published {
			_ = file.Close()
		}
	}()

	if err := file.Chmod(mode); err != nil {
		return cleanup(fmt.Errorf("set atomic temp mode: %w", err))
	}
	if _, err := file.Write(content); err != nil {
		return cleanup(fmt.Errorf("write atomic temp: %w", err))
	}
	if err := file.Sync(); err != nil {
		return cleanup(fmt.Errorf("sync atomic temp: %w", err))
	}
	if options.Harden != nil {
		if err := options.Harden(temp); err != nil {
			return cleanup(fmt.Errorf("harden atomic temp: %w", err))
		}
	}
	if err := file.Close(); err != nil {
		return cleanup(fmt.Errorf("close atomic temp: %w", err))
	}
	if err := retryRename(temp, target, ioOps.rename); err != nil {
		return cleanup(fmt.Errorf("publish atomic temp: %w", err))
	}
	published = true
	if runtime.GOOS != "windows" {
		dir, err := ioOps.openDir(parent)
		if err != nil {
			return &DurabilityError{Path: target, Cause: fmt.Errorf("open parent directory: %w", err)}
		}
		syncErr := dir.Sync()
		closeErr := dir.Close()
		if syncErr != nil {
			return &DurabilityError{Path: target, Cause: fmt.Errorf("sync parent directory: %w", syncErr)}
		}
		if closeErr != nil {
			return &DurabilityError{Path: target, Cause: fmt.Errorf("close parent directory: %w", closeErr)}
		}
	}
	return nil
}

func resolveWriteTarget(path string, ioOps ops) (string, error) {
	target, err := ioOps.evalSymlinks(path)
	if err == nil {
		return target, nil
	}
	info, statErr := ioOps.lstat(path)
	if errors.Is(statErr, fs.ErrNotExist) {
		return path, nil
	}
	if statErr != nil {
		return "", fmt.Errorf("inspect atomic write target %q: %w", path, statErr)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("refusing unresolvable symlinked write target %q: %w", path, err)
	}
	return path, nil
}

func retryRename(oldPath, newPath string, rename func(string, string) error) error {
	var err error
	attempts := 1
	if runtime.GOOS == "windows" {
		attempts = 3
	}
	for attempt := 0; attempt < attempts; attempt++ {
		err = rename(oldPath, newPath)
		if err == nil {
			return nil
		}
		if runtime.GOOS != "windows" || attempt == attempts-1 {
			break
		}
		time.Sleep(time.Duration(attempt+1) * 25 * time.Millisecond)
	}
	return err
}

func retryRemove(path string, remove func(string) error) error {
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		err = remove(path)
		if err == nil || errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return err
}

func ReadBounded(path string, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		return nil, fmt.Errorf("maxBytes must be positive")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	limited := io.LimitReader(file, maxBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, ErrTooLarge
	}
	return data, nil
}
