package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/config"
)

func TestRunAccountRemoveDeletesCodexPoolAccount(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	portPath := filepath.Join(home, "runtime-port.json")
	if err := os.WriteFile(configPath, []byte(`{"providers":{"openai":{"adapter":"openai-responses","authMode":"forward"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portPath, []byte(`{"pid":1,"port":18080,"hostname":"127.0.0.1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, Config: configPath, RuntimePort: portPath}, nil
	}
	prev := accessDo
	var saw string
	accessDo = func(method, u string, _ []byte) (int, []byte, error) {
		saw = method + " " + u
		return 200, []byte(`{"ok":true}`), nil
	}
	defer func() { accessDo = prev }()
	var stdout, stderr bytes.Buffer
	if code := runAccount([]string{"remove", "openai", "pool-1", "--yes", "--json"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(saw, "DELETE") || !strings.Contains(saw, "/api/codex-auth/accounts?id=pool-1") {
		t.Fatalf("saw=%s", saw)
	}
}

func TestRunAccountRemoveRefusesMain(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	portPath := filepath.Join(home, "runtime-port.json")
	if err := os.WriteFile(configPath, []byte(`{"providers":{"openai":{"adapter":"openai-responses","authMode":"forward"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portPath, []byte(`{"pid":1,"port":18080,"hostname":"127.0.0.1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, Config: configPath, RuntimePort: portPath}, nil
	}
	var stdout, stderr bytes.Buffer
	if code := runAccount([]string{"remove", "openai", "main", "--yes"}, &stdout, &stderr, deps); code != 2 || !strings.Contains(stderr.String(), "cannot be removed") {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
}
