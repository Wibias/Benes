package usageledger

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (l *Ledger) Append(record []byte) error {
	if l == nil {
		return fmt.Errorf("ledger is nil")
	}
	if len(bytes.TrimSpace(record)) == 0 {
		return fmt.Errorf("record is empty")
	}
	if bytes.IndexByte(record, '\n') >= 0 || bytes.IndexByte(record, '\r') >= 0 {
		return fmt.Errorf("record contains a newline")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.prepareWriteLocked(); err != nil {
		return err
	}
	if _, err := l.sealedSegments(); err != nil {
		return err
	}
	size, err := l.recoverActiveCommitBoundary()
	if err != nil {
		return err
	}
	line := append([]byte(nil), record...)
	line = append(line, '\n')
	need := int64(len(line))
	if size > 0 && size+need > l.target {
		if err := l.rotateLocked(); err != nil {
			return err
		}
		size = 0
	}
	if err := l.appendCommitted(line); err != nil {
		return err
	}
	size += need
	if size >= l.target {
		return l.rotateLocked()
	}
	return nil
}

func (l *Ledger) prepareWriteLocked() error {
	if err := l.ensureLedgerDirs(); err != nil {
		return err
	}
	return l.removeLeftoverTempsLocked()
}

func (l *Ledger) removeLeftoverTempsLocked() error {
	for _, dir := range []string{l.dir(), l.segmentsPath()} {
		if err := l.refuseReparseAncestors(dir); err != nil {
			return err
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		for _, entry := range entries {
			name := entry.Name()
			if !strings.HasSuffix(strings.ToLower(name), ".tmp") {
				continue
			}
			path := filepath.Join(dir, name)
			if err := l.rejectLeafReparse(path); err != nil {
				if err == errPathEscape {
					continue
				}
				return err
			}
			info, err := os.Lstat(path)
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return err
			}
			if info.IsDir() || isReparse(info) {
				continue
			}
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	return nil
}

func (l *Ledger) rotateLocked() error {
	active := l.activePath()
	if err := l.refuseReparseAncestors(active); err != nil {
		return err
	}
	if err := l.rejectLeafReparse(active); err != nil {
		return err
	}
	size, err := l.recoverActiveCommitBoundary()
	if err != nil {
		return err
	}
	if size <= 0 {
		return nil
	}
	seq, err := l.nextSequenceLocked()
	if err != nil {
		return err
	}
	dest := l.segmentPath(seq)
	if err := l.insideHome(dest); err != nil {
		return err
	}
	if err := l.refuseReparseAncestors(dest); err != nil {
		return err
	}
	if _, err := os.Lstat(dest); err == nil {
		return fmt.Errorf("sealed segment %s already exists", filepath.Base(dest))
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := l.rejectLeafReparse(active); err != nil {
		return err
	}
	if err := renameNoReplace(active, dest); err != nil {
		if renameAlreadyExists(err) {
			return fmt.Errorf("sealed segment %s already exists", filepath.Base(dest))
		}
		return err
	}
	if err := l.refuseReparseAncestors(active); err != nil {
		return err
	}
	return createEmptyActiveAfterSeal(active)
}

func createEmptyActiveAfterSeal(path string) error {
	file, err := openLedgerFile(path, openCreateExclusive)
	if err == nil {
		return file.Close()
	}
	if os.IsExist(err) {
		return fmt.Errorf("active segment already exists after rotation")
	}
	return nil
}

func (l *Ledger) nextSequenceLocked() (int64, error) {
	segs, err := l.sealedSegments()
	if err != nil {
		return 0, err
	}
	if len(segs) == 0 {
		return 1, nil
	}
	return segs[len(segs)-1].seq + 1, nil
}

func (l *Ledger) appendCommitted(line []byte) error {
	path := l.activePath()
	if err := l.refuseReparseAncestors(path); err != nil {
		return err
	}
	if err := l.rejectLeafReparse(path); err != nil {
		return err
	}
	file, err := openLedgerFile(path, openAppend)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(line)
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}
