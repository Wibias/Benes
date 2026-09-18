//go:build windows

package storage

import (
	"os"

	"golang.org/x/sys/windows"
)

func openNoFollow(path string) (*os.File, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(
		name,
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
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
