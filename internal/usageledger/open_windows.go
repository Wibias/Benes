//go:build windows

package usageledger

import (
	"os"

	"golang.org/x/sys/windows"
)

func openLedgerFile(path string, mode openMode) (*os.File, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	var access uint32
	var disposition uint32
	share := uint32(windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE | windows.FILE_SHARE_DELETE)
	attrs := uint32(windows.FILE_FLAG_BACKUP_SEMANTICS | windows.FILE_FLAG_OPEN_REPARSE_POINT)
	switch mode {
	case openRead:
		access = windows.GENERIC_READ
		disposition = windows.OPEN_EXISTING
	case openAppend:
		access = windows.FILE_APPEND_DATA | windows.SYNCHRONIZE
		disposition = windows.OPEN_ALWAYS
	case openRecover:
		access = windows.GENERIC_READ | windows.GENERIC_WRITE
		disposition = windows.OPEN_EXISTING
	case openCreateExclusive:
		access = windows.GENERIC_WRITE
		disposition = windows.CREATE_NEW
	default:
		return nil, errPathEscape
	}
	handle, err := windows.CreateFile(name, access, share, nil, disposition, attrs, 0)
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
