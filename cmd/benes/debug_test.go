package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/config"
)

func TestRunDebugRequiresLiveProxy(t *testing.T) {
	deps := accessTestDeps(t)
	var stdout, stderr bytes.Buffer
	if code := runDebug(context.Background(), []string{"provider", "status"}, &stdout, &stderr, deps); code == 0 || !strings.Contains(stderr.String(), "not running") {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
}

func TestRunDebugStatusUsesLiveSettings(t *testing.T) {
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
	accessDo = func(method, url string, _ []byte) (int, []byte, error) {
		if method != "GET" || !strings.Contains(url, "/api/debug") {
			t.Fatalf("method=%s url=%s", method, url)
		}
		return 200, []byte(`{"enabled":true,"usage":false,"injection":false,"claude":false,"runtimeOverride":{"debug":true},"env":{"debug":false,"usage":false,"injection":false,"claude":false}}`), nil
	}
	defer func() { accessDo = prev }()
	var stdout, stderr bytes.Buffer
	if code := runDebug(context.Background(), []string{"provider", "status"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Provider debug: ON") {
		t.Fatalf("stdout=%q", stdout.String())
	}
}

func liveProxyDeps(t *testing.T) commandDependencies {
	t.Helper()
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
	return deps
}

func TestRunDebugLogsPrintsSnapshot(t *testing.T) {
	deps := liveProxyDeps(t)
	prev := accessDo
	accessDo = func(method, url string, _ []byte) (int, []byte, error) {
		if method != "GET" || !strings.Contains(url, "/api/debug/logs") {
			t.Fatalf("method=%s url=%s", method, url)
		}
		if strings.Contains(url, "after=") {
			t.Fatalf("snapshot requested after=%s", url)
		}
		return 200, []byte(`[{"seq":7,"line":"GET /v1/models"}]`), nil
	}
	defer func() { accessDo = prev }()
	var stdout, stderr bytes.Buffer
	if code := runDebug(context.Background(), []string{"provider", "logs"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "GET /v1/models") {
		t.Fatalf("stdout=%q", stdout.String())
	}
}

func TestRunDebugLogsFollowPollsAfterSeq(t *testing.T) {
	deps := liveProxyDeps(t)
	var urls []string
	prev := accessDo
	accessDo = func(method, url string, _ []byte) (int, []byte, error) {
		if method != "GET" {
			t.Fatalf("method=%s url=%s", method, url)
		}
		urls = append(urls, url)
		if len(urls) == 1 {
			return 200, []byte(`[{"seq":1,"line":"first"}]`), nil
		}
		return 200, []byte(`[{"seq":2,"line":"second"}]`), nil
	}
	defer func() { accessDo = prev }()
	ctx, cancel := context.WithCancel(context.Background())
	deps.sleep = func(time.Duration) {
		if len(urls) >= 2 {
			cancel()
		}
	}
	var stdout, stderr bytes.Buffer
	if code := runDebug(ctx, []string{"provider", "logs", "-f"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "first") || !strings.Contains(stdout.String(), "second") {
		t.Fatalf("stdout=%q", stdout.String())
	}
	if len(urls) < 2 || !strings.Contains(urls[1], "after=1") {
		t.Fatalf("urls=%v", urls)
	}
	if strings.Contains(stderr.String(), "not implemented") {
		t.Fatalf("stderr=%q", stderr.String())
	}
}
