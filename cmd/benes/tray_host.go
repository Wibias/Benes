package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/Wibias/Benes/internal/wintray"
)

type trayHostEntry struct {
	CLI       string `json:"cli"`
	Script    string `json:"script"`
	CodexHome string `json:"codexHome"`
	BenesHome string `json:"benesHome"`
}

func runTrayHost(stdout, stderr io.Writer) int {
	_ = stdout
	if runtime.GOOS != "windows" {
		fmt.Fprintln(stderr, "benes: tray host is Windows-only")
		return 2
	}
	entry, err := parseTrayHostEntry(os.Getenv("BENES_TRAY_ENTRY_B64"))
	if err != nil {
		fmt.Fprintf(stderr, "benes: tray host: %v\n", err)
		return 1
	}
	_ = os.Unsetenv("BENES_TRAY_ENTRY_B64")
	_ = os.Unsetenv("BENES_TRAY_HOST_ARGS")
	args, err := wintray.ProcessArgs(wintray.Entry{
		CLI:       entry.CLI,
		Script:    entry.Script,
		CodexHome: entry.CodexHome,
		BenesHome: entry.BenesHome,
	}, "Run", os.Getpid())
	if err != nil {
		fmt.Fprintf(stderr, "benes: tray host: %v\n", err)
		return 1
	}
	cmd := exec.Command(wintray.PowerShellPath(), args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(stderr, "benes: tray host exited: %v\n", err)
		return 1
	}
	return 0
}

func parseTrayHostEntry(encoded string) (trayHostEntry, error) {
	if strings.TrimSpace(encoded) == "" {
		return trayHostEntry{}, fmt.Errorf("missing tray host entry")
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return trayHostEntry{}, fmt.Errorf("invalid tray host entry")
	}
	var entry trayHostEntry
	if json.Unmarshal(raw, &entry) != nil {
		return trayHostEntry{}, fmt.Errorf("invalid tray host entry")
	}
	for _, path := range []string{entry.CLI, entry.Script, entry.CodexHome, entry.BenesHome} {
		if _, err := wintray.Quote(path); err != nil {
			return trayHostEntry{}, err
		}
	}
	return entry, nil
}
