package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/codexappserver"
	"github.com/Wibias/Benes/internal/config"
)

func TestRunSyncCacheSkipsWhenCodexOff(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"clientIntegrations":{"codex":false}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, Config: configPath}, nil
	}
	var stdout, stderr bytes.Buffer
	if code := runSyncCache(nil, &stdout, &stderr, deps); code != 0 || !strings.Contains(stdout.String(), "Codex integration is OFF") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRunSyncCacheWritesModelsCache(t *testing.T) {
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	if err := os.WriteFile(filepath.Join(codexHome, "benes-catalog.json"), []byte(`{"models":[{"slug":"openai/gpt-5"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, Config: configPath}, nil
	}
	deps.codexAppServerIO = func() codexappserver.IO {
		return codexappserver.IO{ListSnapshots: func() []codexappserver.Snapshot { return nil }}
	}
	var stdout, stderr bytes.Buffer
	if code := runSyncCache(nil, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(codexHome, "models_cache.json")); err != nil {
		t.Fatal(err)
	}
}
