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
)

func TestZcodeEnableWritesOwnedFragmentAndAsksForRestart(t *testing.T) {
	data := t.TempDir()
	t.Setenv("ZCODE_DATA_DIR", data)
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	body := `{"hostname":"127.0.0.1","port":1455,"providers":{"openai-apikey":{"adapter":"openai-responses","baseUrl":"https://api.openai.com/v1","apiKey":"k","defaultModel":"gpt-5.6"}}}`
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, Config: configPath}, nil
	}
	var stdout, stderr bytes.Buffer
	code := runWithDependencies(context.Background(), []string{"zcode", "enable"}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Restart ZCode") {
		t.Fatalf("stdout=%q", stdout.String())
	}
	raw, err := os.ReadFile(filepath.Join(data, "v2", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	providers, _ := doc["provider"].(map[string]any)
	owned, _ := providers["benes"].(map[string]any)
	if owned["kind"] != "openai-compatible" {
		t.Fatalf("owned=%#v", owned)
	}
	if owned["baseURL"] != "http://127.0.0.1:1455/v1" {
		t.Fatalf("baseURL=%v", owned["baseURL"])
	}
	models, _ := owned["models"].(map[string]any)
	if _, ok := models["openai-apikey/gpt-5.6"]; !ok {
		t.Fatalf("models=%#v", models)
	}
}

func TestZcodeDisableRemovesOwnedFragmentAndAsksForRestart(t *testing.T) {
	data := t.TempDir()
	t.Setenv("ZCODE_DATA_DIR", data)
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"hostname":"127.0.0.1","port":23100}`), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, Config: configPath}, nil
	}
	if code := runWithDependencies(context.Background(), []string{"zcode", "enable"}, bytes.NewBuffer(nil), bytes.NewBuffer(nil), deps); code != 0 {
		t.Fatal("enable")
	}
	var stdout bytes.Buffer
	if code := runWithDependencies(context.Background(), []string{"zcode", "disable"}, &stdout, bytes.NewBuffer(nil), deps); code != 0 {
		t.Fatal("disable")
	}
	if !strings.Contains(stdout.String(), "Restart ZCode") {
		t.Fatalf("stdout=%q", stdout.String())
	}
	raw, err := os.ReadFile(filepath.Join(data, "v2", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	providers, _ := doc["provider"].(map[string]any)
	if _, ok := providers["benes"]; ok {
		t.Fatal("owned fragment survived disable")
	}
}

func TestZcodeRestoreReplaysHistoryAndAsksForRestart(t *testing.T) {
	data := t.TempDir()
	t.Setenv("ZCODE_DATA_DIR", data)
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"hostname":"127.0.0.1","port":1455}`), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, Config: configPath}, nil
	}
	if code := runWithDependencies(context.Background(), []string{"zcode", "enable"}, bytes.NewBuffer(nil), bytes.NewBuffer(nil), deps); code != 0 {
		t.Fatal("first enable")
	}
	if err := os.WriteFile(configPath, []byte(`{"hostname":"127.0.0.1","port":23100}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := runWithDependencies(context.Background(), []string{"zcode", "enable"}, bytes.NewBuffer(nil), bytes.NewBuffer(nil), deps); code != 0 {
		t.Fatal("second enable")
	}
	var stdout bytes.Buffer
	if code := runWithDependencies(context.Background(), []string{"zcode", "restore", "--generation", "1"}, &stdout, bytes.NewBuffer(nil), deps); code != 0 {
		t.Fatal("restore")
	}
	if !strings.Contains(stdout.String(), "Restart ZCode") {
		t.Fatalf("stdout=%q", stdout.String())
	}
	raw, err := os.ReadFile(filepath.Join(data, "v2", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "1455") {
		t.Fatalf("not restored to first generation: %s", raw)
	}
}

func TestZcodeEnableRejectsNonLoopbackOrigin(t *testing.T) {
	data := t.TempDir()
	t.Setenv("ZCODE_DATA_DIR", data)
	var stderr bytes.Buffer
	code := runWithDependencies(context.Background(), []string{"zcode", "enable", "--origin", "https://proxy.example"}, bytes.NewBuffer(nil), &stderr, defaultCommandDependencies())
	if code == 0 {
		t.Fatal("non-loopback origin accepted")
	}
	if !strings.Contains(stderr.String(), "loopback") && !strings.Contains(stderr.String(), "origin") {
		t.Fatalf("stderr=%s", stderr.String())
	}
}
