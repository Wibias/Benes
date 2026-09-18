//go:build windows

package storage

import (
	"os"

	"golang.org/x/sys/windows"
)

type fileID struct {
	Vol   uint64
	Idx   uint64
	Nlink uint32
	OK    bool
}

func readFileID(path string, info os.FileInfo) fileID {
	if info != nil && isReparse(info) {
		return fileID{}
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return fileID{}
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
		return fileID{}
	}
	defer windows.CloseHandle(handle)
	var data windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &data); err != nil {
		return fileID{}
	}
	idx := (uint64(data.FileIndexHigh) << 32) | uint64(data.FileIndexLow)
	return fileID{
		Vol:   uint64(data.VolumeSerialNumber),
		Idx:   idx,
		Nlink: data.NumberOfLinks,
		OK:    true,
	}
}

func (id fileID) Equal(other fileID) bool {
	return id.OK && other.OK && id.Vol == other.Vol && id.Idx == other.Idx
}
