package requesthistory

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const writerLockName = dbName + ".write.lock"

type writerLock struct {
	file *os.File
}

func acquireWriter(home string) (*writerLock, error) {
	return acquireWriterContext(context.Background(), home)
}

func acquireWriterContext(ctx context.Context, home string) (*writerLock, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("acquire request-history writer lock: %w", err)
	}
	home = strings.TrimSpace(home)
	if home == "" {
		return nil, fmt.Errorf("home is required")
	}
	absHome, err := filepath.Abs(home)
	if err != nil {
		return nil, err
	}
	home = filepath.Clean(absHome)
	if err := ensureRealHomeDir(home); err != nil {
		return nil, fmt.Errorf("open request-history writer lock: %w", err)
	}
	path := filepath.Join(home, writerLockName)
	if err := refuseReparseAncestors(home, path); err != nil {
		return nil, fmt.Errorf("open request-history writer lock: %w", err)
	}
	if err := rejectLeafReparse(home, path); err != nil {
		return nil, fmt.Errorf("open request-history writer lock: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("acquire request-history writer lock: %w", err)
	}
	file, err := openWriterLockFile(path)
	if err != nil {
		return nil, fmt.Errorf("open request-history writer lock: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("open request-history writer lock: %w", err)
	}
	if isReparse(info) {
		_ = file.Close()
		return nil, fmt.Errorf("open request-history writer lock: %w", errPathEscape)
	}
	_ = file.Chmod(0o600)
	for {
		if err := ctx.Err(); err != nil {
			_ = file.Close()
			return nil, fmt.Errorf("acquire request-history writer lock: %w", err)
		}
		testHookMu.Lock()
		hookBefore := testHookBeforeAcquireWriterAttempt
		testHookMu.Unlock()
		if hook := hookBefore; hook != nil {
			hook()
		}
		if err := ctx.Err(); err != nil {
			_ = file.Close()
			return nil, fmt.Errorf("acquire request-history writer lock: %w", err)
		}
		locked, err := tryExclusiveFileLock(file)
		if err != nil {
			_ = file.Close()
			return nil, fmt.Errorf("acquire request-history writer lock: %w", err)
		}
		if locked {
			// Cancellation wins over a raced grant: drop our exclusive lock and
			// close the handle without affecting another process's lock.
			if err := ctx.Err(); err != nil {
				_ = unlockExclusiveFileLock(file)
				_ = file.Close()
				return nil, fmt.Errorf("acquire request-history writer lock: %w", err)
			}
			return &writerLock{file: file}, nil
		}
		testHookMu.Lock()
		hookWait := testHookAcquireWriterWaiting
		testHookMu.Unlock()
		if hook := hookWait; hook != nil {
			hook()
		}
		timer := time.NewTimer(15 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			_ = file.Close()
			return nil, fmt.Errorf("acquire request-history writer lock: %w", ctx.Err())
		case <-timer.C:
		}
	}
}

func (l *writerLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	u := unlockExclusiveFileLock(l.file)
	c := l.file.Close()
	l.file = nil
	if u != nil {
		return u
	}
	return c
}
