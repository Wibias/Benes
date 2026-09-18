//go:build windows

package requesthistory

import (
	"time"

	"golang.org/x/sys/windows"
)

func replaceFile(tmp, dest string) error {
	from, err := windows.UTF16PtrFromString(tmp)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(dest)
	if err != nil {
		return err
	}
	flags := uint32(windows.MOVEFILE_REPLACE_EXISTING | windows.MOVEFILE_WRITE_THROUGH)
	var last error
	for attempt := 0; attempt < 8; attempt++ {
		last = windows.MoveFileEx(from, to, flags)
		if last == nil {
			return nil
		}
		time.Sleep(time.Duration(attempt+1) * 15 * time.Millisecond)
	}
	return last
}
