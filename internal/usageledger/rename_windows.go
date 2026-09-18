//go:build windows

package usageledger

import (
	"errors"
	"os"
	"time"

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
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		last = windows.MoveFileEx(from, to, windows.MOVEFILE_WRITE_THROUGH)
		if last == nil {
			return nil
		}
		if renameAlreadyExists(last) || attempt == 2 {
			return last
		}
		time.Sleep(time.Duration(attempt+1) * 25 * time.Millisecond)
	}
	return last
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
