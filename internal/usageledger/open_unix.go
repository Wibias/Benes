//go:build unix

package usageledger

import (
	"errors"
	"os"
	"syscall"
)

func openLedgerFile(path string, mode openMode) (*os.File, error) {
	flag := syscall.O_NOFOLLOW
	switch mode {
	case openRead:
		flag |= os.O_RDONLY
	case openAppend:
		flag |= os.O_WRONLY | os.O_APPEND | os.O_CREATE
	case openRecover:
		flag |= os.O_RDWR
	case openCreateExclusive:
		flag |= os.O_WRONLY | os.O_CREATE | os.O_EXCL
	default:
		return nil, errPathEscape
	}
	file, err := os.OpenFile(path, flag, 0o600)
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return nil, errPathEscape
		}
		return nil, err
	}
	return file, nil
}
