package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/config"
)

func TestRunAccountResetCreditsHitsLoopbackWHAMRoute(t *testing.T) {
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
	var saw string
	accessDo = func(method, u string, _ []byte) (int, []byte, error) {
		saw = method + " " + u
		return 200, []byte(`{"credits":[],"available_count":1}`), nil
	}
	defer func() { accessDo = prev }()
	var stdout, stderr bytes.Buffer
	if code := runAccount([]string{"reset-credits", "main", "--json"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(saw, "GET") || !strings.Contains(saw, "/api/codex-auth/reset-credits?accountId=__main__") {
		t.Fatalf("saw=%s", saw)
	}
}

func TestRunAccountResetCreditsConsumeSendsOneRedeemRequestID(t *testing.T) {
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
	prevID := newRedeemRequestID
	calls := 0
	newRedeemRequestID = func() (string, error) {
		calls++
		return "550e8400-e29b-41d4-a716-446655440000", nil
	}
	t.Cleanup(func() { newRedeemRequestID = prevID })
	prev := accessDo
	var method, url string
	var body []byte
	accessDo = func(m, u string, b []byte) (int, []byte, error) {
		method, url, body = m, u, append([]byte(nil), b...)
		return 200, []byte(`{"code":"reset"}`), nil
	}
	t.Cleanup(func() { accessDo = prev })
	var stdout, stderr bytes.Buffer
	if code := runAccount([]string{"reset-credits", "main", "--consume", "--yes", "--json"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if calls != 1 {
		t.Fatalf("id generator calls=%d", calls)
	}
	if method != "POST" || !strings.Contains(url, "/api/codex-auth/reset-credits/consume") {
		t.Fatalf("saw=%s %s", method, url)
	}
	if string(body) != `{"accountId":"__main__","redeemRequestId":"550e8400-e29b-41d4-a716-446655440000"}` {
		t.Fatalf("body=%s", body)
	}
}
