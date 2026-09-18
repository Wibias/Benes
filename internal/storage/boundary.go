package storage

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

var (
	errPathEscape = errors.New(CodePathEscape)
	errInvalidRel = errors.New("invalid relative path")
)

type Root struct {
	Raw  string
	Abs  string
	Real string
}

func OpenRoot(codexHome string) (Root, error) {
	home := strings.TrimSpace(codexHome)
	if home == "" {
		return Root{}, fmt.Errorf("CODEX_HOME is required")
	}
	abs, err := filepath.Abs(home)
	if err != nil {
		return Root{}, fmt.Errorf("resolve CODEX_HOME: %w", err)
	}
	abs = filepath.Clean(abs)
	info, err := os.Lstat(abs)
	if err != nil {
		return Root{}, err
	}
	if !info.IsDir() || isReparse(info) {
		return Root{}, fmt.Errorf("CODEX_HOME is not a real directory")
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return Root{}, fmt.Errorf("resolve CODEX_HOME: %w", err)
	}
	real = filepath.Clean(real)
	return Root{Raw: home, Abs: abs, Real: real}, nil
}

func (r Root) TrashDir() string {
	return filepath.Join(r.Abs, trashDirName)
}

func (r Root) ensureInside(candidate string, eval bool) error {
	clean, err := filepath.Abs(candidate)
	if err != nil {
		return errPathEscape
	}
	clean = filepath.Clean(clean)
	base := r.Abs
	check := clean
	if eval {
		if real, err := filepath.EvalSymlinks(clean); err == nil {
			check = filepath.Clean(real)
			base = r.Real
		}
	}
	rel, err := filepath.Rel(base, check)
	if err != nil {
		return errPathEscape
	}
	rel = filepath.ToSlash(rel)
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return errPathEscape
	}
	if runtimeVolumeMismatch(base, check) {
		return errPathEscape
	}
	return nil
}

func normalizeRel(rel string) (string, error) {
	raw := strings.TrimSpace(rel)
	if raw == "" || !utf8.ValidString(raw) || strings.ContainsRune(raw, 0) {
		return "", errInvalidRel
	}
	if len(raw) > maxRelPathLen {
		return "", errInvalidRel
	}
	if filepath.IsAbs(raw) || looksDriveAbs(raw) || strings.HasPrefix(raw, `\\`) {
		return "", errPathEscape
	}
	slash := strings.ReplaceAll(filepath.ToSlash(raw), `\`, "/")
	if strings.HasPrefix(slash, "/") {
		return "", errPathEscape
	}
	parts := strings.Split(slash, "/")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" || part == "." {
			continue
		}
		if part == ".." {
			return "", errPathEscape
		}
		if strings.ContainsAny(part, `\:`) && looksDriveAbs(part) {
			return "", errPathEscape
		}
		out = append(out, part)
	}
	if len(out) == 0 {
		return "", errInvalidRel
	}
	return strings.Join(out, "/"), nil
}

func isTrashName(name string) bool {
	return strings.EqualFold(name, trashDirName)
}

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

func looksDriveAbs(raw string) bool {
	if len(raw) >= 2 && raw[1] == ':' {
		c := raw[0]
		if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' {
			return true
		}
	}
	return false
}

func fileModeBits(info os.FileInfo) uint32 {
	if info == nil {
		return 0
	}
	mode := uint32(info.Mode())
	if isReparse(info) {
		mode |= uint32(fs.ModeIrregular)
	}
	return mode
}
