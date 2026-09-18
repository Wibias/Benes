//go:build windows

package storage

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

func renameNoReplace(oldpath, newpath string) error {
	from, err := windows.UTF16PtrFromString(oldpath)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(newpath)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(from, to, windows.MOVEFILE_WRITE_THROUGH)
}

func renameAlreadyExists(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, os.ErrExist) {
		return true
	}
	return errors.Is(err, windows.ERROR_ALREADY_EXISTS) || errors.Is(err, windows.ERROR_FILE_EXISTS)
}
