package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/config"
)

func TestRunAccountLoginHitsCodexAuthLogin(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	portPath := filepath.Join(home, "runtime-port.json")
	if err := os.WriteFile(configPath, []byte(`{"listen":"127.0.0.1:0"}`), 0o600); err != nil {
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
	var saw []string
	accessDo = func(method, u string, body []byte) (int, []byte, error) {
		saw = append(saw, method+" "+u)
		switch {
		case strings.Contains(u, "/api/codex-auth/login") && !strings.Contains(u, "login/"):
			return 200, []byte(`{"ok":true,"flowId":"flow-1","url":"https://auth.openai.com/oauth/authorize"}`), nil
		case strings.Contains(u, "/api/codex-auth/login/cancel"):
			return 200, []byte(`{"ok":true,"cancelled":true}`), nil
		default:
			t.Fatalf("url=%s", u)
			return 0, nil, nil
		}
	}
	defer func() { accessDo = prev }()
	var stdout, stderr bytes.Buffer
	if code := runAccount([]string{"login", "openai", "--no-wait", "--json"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("login code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "flow-1") {
		t.Fatalf("out=%s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runAccount([]string{"cancel", "openai", "--flow", "flow-1", "--json"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("cancel code=%d stderr=%s", code, stderr.String())
	}
	joined := strings.Join(saw, "\n")
	if !strings.Contains(joined, "/api/codex-auth/login") || !strings.Contains(joined, "/api/codex-auth/login/cancel") {
		t.Fatalf("saw=%v", saw)
	}
}
