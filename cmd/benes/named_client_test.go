package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/export"
)

func TestFileClientShortcutIDsSkipDedicatedLaunchers(t *testing.T) {
	got := fileClientShortcutIDs()
	if len(got) == 0 {
		t.Fatal("expected shortcuts")
	}
	for _, id := range got {
		if _, skip := dedicatedFileClientCommands[id]; skip {
			t.Fatalf("dedicated launcher leaked into shortcuts: %s", id)
		}
	}
	for _, id := range export.ClientIDs {
		if _, skip := dedicatedFileClientCommands[id]; skip {
			continue
		}
		if !isFileClientShortcut(id) {
			t.Fatalf("missing shortcut for %s", id)
		}
	}
}

func TestFileClientShortcutEnablesThenLaunches(t *testing.T) {
	home := t.TempDir()
	runtime := filepath.Join(home, "runtime-port.json")
	if err := os.WriteFile(runtime, []byte(`{"pid":9,"port":18080,"hostname":"127.0.0.1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := commandDependencies{
		resolvePaths: func(config.PathOptions) (config.Paths, error) {
			return config.Paths{Home: home, RuntimePort: runtime}, nil
		},
		spawnStart: func() error {
			t.Fatal("should not start")
			return nil
		},
	}
	prevDo := accessDo
	prevLaunch := launchNamedClient
	t.Cleanup(func() {
		accessDo = prevDo
		launchNamedClient = prevLaunch
	})

	for _, id := range fileClientShortcutIDs() {
		t.Run(id, func(t *testing.T) {
			var putURL string
			accessDo = func(method, u string, body []byte) (int, []byte, error) {
				if method != "PUT" {
					t.Fatalf("unexpected %s %s", method, u)
				}
				if strings.Contains(string(body), "sk-") {
					t.Fatalf("secret: %s", body)
				}
				putURL = u
				payload, _ := json.Marshal(map[string]any{"ok": true, "message": "ok", "clientId": id})
				return 200, payload, nil
			}
			var gotName string
			var gotArgs []string
			launchNamedClient = func(name string, args []string, env []string) error {
				gotName = name
				gotArgs = append([]string{}, args...)
				return nil
			}
			var stdout, stderr bytes.Buffer
			code := runFileClientShortcut(id, []string{"--help"}, &stdout, &stderr, deps)
			if code != 0 {
				t.Fatalf("code=%d stderr=%s", code, stderr.String())
			}
			if !strings.Contains(putURL, "/api/client-integrations/"+id) {
				t.Fatalf("put=%s", putURL)
			}
			if gotName != id || strings.Join(gotArgs, " ") != "--help" {
				t.Fatalf("name=%s args=%v", gotName, gotArgs)
			}
			if strings.Contains(stdout.String(), "sk-") || strings.Contains(stderr.String(), "sk-") {
				t.Fatalf("secret in output stdout=%s stderr=%s", stdout.String(), stderr.String())
			}
		})
	}
}

func TestFileClientShortcutRejectsNonLoopbackWhenRequired(t *testing.T) {
	home := t.TempDir()
	runtime := filepath.Join(home, "runtime-port.json")
	if err := os.WriteFile(runtime, []byte(`{"pid":9,"port":18080,"hostname":"10.0.0.8"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := commandDependencies{
		resolvePaths: func(config.PathOptions) (config.Paths, error) {
			return config.Paths{Home: home, RuntimePort: runtime}, nil
		},
	}
	prevLaunch := launchNamedClient
	t.Cleanup(func() { launchNamedClient = prevLaunch })
	launchNamedClient = func(string, []string, []string) error {
		t.Fatal("must not launch")
		return nil
	}
	var stdout, stderr bytes.Buffer
	code := runFileClientShortcut("pi", nil, &stdout, &stderr, deps)
	if code != 2 || !strings.Contains(stderr.String(), "loopback-only") {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
}

func TestRunDispatchesFileClientShortcut(t *testing.T) {
	home := t.TempDir()
	runtime := filepath.Join(home, "runtime-port.json")
	if err := os.WriteFile(runtime, []byte(`{"pid":9,"port":18080,"hostname":"127.0.0.1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	prevDo := accessDo
	prevLaunch := launchNamedClient
	t.Cleanup(func() {
		accessDo = prevDo
		launchNamedClient = prevLaunch
	})
	accessDo = func(method, u string, _ []byte) (int, []byte, error) {
		if method != "PUT" || !strings.Contains(u, "/api/client-integrations/prime") {
			t.Fatalf("unexpected %s %s", method, u)
		}
		return 200, []byte(`{"ok":true,"message":"ok","clientId":"prime"}`), nil
	}
	var launched bool
	launchNamedClient = func(name string, args []string, _ []string) error {
		launched = true
		if name != "prime" {
			t.Fatalf("name=%s", name)
		}
		return nil
	}
	var stdout, stderr bytes.Buffer
	code := runWithDependencies(context.Background(), []string{"prime"}, &stdout, &stderr, commandDependencies{
		resolvePaths: func(config.PathOptions) (config.Paths, error) {
			return config.Paths{Home: home, RuntimePort: runtime}, nil
		},
		spawnStart: func() error {
			t.Fatal("should not start")
			return nil
		},
	})
	if code != 0 || !launched {
		t.Fatalf("code=%d launched=%v stderr=%s", code, launched, stderr.String())
	}
}

func TestHelpListsFileClientShortcuts(t *testing.T) {
	var stdout bytes.Buffer
	printHelp(&stdout)
	help := stdout.String()
	want := strings.Join(fileClientShortcutIDs(), ", ")
	if !strings.Contains(help, want) {
		t.Fatalf("help missing %q: %s", want, help)
	}
}
