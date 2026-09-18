package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/codexappserver"
	"github.com/Wibias/Benes/internal/config"
)

func inertAppServerIO() codexappserver.IO {
	return codexappserver.IO{ListSnapshots: func() []codexappserver.Snapshot { return nil }}
}

func TestRunSyncInjectsLiveProxyURL(t *testing.T) {
	benesHome := t.TempDir()
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	runtime := filepath.Join(benesHome, "runtime-port.json")
	if err := os.WriteFile(runtime, []byte(`{"pid":7,"port":18080,"hostname":"127.0.0.1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codexHome, "config.toml"), []byte("model = \"gpt-5\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := runSync(nil, &stdout, &stderr, commandDependencies{
		resolvePaths: func(config.PathOptions) (config.Paths, error) {
			return config.Paths{RuntimePort: runtime}, nil
		},
		codexAppServerIO: inertAppServerIO,
	})
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	raw, _ := os.ReadFile(filepath.Join(codexHome, "config.toml"))
	if !strings.Contains(string(raw), "http://127.0.0.1:18080/v1") {
		t.Fatalf("config=%s stdout=%s", raw, stdout.String())
	}
}

func TestRunSyncWritesRoutedCatalogWithoutNativeEligibility(t *testing.T) {
	benesHome := t.TempDir()
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	runtime := filepath.Join(benesHome, "runtime-port.json")
	if err := os.WriteFile(runtime, []byte(`{"pid":7,"port":18080,"hostname":"127.0.0.1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codexHome, "config.toml"), []byte("model = \"gpt-5\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codexHome, "models_cache.json"), []byte(`{"models":[{"slug":"gpt-reserve","base_instructions":"Luna Reserve","available_in_plans":["plus"]},{"slug":"gpt-5.6-sol","base_instructions":"You are a helpful coding assistant.","available_in_plans":["plus"],"availability_nux":{"title":"nux"}}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(benesHome, "config.json")
	body := `{"providers":{"openai-apikey":{"adapter":"openai-responses","baseUrl":"https://api.openai.com/v1","apiKey":"k","defaultModel":"gpt-5.6"}}}`
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := runSync(nil, &stdout, &stderr, commandDependencies{
		resolvePaths: func(config.PathOptions) (config.Paths, error) {
			return config.Paths{RuntimePort: runtime, Config: configPath}, nil
		},
		codexAppServerIO: inertAppServerIO,
	})
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	raw, err := os.ReadFile(filepath.Join(codexHome, "benes-catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Models []map[string]any `json:"models"`
	}
	if json.Unmarshal(raw, &file) != nil {
		t.Fatalf("catalog=%s", raw)
	}
	bySlug := map[string]map[string]any{}
	for _, model := range file.Models {
		slug, _ := model["slug"].(string)
		bySlug[slug] = model
	}
	if _, ok := bySlug["gpt-5.6-sol"]; !ok {
		t.Fatalf("native dropped: %s", raw)
	}
	routed := bySlug["openai-apikey/gpt-5.6"]
	if routed == nil {
		t.Fatalf("routed missing: %s", raw)
	}
	if routed["supported_in_api"] != true || routed["visibility"] != "list" {
		t.Fatalf("routed=%#v", routed)
	}
	if _, ok := routed["available_in_plans"]; ok {
		t.Fatal("routed row cloned ChatGPT plan gating")
	}
	if _, ok := routed["availability_nux"]; ok {
		t.Fatal("routed row cloned availability_nux")
	}
}

func TestRunSyncRequiresRunningProxy(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runSync(nil, &stdout, &stderr, commandDependencies{
		resolvePaths: func(config.PathOptions) (config.Paths, error) {
			return config.Paths{RuntimePort: filepath.Join(t.TempDir(), "missing.json")}, nil
		},
	})
	if code != 1 {
		t.Fatalf("code=%d", code)
	}
}

func TestRunSyncRestartCodexStillInjects(t *testing.T) {
	benesHome := t.TempDir()
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	runtime := filepath.Join(benesHome, "runtime-port.json")
	if err := os.WriteFile(runtime, []byte(`{"pid":7,"port":18080}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codexHome, "config.toml"), []byte("model = \"gpt-5\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := runSync([]string{"--restart-codex"}, &stdout, &stderr, commandDependencies{
		resolvePaths: func(config.PathOptions) (config.Paths, error) {
			return config.Paths{RuntimePort: runtime}, nil
		},
		codexAppServerIO: inertAppServerIO,
	})
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "not implemented") {
		t.Fatalf("stdout=%q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "benes") {
		t.Fatalf("inject missing stdout=%q", stdout.String())
	}
	if strings.Contains(stdout.String(), "Stopping Codex app-server") {
		t.Fatalf("unexpected restart log stdout=%q", stdout.String())
	}
}

func TestRunSyncRestartCodexStopsMatchingProcesses(t *testing.T) {
	benesHome := t.TempDir()
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	runtime := filepath.Join(benesHome, "runtime-port.json")
	if err := os.WriteFile(runtime, []byte(`{"pid":7,"port":18080}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codexHome, "config.toml"), []byte("model = \"gpt-5\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var killed []int
	var stdout, stderr bytes.Buffer
	code := runSync([]string{"--restart-codex"}, &stdout, &stderr, commandDependencies{
		resolvePaths: func(config.PathOptions) (config.Paths, error) {
			return config.Paths{RuntimePort: runtime}, nil
		},
		codexAppServerIO: func() codexappserver.IO {
			return codexappserver.IO{
				Platform: "linux",
				ListSnapshots: func() []codexappserver.Snapshot {
					return []codexappserver.Snapshot{{PID: 4242, CommandLine: "/usr/local/bin/codex app-server"}}
				},
				Kill: func(pid int) error {
					killed = append(killed, pid)
					return nil
				},
				IsAlive:  func(int) bool { return false },
				WaitExit: func(int, int) bool { return true },
			}
		},
	})
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if len(killed) != 1 || killed[0] != 4242 {
		t.Fatalf("killed=%v", killed)
	}
	if !strings.Contains(stdout.String(), "Stopping Codex app-server process(es): 4242") {
		t.Fatalf("stdout=%q", stdout.String())
	}
}
