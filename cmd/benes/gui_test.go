package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/config"
)

func TestRunGUIOpensExistingProxy(t *testing.T) {
	home := t.TempDir()
	runtime := filepath.Join(home, "runtime-port.json")
	if err := os.WriteFile(runtime, []byte(`{"pid":9,"port":18080,"hostname":"127.0.0.1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var opened string
	var stdout, stderr bytes.Buffer
	code := runGUI(nil, &stdout, &stderr, withLiveRuntime(commandDependencies{
		resolvePaths: func(config.PathOptions) (config.Paths, error) {
			return config.Paths{RuntimePort: runtime}, nil
		},
		openURL: func(rawURL string) error {
			opened = rawURL
			return nil
		},
		spawnStart: func() error {
			t.Fatal("should not start")
			return nil
		},
	}))
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if opened != "http://localhost:18080" {
		t.Fatalf("opened=%q stdout=%q", opened, stdout.String())
	}
}

func TestRunGUIStartsProxyThenOpens(t *testing.T) {
	home := t.TempDir()
	runtime := filepath.Join(home, "runtime-port.json")
	var stdout, stderr bytes.Buffer
	var opened string
	code := runGUI(nil, &stdout, &stderr, withLiveRuntime(commandDependencies{
		resolvePaths: func(config.PathOptions) (config.Paths, error) {
			return config.Paths{RuntimePort: runtime}, nil
		},
		spawnStart: func() error {
			return os.WriteFile(runtime, []byte(`{"pid":11,"port":19090,"exe":"benes"}`), 0o600)
		},
		sleep: func(time.Duration) {},
		openURL: func(rawURL string) error {
			opened = rawURL
			return nil
		},
	}))
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Proxy not running. Starting...") {
		t.Fatalf("stdout=%q", stdout.String())
	}
	if opened != "http://localhost:19090" {
		t.Fatalf("opened=%q", opened)
	}
}

func TestRunGUIFailsWhenStartNeverPublishes(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runGUI(nil, &stdout, &stderr, commandDependencies{
		resolvePaths: func(config.PathOptions) (config.Paths, error) {
			return config.Paths{RuntimePort: filepath.Join(t.TempDir(), "missing.json")}, nil
		},
		spawnStart: func() error { return nil },
		sleep:      func(time.Duration) {},
		openURL: func(string) error {
			t.Fatal("should not open")
			return nil
		},
	})
	if code != 1 {
		t.Fatalf("code=%d", code)
	}
}
