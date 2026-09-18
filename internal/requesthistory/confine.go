package requesthistory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var errPathEscape = fmt.Errorf("request-history path escapes home or follows a reparse point")

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

func insideHome(home, candidate string) error {
	clean, err := filepath.Abs(candidate)
	if err != nil {
		return errPathEscape
	}
	clean = filepath.Clean(clean)
	base := filepath.Clean(home)
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

func refuseReparseAncestors(home, abs string) error {
	if err := insideHome(home, abs); err != nil {
		return err
	}
	cur := abs
	rootAbs := filepath.Clean(home)
	for {
		if filepath.Clean(cur) == rootAbs {
			return nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return errPathEscape
		}
		info, err := os.Lstat(parent)
		if err != nil {
			if os.IsNotExist(err) {
				return os.ErrNotExist
			}
			return err
		}
		if !isDirNotReparse(info) {
			return errPathEscape
		}
		if err := insideHome(home, parent); err != nil {
			return err
		}
		cur = parent
	}
}

func rejectLeafReparse(home, path string) error {
	if err := insideHome(home, path); err != nil {
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

func ensureRealHomeDir(home string) error {
	info, err := os.Lstat(home)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		if err := os.MkdirAll(home, 0o700); err != nil {
			return err
		}
		info, err = os.Lstat(home)
		if err != nil {
			return err
		}
	}
	if !isDirNotReparse(info) {
		return fmt.Errorf("home is not a real directory")
	}
	return nil
}
