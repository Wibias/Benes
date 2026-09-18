package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/codexshim"
	"github.com/Wibias/Benes/internal/config"
)

func TestRunUninstallRemovesShimWhenProxyAbsent(t *testing.T) {
	home := t.TempDir()
	wrapper := filepath.Join(home, "codex-wrapper")
	original := filepath.Join(home, "codex")
	backup := filepath.Join(home, "codex.bak")
	if err := os.WriteFile(wrapper, []byte("# benes codex autostart shim\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backup, []byte("native\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	state, _ := jsonShimState(wrapper, original, backup)
	if err := os.WriteFile(codexshim.StatePath(home), state, 0o600); err != nil {
		t.Fatal(err)
	}

	previousSched := queryScheduler
	queryScheduler = func(args []string) (string, error) {
		if strings.Contains(strings.Join(args, " "), "/fo") {
			return `"TaskName"` + "\n", nil
		}
		return "", errors.New("missing")
	}
	t.Cleanup(func() { queryScheduler = previousSched })
	previousCtl := querySystemctl
	querySystemctl = func(args []string) (string, error) { return "not-found\n", nil }
	t.Cleanup(func() { querySystemctl = previousCtl })

	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, PID: filepath.Join(home, "benes.pid"), RuntimePort: filepath.Join(home, "runtime-port.json")}, nil
	}
	t.Setenv("CODEX_HOME", t.TempDir())
	var stdout, stderr bytes.Buffer
	if code := runUninstall(&stdout, &stderr, deps); code != 0 {
		t.Fatalf("code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	if _, err := os.Stat(wrapper); !os.IsNotExist(err) {
		t.Fatal("shim wrapper remained")
	}
	if _, err := os.Stat(codexshim.StatePath(home)); !os.IsNotExist(err) {
		t.Fatal("shim state remained")
	}
}

func jsonShimState(wrapper, original, backup string) ([]byte, error) {
	return json.Marshal(map[string]string{
		"platform":     "linux",
		"wrapperPath":  wrapper,
		"originalPath": original,
		"backupPath":   backup,
	})
}
