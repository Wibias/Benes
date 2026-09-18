package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/config"
)

func TestRunClientIntegrationStatusHitsLoopbackRoute(t *testing.T) {
	home := t.TempDir()
	portPath := filepath.Join(home, "runtime-port.json")
	if err := os.WriteFile(portPath, []byte(`{"pid":1,"port":18080,"hostname":"127.0.0.1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, RuntimePort: portPath}, nil
	}
	prev := accessDo
	accessDo = func(method, u string, _ []byte) (int, []byte, error) {
		if method != "GET" || !strings.Contains(u, "/api/client-integrations") {
			t.Fatalf("unexpected %s %s", method, u)
		}
		return 200, []byte(`{"clients":[{"clientId":"opencode","state":"absent","installed":false}]}`), nil
	}
	defer func() { accessDo = prev }()
	var stdout, stderr bytes.Buffer
	if code := runIntegration([]string{"client", "status", "--json"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "sk-") || !strings.Contains(stdout.String(), `"opencode"`) {
		t.Fatalf("stdout=%s", stdout.String())
	}
}

func TestRunClientIntegrationEnableHitsPut(t *testing.T) {
	home := t.TempDir()
	portPath := filepath.Join(home, "runtime-port.json")
	if err := os.WriteFile(portPath, []byte(`{"pid":1,"port":18080,"hostname":"127.0.0.1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, RuntimePort: portPath}, nil
	}
	prev := accessDo
	accessDo = func(method, u string, body []byte) (int, []byte, error) {
		if method != "PUT" || !strings.Contains(u, "/api/client-integrations/opencode") {
			t.Fatalf("unexpected %s %s %s", method, u, body)
		}
		if strings.Contains(string(body), "sk-") {
			t.Fatalf("secret: %s", body)
		}
		return 200, []byte(`{"ok":true,"message":"ok","clientId":"opencode"}`), nil
	}
	defer func() { accessDo = prev }()
	var stdout, stderr bytes.Buffer
	if code := runIntegration([]string{"client", "enable", "--client", "opencode", "--json"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
}
