//go:build windows

package requesthistory

import (
	"os"

	"golang.org/x/sys/windows"
)

func openWriterLockFile(path string) (*os.File, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	access := uint32(windows.GENERIC_READ | windows.GENERIC_WRITE)
	share := uint32(windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE | windows.FILE_SHARE_DELETE)
	attrs := uint32(windows.FILE_ATTRIBUTE_NORMAL | windows.FILE_FLAG_OPEN_REPARSE_POINT)
	handle, err := windows.CreateFile(name, access, share, nil, windows.OPEN_ALWAYS, attrs, 0)
	if err != nil {
		return nil, err
	}
	var data windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &data); err != nil {
		_ = windows.CloseHandle(handle)
		return nil, err
	}
	const fileAttributeReparsePoint = 0x00000400
	if data.FileAttributes&fileAttributeReparsePoint != 0 {
		_ = windows.CloseHandle(handle)
		return nil, errPathEscape
	}
	return os.NewFile(uintptr(handle), path), nil
}
