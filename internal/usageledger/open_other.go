//go:build !unix && !windows

package usageledger

import (
	"os"
)

func openLedgerFile(path string, mode openMode) (*os.File, error) {
	info, err := os.Lstat(path)
	exists := err == nil
	if exists && isReparse(info) {
		return nil, errPathEscape
	}
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	flag := 0
	switch mode {
	case openRead:
		flag = os.O_RDONLY
	case openAppend:
		flag = os.O_WRONLY | os.O_APPEND | os.O_CREATE
	case openRecover:
		flag = os.O_RDWR
	case openCreateExclusive:
		flag = os.O_WRONLY | os.O_CREATE | os.O_EXCL
	default:
		return nil, errPathEscape
	}
	file, err := os.OpenFile(path, flag, 0o600)
	if err != nil {
		return nil, err
	}
	info, err = os.Lstat(path)
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if isReparse(info) {
		_ = file.Close()
		return nil, errPathEscape
	}
	return file, nil
}
