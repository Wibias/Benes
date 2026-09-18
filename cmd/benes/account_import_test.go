package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/config"
)

func TestRunAccountImportPostsCockpitDocument(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	portPath := filepath.Join(home, "runtime-port.json")
	source := filepath.Join(home, "cockpit.json")
	if err := os.WriteFile(configPath, []byte(`{"listen":"127.0.0.1:0"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portPath, []byte(`{"pid":1,"port":18080,"hostname":"127.0.0.1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte(`[{"email":"user@example.com","refresh_token":"rt"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, Config: configPath, RuntimePort: portPath}, nil
	}
	prev := accessDo
	var saw string
	accessDo = func(method, u string, body []byte) (int, []byte, error) {
		saw = method + " " + u
		if !strings.Contains(string(body), "google-antigravity") || !strings.Contains(string(body), "cockpit-tools") {
			t.Fatalf("body=%s", body)
		}
		return 200, []byte(`{"totalCount":1,"importedCount":1,"updatedCount":0,"failedCount":0,"unsupportedCount":0,"results":[{"index":0,"status":"imported","code":"imported"}]}`), nil
	}
	defer func() { accessDo = prev }()
	var stdout, stderr bytes.Buffer
	if code := runAccount([]string{"import", "google-antigravity", "--format", "cockpit-tools", "--file", source, "--json"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(saw, "POST") || !strings.Contains(saw, "/api/oauth/accounts/import") {
		t.Fatalf("saw=%s", saw)
	}
}
