package codexshim

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

var lookPath = exec.LookPath
var probeVersion = probeCodexVersion
var benesExecutable = os.Executable

func Install(home string) (bool, string, error) {
	if existing, err := ReadState(home); err != nil {
		return false, "", err
	} else if existing != nil {
		return false, Status(home), nil
	}
	original, err := lookPath("codex")
	if err != nil {
		return false, "Could not find a codex executable on PATH.", nil
	}
	original, err = filepath.Abs(original)
	if err != nil {
		return false, "", err
	}
	if isShim(original) {
		return false, "Codex autostart shim already installed at " + original + ".", nil
	}
	backup := backupPathFor(original)
	if _, err := os.Stat(backup); err == nil {
		return false, "Refusing to overwrite existing backup: " + backup, nil
	}
	if err := os.Rename(original, backup); err != nil {
		return false, "", err
	}
	if err := probeVersion(backup); err != nil {
		_ = os.Rename(backup, original)
		return false, "Refusing Codex autostart shim because the saved launcher failed its --version probe. The original launcher was restored.", nil
	}
	benes := resolveEnsureBenes()
	if benes == "" {
		benes = "benes"
	}
	if err := writeWrapper(original, benes, backup); err != nil {
		rollbackInstall(original, backup)
		if needsPEWrapper(original) {
			return false, "Refusing Codex autostart shim because wrapping a .exe needs a PE benes executable. The original launcher was restored.", nil
		}
		return false, "", err
	}
	if err := probeVersion(original); err != nil {
		rollbackInstall(original, backup)
		return false, "Refusing Codex autostart shim because the installed wrapper failed its --version probe. The original launcher was restored.", nil
	}
	state := State{
		Platform: runtime.GOOS,
		File: File{
			WrapperPath:  original,
			OriginalPath: original,
			BackupPath:   backup,
		},
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		rollbackInstall(original, backup)
		return false, "", err
	}
	if err := os.MkdirAll(home, 0o755); err != nil {
		rollbackInstall(original, backup)
		return false, "", err
	}
	if err := os.WriteFile(StatePath(home), append(encoded, '\n'), 0o600); err != nil {
		rollbackInstall(original, backup)
		return false, "", err
	}
	return true, "Codex autostart shim installed at " + original + ". Original saved at " + backup + ".", nil
}

func rollbackInstall(original, backup string) {
	_ = os.Remove(SidecarPath(original))
	_ = os.Remove(original)
	_ = os.Rename(backup, original)
}

func backupPathFor(path string) string {
	ext := filepath.Ext(path)
	if ext == "" {
		return path + ".benes-real"
	}
	return strings.TrimSuffix(path, ext) + ".benes-real" + ext
}

func wrapperScript(benes, backup string) string {
	if runtime.GOOS == "windows" {
		return "@echo off\r\nrem " + Marker + "\r\n\"" + benes + "\" ensure >nul 2>&1\r\n\"" + backup + "\" %*\r\n"
	}
	return "#!/bin/sh\n# " + Marker + "\n\"" + benes + "\" ensure >/dev/null 2>&1 || true\nexec \"" + backup + "\" \"$@\"\n"
}

func wrapperMode() os.FileMode {
	if runtime.GOOS == "windows" {
		return 0o644
	}
	return 0o755
}

func probeCodexVersion(path string) error {
	cmd := exec.Command(path, "--version")
	cmd.Stdout = nil
	cmd.Stderr = nil
	done := make(chan error, 1)
	go func() { done <- cmd.Run() }()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		return fmt.Errorf("codex --version timed out")
	}
}
