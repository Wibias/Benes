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

func TestRunModelsPresetApplySeedsOpenRouterAndAllClears(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	body := `{
		"providers":{
			"openrouter":{
				"adapter":"openai-chat",
				"baseUrl":"https://openrouter.ai/api/v1",
				"apiKey":"k",
				"models":["openai/gpt-5.6-sol","openai/gpt-4o","anthropic/claude-opus-5"]
			}
		},
		"customModels":[{"id":"cm1","provider":"openrouter","modelId":"lab/fixture","addedAt":"2026-01-01T00:00:00Z"}]
	}`
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, Config: configPath}, nil
	}
	var stdout, stderr bytes.Buffer
	if code := runModels([]string{"preset", "apply", "openrouter"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("apply code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "preset") || !strings.Contains(stdout.String(), "openai/gpt-5.6-sol") {
		t.Fatalf("apply stdout=%q", stdout.String())
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	providers, _ := doc["providers"].(map[string]any)
	owned, _ := providers["openrouter"].(map[string]any)
	selected, _ := owned["selectedModels"].([]any)
	if len(selected) != 3 {
		t.Fatalf("selected=%#v", selected)
	}
	marker, _ := owned["modelPreset"].(map[string]any)
	if marker["mode"] != "preset" {
		t.Fatalf("marker=%#v", marker)
	}

	stdout.Reset()
	stderr.Reset()
	if code := runModels([]string{"preset", "show", "openrouter"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("show code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "mode=preset") {
		t.Fatalf("show stdout=%q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := runModels([]string{"preset", "apply", "openrouter", "--all"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("all code=%d stderr=%s", code, stderr.String())
	}
	raw, err = os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	providers, _ = doc["providers"].(map[string]any)
	owned, _ = providers["openrouter"].(map[string]any)
	if _, ok := owned["selectedModels"]; ok {
		t.Fatalf("selectedModels survived --all: %#v", owned["selectedModels"])
	}
	marker, _ = owned["modelPreset"].(map[string]any)
	if marker["mode"] != "all" {
		t.Fatalf("after all marker=%#v", marker)
	}
}

func TestRunModelsPresetApplyZeroMatchKeepsSelection(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	body := `{
		"providers":{
			"openrouter":{
				"adapter":"openai-chat",
				"baseUrl":"https://openrouter.ai/api/v1",
				"apiKey":"k",
				"models":["openai/gpt-4o"],
				"selectedModels":["openai/gpt-4o"]
			}
		}
	}`
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, Config: configPath}, nil
	}
	var stdout, stderr bytes.Buffer
	if code := runModels([]string{"preset", "apply", "openrouter"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "0 catalog") && !strings.Contains(stderr.String(), "0 catalog") {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"selectedModels"`) {
		t.Fatalf("selection dropped: %s", raw)
	}
}

func TestRunModelsSelectedFlipsPresetToCustom(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	body := `{
		"providers":{
			"openrouter":{
				"adapter":"openai-chat",
				"baseUrl":"https://openrouter.ai/api/v1",
				"apiKey":"k",
				"models":["openai/gpt-5.6-sol"],
				"selectedModels":["openai/gpt-5.6-sol"],
				"modelPreset":{"mode":"preset","appliedVersion":1}
			}
		}
	}`
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, Config: configPath}, nil
	}
	var stdout, stderr bytes.Buffer
	if code := runModels([]string{"selected", "openrouter", "--set", "openai/gpt-4o"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"mode": "custom"`) && !strings.Contains(string(raw), `"mode":"custom"`) {
		t.Fatalf("did not mark custom: %s", raw)
	}
}

func TestRunModelsAddWhilePresetKeepsCustomVisible(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	body := `{
		"providers":{
			"openrouter":{
				"adapter":"openai-chat",
				"baseUrl":"https://openrouter.ai/api/v1",
				"apiKey":"k",
				"selectedModels":["openai/gpt-5.6-sol"],
				"modelPreset":{"mode":"preset","appliedVersion":1}
			}
		}
	}`
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, Config: configPath}, nil
	}
	var stdout, stderr bytes.Buffer
	if code := runModels([]string{"add", "openrouter", "lab/fixture"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, `"lab/fixture"`) {
		t.Fatalf("custom id missing from allowlist: %s", text)
	}
	if !strings.Contains(text, `"mode": "preset"`) && !strings.Contains(text, `"mode":"preset"`) {
		t.Fatalf("preset marker flipped: %s", text)
	}
}
