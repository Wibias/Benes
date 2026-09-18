package usageledger

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func isReparse(info os.FileInfo) bool {
	if info == nil {
		return false
	}
	mode := info.Mode()
	if mode&os.ModeSymlink != 0 || mode&os.ModeIrregular != 0 {
		return true
	}
	return windowsReparse(info)
}

func isDirNotReparse(info os.FileInfo) bool {
	return info != nil && info.IsDir() && !isReparse(info)
}

func (l *Ledger) insideHome(candidate string) error {
	clean, err := filepath.Abs(candidate)
	if err != nil {
		return errPathEscape
	}
	clean = filepath.Clean(clean)
	base := filepath.Clean(l.home)
	rel, err := filepath.Rel(base, clean)
	if err != nil {
		return errPathEscape
	}
	rel = filepath.ToSlash(rel)
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return errPathEscape
	}
	if runtimeVolumeMismatch(base, clean) {
		return errPathEscape
	}
	return nil
}

func (l *Ledger) ancestorsOK(abs string) (missing bool, err error) {
	if err := l.insideHome(abs); err != nil {
		return false, err
	}
	cur := abs
	rootAbs := filepath.Clean(l.home)
	for {
		if filepath.Clean(cur) == rootAbs {
			return false, nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return false, errPathEscape
		}
		info, err := os.Lstat(parent)
		if err != nil {
			if os.IsNotExist(err) {
				return true, nil
			}
			return false, err
		}
		if !isDirNotReparse(info) {
			return false, errPathEscape
		}
		if err := l.insideHome(parent); err != nil {
			return false, err
		}
		cur = parent
	}
}

func (l *Ledger) refuseReparseAncestors(abs string) error {
	missing, err := l.ancestorsOK(abs)
	if err != nil {
		return err
	}
	if missing {
		return os.ErrNotExist
	}
	return nil
}

func (l *Ledger) ensureLedgerDirs() error {
	homeInfo, err := os.Lstat(l.home)
	if err != nil {
		return err
	}
	if !isDirNotReparse(homeInfo) {
		return fmt.Errorf("home is not a real directory")
	}
	if err := l.ensureChildDir(l.home, DirName); err != nil {
		return err
	}
	if err := l.ensureChildDir(l.dir(), SegmentsDir); err != nil {
		return err
	}
	return l.refuseReparseAncestors(l.segmentsPath())
}

func (l *Ledger) ensureChildDir(parent, name string) error {
	next := filepath.Join(parent, name)
	if err := l.insideHome(next); err != nil {
		return err
	}
	info, err := os.Lstat(next)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		if err := os.Mkdir(next, 0o700); err != nil {
			return err
		}
		info, err = os.Lstat(next)
		if err != nil {
			return err
		}
	}
	if !isDirNotReparse(info) {
		return fmt.Errorf("%s is not a real directory", next)
	}
	return nil
}

func (l *Ledger) rejectLeafReparse(path string) error {
	if err := l.insideHome(path); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if isReparse(info) {
		return errPathEscape
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory", path)
	}
	return nil
}
