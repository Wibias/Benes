package storage

import (
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func (r Root) insideLexical(abs string) error {
	return r.ensureInside(abs, false)
}

// ConfineRel lexically normalizes rel and walks every existing ancestor with
// Lstat. Directory symlinks/junctions/reparse points are never traversed.
func (r Root) ConfineRel(rel string) (abs string, slashRel string, err error) {
	return r.walkRel(rel, false, true)
}

func (r Root) ConfineExisting(rel string, follow bool) (abs string, slashRel string, info os.FileInfo, err error) {
	abs, slashRel, err = r.walkRel(rel, false, true)
	if err != nil {
		return "", "", nil, err
	}
	info, err = os.Lstat(abs)
	if err != nil {
		return "", "", nil, err
	}
	if follow && !isReparse(info) {
		if err := r.ensureInside(abs, true); err != nil {
			return "", "", nil, err
		}
	}
	if err := r.refuseReparseAncestors(abs); err != nil {
		return "", "", nil, err
	}
	return abs, slashRel, info, nil
}

func (r Root) mkdirExclusive(rel string) (string, error) {
	slash, err := normalizeRel(rel)
	if err != nil {
		return "", err
	}
	dirRel := path.Dir(slash)
	if dirRel != "." {
		if _, err := r.EnsureRealDir(dirRel); err != nil {
			return "", err
		}
	}
	abs := filepath.Join(r.Abs, filepath.FromSlash(slash))
	if err := r.insideLexical(abs); err != nil {
		return "", err
	}
	if err := r.refuseReparseAncestors(abs); err != nil {
		return "", err
	}
	if err := os.Mkdir(abs, 0o700); err != nil {
		return "", err
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return "", err
	}
	if !isDirNotReparse(info) {
		return "", errPathEscape
	}
	return abs, nil
}

func (r Root) EnsureRealDir(rel string) (string, error) {
	abs, _, err := r.walkRel(rel, true, false)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return "", err
	}
	if !isDirNotReparse(info) {
		return "", errPathEscape
	}
	if err := r.insideLexical(abs); err != nil {
		return "", err
	}
	return abs, nil
}

func (r Root) writeConfined(rel string, data []byte, mode os.FileMode) error {
	slash, err := normalizeRel(rel)
	if err != nil {
		return err
	}
	dirRel := path.Dir(slash)
	if dirRel != "." {
		if _, err := r.EnsureRealDir(dirRel); err != nil {
			return err
		}
	}
	abs := filepath.Join(r.Abs, filepath.FromSlash(slash))
	if err := r.insideLexical(abs); err != nil {
		return err
	}
	parent := filepath.Dir(abs)
	if err := r.refuseReparseAncestors(parent); err != nil {
		return err
	}
	f, err := os.CreateTemp(parent, ".benes-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	closed := false
	defer func() {
		if !closed {
			_ = f.Close()
		}
		if tmp != "" {
			_ = os.Remove(tmp)
		}
	}()
	if err := r.insideLexical(tmp); err != nil {
		return err
	}
	if err := r.refuseReparseAncestors(filepath.Dir(tmp)); err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		return err
	}
	_ = f.Chmod(mode)
	if err := f.Close(); err != nil {
		return err
	}
	closed = true
	if err := r.refuseReparseAncestors(parent); err != nil {
		return err
	}
	if err := os.Rename(tmp, abs); err != nil {
		return err
	}
	tmp = ""
	return r.refuseReparseAncestors(parent)
}

func (r Root) readConfinedFile(rel string, maxBytes int) ([]byte, error) {
	abs, _, err := r.ConfineRel(rel)
	if err != nil {
		return nil, err
	}
	if err := r.refuseReparseAncestors(abs); err != nil {
		return nil, err
	}
	f, err := openNoFollow(abs)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if isReparse(info) || info.IsDir() {
		return nil, errPathEscape
	}
	if maxBytes <= 0 {
		maxBytes = maxManifestBytes
	}
	raw, err := io.ReadAll(io.LimitReader(f, int64(maxBytes)+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxBytes {
		return nil, coded(CodeInvalidTrash, "trash manifest is too large")
	}
	return raw, nil
}

func (r Root) renameConfined(srcRel, destRel string, rename func(string, string) error, before func() error) error {
	srcAbs, _, srcInfo, err := r.ConfineExisting(srcRel, false)
	if err != nil {
		return err
	}
	if isDirNotReparse(srcInfo) {
		return coded(CodeFSFailed, "refusing to rename a directory")
	}
	destSlash, err := normalizeRel(destRel)
	if err != nil {
		return err
	}
	if path.Dir(destSlash) != "." {
		if _, err := r.EnsureRealDir(path.Dir(destSlash)); err != nil {
			return err
		}
	}
	destAbs := filepath.Join(r.Abs, filepath.FromSlash(destSlash))
	if err := r.insideLexical(destAbs); err != nil {
		return err
	}
	if before != nil {
		if err := before(); err != nil {
			return err
		}
	}
	if err := r.refuseReparseAncestors(srcAbs); err != nil {
		return err
	}
	if err := r.refuseReparseAncestors(filepath.Dir(destAbs)); err != nil {
		return err
	}
	if _, err := os.Lstat(filepath.Dir(destAbs)); err != nil {
		return err
	}
	if rename == nil {
		rename = renameNoReplace
	}
	if err := rename(srcAbs, destAbs); err != nil {
		if renameAlreadyExists(err) {
			return coded(CodeDestExists, "restore destination already exists")
		}
		return err
	}
	return r.refuseReparseAncestors(destAbs)
}

func (r Root) removeObjectConfined(rel string, remove func(string) error) error {
	abs, _, info, err := r.ConfineExisting(rel, false)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if isDirNotReparse(info) {
		return coded(CodeFSFailed, "refusing to remove a real directory as an object")
	}
	if err := r.refuseReparseAncestors(abs); err != nil {
		return err
	}
	if remove == nil {
		remove = os.Remove
	}
	if err := remove(abs); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (r Root) removeTreeConfined(rel string) error {
	abs, _, err := r.walkRel(rel, false, false)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return r.removeTreeAbs(abs)
}

func (r Root) removeTreeAbs(abs string) error {
	if err := r.insideLexical(abs); err != nil {
		return err
	}
	info, err := os.Lstat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if isReparse(info) || !info.IsDir() {
		return os.Remove(abs)
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		child := filepath.Join(abs, entry.Name())
		if err := r.insideLexical(child); err != nil {
			return err
		}
		cinfo, err := os.Lstat(child)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		if isDirNotReparse(cinfo) {
			if err := r.removeTreeAbs(child); err != nil {
				return err
			}
			continue
		}
		if err := os.Remove(child); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return os.Remove(abs)
}

func (r Root) refuseReparseAncestors(abs string) error {
	if err := r.insideLexical(abs); err != nil {
		return err
	}
	cur := abs
	rootAbs := r.Abs
	for {
		if samePath(cur, rootAbs) {
			return nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return errPathEscape
		}
		info, err := os.Lstat(parent)
		if err != nil {
			return err
		}
		if !isDirNotReparse(info) {
			return errPathEscape
		}
		if err := r.insideLexical(parent); err != nil {
			return err
		}
		cur = parent
	}
}

func (r Root) walkRel(rel string, createDirs bool, lastMayBeMissingFile bool) (string, string, error) {
	slash, err := normalizeRel(rel)
	if err != nil {
		return "", "", err
	}
	parts := strings.Split(slash, "/")
	cur := r.Abs
	for i, part := range parts {
		next := filepath.Join(cur, part)
		if err := r.insideLexical(next); err != nil {
			return "", "", err
		}
		isLast := i == len(parts)-1
		info, err := os.Lstat(next)
		if err != nil {
			if !os.IsNotExist(err) {
				return "", "", err
			}
			if isLast && lastMayBeMissingFile {
				if err := r.insideLexical(next); err != nil {
					return "", "", err
				}
				return next, slash, nil
			}
			if !createDirs {
				if lastMayBeMissingFile {
					full := next
					for _, rest := range parts[i+1:] {
						full = filepath.Join(full, rest)
					}
					if err := r.insideLexical(full); err != nil {
						return "", "", err
					}
					return full, slash, nil
				}
				return "", "", err
			}
			if err := os.Mkdir(next, 0o700); err != nil {
				return "", "", err
			}
			info, err = os.Lstat(next)
			if err != nil {
				return "", "", err
			}
			if !isDirNotReparse(info) {
				return "", "", errPathEscape
			}
			cur = next
			continue
		}
		if isLast && lastMayBeMissingFile {
			if err := r.refuseReparseAncestors(next); err != nil {
				return "", "", err
			}
			return next, slash, nil
		}
		if isReparse(info) || !info.IsDir() {
			return "", "", errPathEscape
		}
		cur = next
	}
	return cur, slash, nil
}

func samePath(a, b string) bool {
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	if a == b {
		return true
	}
	return strings.EqualFold(a, b)
}
