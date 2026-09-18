//go:build !windows

package requesthistory

import (
	"errors"
	"os"
	"syscall"
)

func tryExclusiveFileLock(f *os.File) (bool, error) {
	e := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if e == nil {
		return true, nil
	}
	if errors.Is(e, syscall.EWOULDBLOCK) || errors.Is(e, syscall.EAGAIN) {
		return false, nil
	}
	return false, e
}

func unlockExclusiveFileLock(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
