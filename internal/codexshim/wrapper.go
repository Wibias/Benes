package codexshim

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func writeWrapper(original, benes, backup string) error {
	if needsPEWrapper(original) {
		return writePEWrapper(original, benes, backup)
	}
	return os.WriteFile(original, []byte(wrapperScript(benes, backup)), wrapperMode())
}

func needsPEWrapper(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".exe")
}

func writePEWrapper(original, benes, backup string) error {
	src, err := resolveWrapperPE()
	if err != nil {
		return err
	}
	if err := copyFile(src, original); err != nil {
		return err
	}
	if !isPEFile(original) {
		return fmt.Errorf("wrapper is not a PE executable")
	}
	ensureBenes := strings.TrimSpace(benes)
	if ensureBenes == "" || isEphemeralGoBinary(ensureBenes) || samePath(ensureBenes, original) {
		ensureBenes = ""
	}
	return WriteSidecar(SidecarPath(original), Sidecar{
		Marker: Marker,
		Benes:  ensureBenes,
		Backup: backup,
	})
}

func resolveWrapperPE() (string, error) {
	if exe, err := benesExecutable(); err == nil && isPEFile(exe) {
		return exe, nil
	}
	if p, err := lookPath("benes"); err == nil && isPEFile(p) {
		return p, nil
	}
	return "", fmt.Errorf("need a PE benes executable to wrap a .exe Codex launcher")
}

func resolveEnsureBenes() string {
	if exe, err := benesExecutable(); err == nil && strings.TrimSpace(exe) != "" && !isEphemeralGoBinary(exe) {
		return exe
	}
	if p, err := lookPath("benes"); err == nil && !isEphemeralGoBinary(p) {
		return p
	}
	return ""
}

func isEphemeralGoBinary(path string) bool {
	normalized := strings.ToLower(filepath.ToSlash(strings.ReplaceAll(path, `\`, "/")))
	return strings.Contains(normalized, "/go-build") || strings.Contains(normalized, "/go-test")
}

func isPEFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var hdr [2]byte
	n, err := f.Read(hdr[:])
	return err == nil && n == 2 && hdr[0] == 'M' && hdr[1] == 'Z'
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, wrapperMode())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func samePath(a, b string) bool {
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return strings.EqualFold(absA, absB)
}
