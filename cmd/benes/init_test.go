package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/config"
)

func initTestDeps(t *testing.T) commandDependencies {
	t.Helper()
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, Config: configPath}, nil
	}
	return deps
}

func TestRunInitEOFDoesNotWriteConfig(t *testing.T) {
	deps := initTestDeps(t)
	var stdout, stderr bytes.Buffer
	code := runInit(nil, strings.NewReader(""), &stdout, &stderr, deps)
	if code != 1 || !strings.Contains(strings.ToLower(stderr.String()), "stdin reached eof") {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	paths, err := deps.resolvePaths(config.PathOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(paths.Config); !os.IsNotExist(err) {
		t.Fatalf("config written on EOF: %v", err)
	}
}

func TestRunInitProviderFlagWritesForwardOpenAI(t *testing.T) {
	deps := initTestDeps(t)
	var stdout, stderr bytes.Buffer
	code := runInit([]string{"--provider", "openai", "--port", "18080", "--no-inject", "--no-shim"}, strings.NewReader(""), &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	paths, err := deps.resolvePaths(config.PathOptions{})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(paths.Config)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"apiKey"`) {
		t.Fatalf("unexpected apiKey: %s", raw)
	}
	var cfg struct {
		Port            int    `json:"port"`
		DefaultProvider string `json:"defaultProvider"`
		Providers       map[string]struct {
			Adapter  string `json:"adapter"`
			AuthMode string `json:"authMode"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 18080 || cfg.DefaultProvider != "openai" || cfg.Providers["openai"].AuthMode != "forward" {
		t.Fatalf("cfg=%s", raw)
	}
}

func TestRunInitUnknownProvider(t *testing.T) {
	deps := initTestDeps(t)
	var stdout, stderr bytes.Buffer
	if code := runInit([]string{"--provider", "not-a-provider", "--no-inject", "--no-shim"}, strings.NewReader(""), &stdout, &stderr, deps); code != 2 || !strings.Contains(stderr.String(), "unknown") {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
}
