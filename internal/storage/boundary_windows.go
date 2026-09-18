//go:build windows

package storage

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

func runtimeVolumeMismatch(root, candidate string) bool {
	rv := strings.ToUpper(filepath.VolumeName(root))
	cv := strings.ToUpper(filepath.VolumeName(candidate))
	return rv != "" && cv != "" && rv != cv
}

func windowsReparse(info os.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Win32FileAttributeData)
	if !ok || st == nil {
		return info.Mode()&fs.ModeIrregular != 0
	}
	const fileAttributeReparsePoint = 0x00000400
	return st.FileAttributes&fileAttributeReparsePoint != 0
}
