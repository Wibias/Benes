package continuation

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/Wibias/Benes/internal/store/atomicfile"
)

var (
	ErrInvalidInstallationSalt = errors.New("installation replay salt must be exactly 32 bytes")
	ErrPersisterClosed         = errors.New("continuation persister is closed")
)

const installationSaltBytes = 32

var installationSaltLocks sync.Map

func LoadOrCreateInstallationSalt(path string) ([]byte, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("installation salt path is required")
	}
	cleaned, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("normalize installation salt path: %w", err)
	}
	lockValue, _ := installationSaltLocks.LoadOrStore(cleaned, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()

	if value, err := readInstallationSalt(cleaned); err == nil {
		return value, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	parent := filepath.Dir(cleaned)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return nil, fmt.Errorf("create installation salt directory: %w", err)
	}
	value := make([]byte, installationSaltBytes)
	if _, err := io.ReadFull(rand.Reader, value); err != nil {
		return nil, fmt.Errorf("generate installation salt: %w", err)
	}

	file, err := os.OpenFile(cleaned, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return waitForInstallationSalt(cleaned)
	}
	if err != nil {
		return nil, fmt.Errorf("create installation salt: %w", err)
	}
	created := true
	cleanup := func() {
		_ = file.Close()
		if created {
			_ = os.Remove(cleaned)
		}
	}
	if err := file.Chmod(0o600); err != nil {
		cleanup()
		return nil, fmt.Errorf("harden installation salt: %w", err)
	}
	if _, err := file.Write(value); err != nil {
		cleanup()
		return nil, fmt.Errorf("write installation salt: %w", err)
	}
	if err := file.Sync(); err != nil {
		cleanup()
		return nil, fmt.Errorf("sync installation salt: %w", err)
	}
	if err := file.Close(); err != nil {
		created = false
		return nil, fmt.Errorf("close installation salt: %w", err)
	}
	created = false
	if runtime.GOOS != "windows" {
		dir, err := os.Open(parent)
		if err != nil {
			return nil, fmt.Errorf("open installation salt directory: %w", err)
		}
		syncErr := dir.Sync()
		closeErr := dir.Close()
		if syncErr != nil {
			return nil, fmt.Errorf("sync installation salt directory: %w", syncErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close installation salt directory: %w", closeErr)
		}
	}
	return append([]byte(nil), value...), nil
}

func readInstallationSalt(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("refusing symlinked installation salt")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		if err := os.Chmod(path, 0o600); err != nil {
			return nil, fmt.Errorf("harden existing installation salt: %w", err)
		}
	}
	value, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(value) != installationSaltBytes {
		return nil, ErrInvalidInstallationSalt
	}
	return append([]byte(nil), value...), nil
}

func waitForInstallationSalt(path string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < 50; attempt++ {
		value, err := readInstallationSalt(path)
		if err == nil {
			return value, nil
		}
		lastErr = err
		if !errors.Is(err, ErrInvalidInstallationSalt) && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		time.Sleep(10 * time.Millisecond)
	}
	return nil, fmt.Errorf("installation salt did not become readable: %w", lastErr)
}

func LoadSnapshot(path string, store *Store, maxBytes int64) error {
	if store == nil {
		return fmt.Errorf("continuation store is required")
	}
	if maxBytes <= 0 {
		maxBytes = 64 << 20
	}
	data, err := atomicfile.ReadBounded(path, maxBytes)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read continuation snapshot: %w", err)
	}
	if err := store.Restore(data); err != nil {
		if errors.Is(err, ErrSnapshotVersion) {
			removeErr := os.Remove(path)
			if removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				return fmt.Errorf("invalidate legacy continuation snapshot: %v: %w", removeErr, err)
			}
		}
		return err
	}
	return nil
}

type persistWriter func(path string, content []byte) error

type Persister struct {
	store *Store
	path  string
	write persistWriter

	mu        sync.Mutex
	requested uint64
	completed uint64
	pending   []byte
	lastErr   error
	closed    bool
	changed   chan struct{}
	wake      chan struct{}
	stop      chan struct{}
	done      chan struct{}
	stopOnce  sync.Once
}

func NewPersister(store *Store, path string) *Persister {
	return newPersister(store, path, func(path string, content []byte) error {
		parent := filepath.Dir(path)
		if err := os.MkdirAll(parent, 0o700); err != nil {
			return fmt.Errorf("create continuation snapshot directory: %w", err)
		}
		return atomicfile.Write(path, content, atomicfile.Options{Mode: 0o600})
	})
}

func newPersister(store *Store, path string, writer persistWriter) *Persister {
	p := &Persister{
		store:   store,
		path:    path,
		write:   writer,
		changed: make(chan struct{}),
		wake:    make(chan struct{}, 1),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
	go p.run()
	return p
}

func (p *Persister) Queue() error {
	if p == nil || p.store == nil || p.write == nil || strings.TrimSpace(p.path) == "" {
		return fmt.Errorf("continuation persister is not configured")
	}
	snapshot, err := p.store.Snapshot()
	if err != nil {
		return err
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return ErrPersisterClosed
	}
	p.requested++
	p.pending = append(p.pending[:0], snapshot...)
	p.mu.Unlock()
	select {
	case p.wake <- struct{}{}:
	default:
	}
	return nil
}

func (p *Persister) Flush(ctx context.Context) error {
	if p == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	p.mu.Lock()
	target := p.requested
	p.mu.Unlock()
	return p.flushTarget(ctx, target)
}

func (p *Persister) Close(ctx context.Context) error {
	if p == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	p.mu.Lock()
	p.closed = true
	target := p.requested
	p.mu.Unlock()
	if err := p.flushTarget(ctx, target); err != nil {
		return err
	}
	p.stopOnce.Do(func() { close(p.stop) })
	select {
	case <-p.done:
		p.mu.Lock()
		err := p.lastErr
		p.mu.Unlock()
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *Persister) flushTarget(ctx context.Context, target uint64) error {
	for {
		p.mu.Lock()
		if p.completed >= target {
			err := p.lastErr
			p.mu.Unlock()
			return err
		}
		changed := p.changed
		p.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (p *Persister) run() {
	defer close(p.done)
	for {
		select {
		case <-p.wake:
			p.drain()
		case <-p.stop:
			return
		}
	}
}

func (p *Persister) drain() {
	for {
		p.mu.Lock()
		if p.completed >= p.requested {
			p.mu.Unlock()
			return
		}
		target := p.requested
		content := append([]byte(nil), p.pending...)
		p.mu.Unlock()

		err := p.write(p.path, content)

		p.mu.Lock()
		if target > p.completed {
			p.completed = target
		}
		p.lastErr = err
		close(p.changed)
		p.changed = make(chan struct{})
		more := p.completed < p.requested
		p.mu.Unlock()
		if !more {
			return
		}
	}
}
