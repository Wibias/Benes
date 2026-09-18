package codexauth

import (
	"context"
	"fmt"
	"os"
	"time"
)

const managedStoreLockPollInterval = 25 * time.Millisecond

type managedStoreMutationLock struct{ file *os.File }

func acquireManagedStoreMutationLock(ctx context.Context, path string) (*managedStoreMutationLock, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open managed credential mutation lock: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("harden managed credential mutation lock: %w", err)
	}
	ticker := time.NewTicker(managedStoreLockPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = file.Close()
			return nil, ctx.Err()
		default:
		}
		locked, err := tryExclusiveFileLock(file)
		if err != nil {
			_ = file.Close()
			return nil, fmt.Errorf("acquire managed credential mutation lock: %w", err)
		}
		if locked {
			select {
			case <-ctx.Done():
				_ = unlockExclusiveFileLock(file)
				_ = file.Close()
				return nil, ctx.Err()
			default:
				return &managedStoreMutationLock{file: file}, nil
			}
		}
		select {
		case <-ctx.Done():
			_ = file.Close()
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}
func (l *managedStoreMutationLock) Close() error {
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
