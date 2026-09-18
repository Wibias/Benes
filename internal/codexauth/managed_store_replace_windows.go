//go:build windows

package codexauth

import (
	"syscall"
	"unsafe"
)

const (
	moveFileReplaceExisting = 0x00000001
	moveFileWriteThrough    = 0x00000008
)

var (
	kernel32MoveDLL    = syscall.NewLazyDLL("kernel32.dll")
	procMoveFileExWide = kernel32MoveDLL.NewProc("MoveFileExW")
)

func replaceFileAtomic(source, target string) error {
	s, e := syscall.UTF16PtrFromString(source)
	if e != nil {
		return e
	}
	t, e := syscall.UTF16PtrFromString(target)
	if e != nil {
		return e
	}
	r1, _, e := procMoveFileExWide.Call(uintptr(unsafe.Pointer(s)), uintptr(unsafe.Pointer(t)), uintptr(moveFileReplaceExisting|moveFileWriteThrough))
	if r1 == 0 {
		return e
	}
	return nil
}
