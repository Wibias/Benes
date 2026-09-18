package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/config"
)

func TestRunAccountUseAndRemoveCursorHitsOAuthRoutes(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	portPath := filepath.Join(home, "runtime-port.json")
	if err := os.WriteFile(configPath, []byte(`{"providers":{}}`), 0o600); err != nil {
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
	t.Cleanup(func() { accessDo = prev })
	accessDo = func(method, u string, body []byte) (int, []byte, error) {
		if method == "PUT" && strings.Contains(u, "/api/oauth/accounts/active") {
			if strings.Contains(string(body), "secret") {
				t.Fatalf("secret: %s", body)
			}
			return 200, []byte(`{"ok":true,"activeAccountId":"user-1"}`), nil
		}
		if method == "DELETE" && strings.Contains(u, "/api/oauth/accounts") && strings.Contains(u, "provider=cursor") {
			return 200, []byte(`{"ok":true}`), nil
		}
		t.Fatalf("unexpected %s %s", method, u)
		return 0, nil, nil
	}
	var stdout, stderr bytes.Buffer
	if code := runAccount([]string{"use", "cursor", "user-1", "--json"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("use code=%d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runAccount([]string{"remove", "cursor", "user-1", "--yes", "--json"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("remove code=%d stderr=%s", code, stderr.String())
	}
}
