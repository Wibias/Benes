package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/config"
)

func TestRunAccountRefreshAndAutoSwitchHitLoopback(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	portPath := filepath.Join(home, "runtime-port.json")
	if err := os.WriteFile(configPath, []byte(`{"providers":{"openai-apikey":{"authMode":"key"}}}`), 0o600); err != nil {
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
	accessDo = func(method, u string, body []byte) (int, []byte, error) {
		if method == "GET" && strings.Contains(u, "/api/provider-quotas?refresh=1") {
			return 200, []byte(`{"reports":[{"provider":"openai-apikey","source":"openrouter:credits","quota":{"weeklyPercent":12}}]}`), nil
		}
		if method == "PUT" && strings.Contains(u, "/api/codex-auth/auto-switch") {
			if !strings.Contains(string(body), `"threshold":0`) {
				t.Fatalf("body=%s", body)
			}
			return 200, []byte(`{"ok":true}`), nil
		}
		if method == "GET" && strings.Contains(u, "/api/codex-auth/active") {
			return 200, []byte(`{"autoSwitchThreshold":80}`), nil
		}
		t.Fatalf("unexpected %s %s", method, u)
		return 0, nil, nil
	}
	defer func() { accessDo = prev }()
	var stdout, stderr bytes.Buffer
	if code := runAccount([]string{"refresh", "openai-apikey"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("refresh code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "openai-apikey") || !strings.Contains(stdout.String(), "weekly 12%") {
		t.Fatalf("refresh out=%s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runAccount([]string{"auto-switch", "openai", "off", "--json"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("auto-switch code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"enabled":false`) {
		t.Fatalf("auto-switch out=%s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runAccount([]string{"auto-switch", "openai", "status"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("status code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "auto-switch: on (threshold 80%)") {
		t.Fatalf("status out=%s", stdout.String())
	}
}
