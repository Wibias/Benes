package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/config"
)

func TestMcodeBenesBaseURL(t *testing.T) {
	got := mcodeBenesBaseURL("custom_provider:\n  benes:\n    options:\n      apiKey: benes\n      baseURL: http://127.0.0.1:18080\n")
	if got != "http://127.0.0.1:18080" {
		t.Fatalf("got=%q", got)
	}
}

func TestRunMcodeLaunchesWhenConfigMatches(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MINIMAX_DATA_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("custom_provider:\n  benes:\n    options:\n      baseURL: http://127.0.0.1:18080\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime := filepath.Join(t.TempDir(), "runtime-port.json")
	if err := os.WriteFile(runtime, []byte(`{"pid":9,"port":18080,"hostname":"127.0.0.1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	prev := launchMcode
	prevDo := accessDo
	t.Cleanup(func() {
		launchMcode = prev
		accessDo = prevDo
	})
	accessDo = func(method, u string, body []byte) (int, []byte, error) {
		if method != "PUT" || !strings.Contains(u, "/api/client-integrations/mcode") {
			t.Fatalf("unexpected %s %s", method, u)
		}
		if strings.Contains(string(body), "sk-") {
			t.Fatalf("secret: %s", body)
		}
		return 200, []byte(`{"ok":true,"message":"ok","clientId":"mcode"}`), nil
	}
	var gotName string
	var gotArgs []string
	launchMcode = func(name string, args []string, env []string) error {
		gotName = name
		gotArgs = append([]string{}, args...)
		return nil
	}
	var stdout, stderr bytes.Buffer
	code := runMcode([]string{"--help"}, &stdout, &stderr, commandDependencies{
		resolvePaths: func(config.PathOptions) (config.Paths, error) {
			return config.Paths{RuntimePort: runtime}, nil
		},
		spawnStart: func() error {
			t.Fatal("should not start")
			return nil
		},
	})
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if gotName != "mcode" || strings.Join(gotArgs, " ") != "--help" {
		t.Fatalf("name=%s args=%v", gotName, gotArgs)
	}
}

func TestRunMcodeRejectsMissingConnection(t *testing.T) {
	t.Setenv("MINIMAX_DATA_DIR", t.TempDir())
	runtime := filepath.Join(t.TempDir(), "runtime-port.json")
	if err := os.WriteFile(runtime, []byte(`{"pid":9,"port":18080,"hostname":"127.0.0.1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	prevDo := accessDo
	t.Cleanup(func() { accessDo = prevDo })
	accessDo = func(method, u string, _ []byte) (int, []byte, error) {
		if method != "PUT" || !strings.Contains(u, "/api/client-integrations/mcode") {
			t.Fatalf("unexpected %s %s", method, u)
		}
		return 200, []byte(`{"ok":true,"message":"ok","clientId":"mcode"}`), nil
	}
	var stdout, stderr bytes.Buffer
	code := runMcode(nil, &stdout, &stderr, commandDependencies{
		resolvePaths: func(config.PathOptions) (config.Paths, error) {
			return config.Paths{RuntimePort: runtime}, nil
		},
	})
	if code != 2 || !strings.Contains(stderr.String(), "not connected") {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
}
