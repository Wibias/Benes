//go:build windows

package codexauth

import (
	"errors"
	"os"
	"syscall"
	"unsafe"
)

const (
	lockFileExclusiveLock   = 0x00000002
	lockFileFailImmediately = 0x00000001
	errorLockViolation      = syscall.Errno(33)
)

var (
	kernel32LockDLL  = syscall.NewLazyDLL("kernel32.dll")
	procLockFileEx   = kernel32LockDLL.NewProc("LockFileEx")
	procUnlockFileEx = kernel32LockDLL.NewProc("UnlockFileEx")
)

func tryExclusiveFileLock(file *os.File) (bool, error) {
	var o syscall.Overlapped
	r1, _, e := procLockFileEx.Call(file.Fd(), uintptr(lockFileExclusiveLock|lockFileFailImmediately), 0, uintptr(^uint32(0)), uintptr(^uint32(0)), uintptr(unsafe.Pointer(&o)))
	if r1 != 0 {
		return true, nil
	}
	if errors.Is(e, errorLockViolation) {
		return false, nil
	}
	return false, e
}
func unlockExclusiveFileLock(file *os.File) error {
	var o syscall.Overlapped
	r1, _, e := procUnlockFileEx.Call(file.Fd(), 0, uintptr(^uint32(0)), uintptr(^uint32(0)), uintptr(unsafe.Pointer(&o)))
	if r1 == 0 {
		return e
	}
	return nil
}
