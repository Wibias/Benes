package wintray

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
)

const (
	StateVersion = 1
	RunKey       = `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`
)

func RunValue(benesHome string) string {
	normalized := strings.ToLower(strings.TrimRight(filepath.Clean(benesHome), `/\`))
	sum := sha256.Sum256([]byte(normalized))
	return "BenesTray-" + hex.EncodeToString(sum[:])[:12]
}

func ScriptPath(home string) string {
	return filepath.Join(home, "benes-tray.ps1")
}

func LauncherPath(home string) string {
	return filepath.Join(home, "benes-tray.vbs")
}

func StatePath(home string) string {
	return filepath.Join(home, "tray-state.json")
}

func HeartbeatPath(home string) string {
	return filepath.Join(home, "tray-heartbeat.json")
}

func IconPaths(home string) []string {
	return []string{
		filepath.Join(home, "benes-tray-online.ico"),
		filepath.Join(home, "benes-tray-warning.ico"),
		filepath.Join(home, "benes-tray-offline.ico"),
	}
}

func Quote(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("empty tray path")
	}
	for _, r := range value {
		if r < 32 || r == 127 || r == '"' {
			return "", fmt.Errorf("tray path contains quotes or control characters")
		}
	}
	return `"` + value + `"`, nil
}
