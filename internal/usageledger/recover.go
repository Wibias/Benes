package usageledger

import (
	"os"
)

func (l *Ledger) recoverActiveCommitBoundary() (int64, error) {
	path := l.activePath()
	if err := l.insideHome(path); err != nil {
		return 0, err
	}
	if err := l.refuseReparseAncestors(path); err != nil {
		return 0, err
	}
	if err := l.rejectLeafReparse(path); err != nil {
		return 0, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	size := info.Size()
	if size <= 0 {
		return 0, nil
	}
	file, err := openLedgerFile(path, openRecover)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	defer file.Close()
	committed, err := committedPrefix(file, size)
	if err != nil {
		return 0, err
	}
	if committed < size {
		if err := file.Truncate(committed); err != nil {
			return 0, err
		}
	}
	return committed, nil
}

func committedPrefix(file *os.File, size int64) (int64, error) {
	if size <= 0 {
		return 0, nil
	}
	var buf [4096]byte
	pos := size
	for pos > 0 {
		chunk := int64(len(buf))
		if chunk > pos {
			chunk = pos
		}
		pos -= chunk
		n, err := file.ReadAt(buf[:chunk], pos)
		if err != nil && n == 0 {
			return 0, err
		}
		for i := n - 1; i >= 0; i-- {
			if buf[i] == '\n' {
				return pos + int64(i) + 1, nil
			}
		}
	}
	return 0, nil
}
