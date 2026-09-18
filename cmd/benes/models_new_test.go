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

func TestRunModelsNewPolicyOffDisablesLaterCatalogArrivalOnce(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	body := `{
		"providers":{
			"openrouter":{
				"adapter":"openai-chat",
				"baseUrl":"https://openrouter.ai/api/v1",
				"models":["openai/gpt-5.6-sol","openai/gpt-4o"]
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
	if code := runModels([]string{"new-policy", "off"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("policy code=%d stderr=%s", code, stderr.String())
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc["disabledModels"]; ok {
		t.Fatalf("bootstrap hid models: %#v", doc["disabledModels"])
	}

	providers, _ := doc["providers"].(map[string]any)
	owned, _ := providers["openrouter"].(map[string]any)
	owned["models"] = []any{"openai/gpt-5.6-sol", "openai/gpt-4o", "x-ai/grok-5"}
	providers["openrouter"] = owned
	doc["providers"] = providers
	updated, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, updated, 0o600); err != nil {
		t.Fatal(err)
	}

	stdout.Reset()
	stderr.Reset()
	if code := runModels([]string{"new-arrivals"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("arrivals code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "x-ai/grok-5") || !strings.Contains(stdout.String(), "auto-disabled") {
		t.Fatalf("arrivals stdout=%q", stdout.String())
	}
	raw, err = os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	disabled, _ := doc["disabledModels"].([]any)
	if len(disabled) != 1 || disabled[0] != "x-ai/grok-5" {
		t.Fatalf("disabled=%#v", disabled)
	}

	stdout.Reset()
	stderr.Reset()
	if code := runModels([]string{"enable", "openrouter/x-ai/grok-5"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("enable code=%d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runModels([]string{"new-arrivals"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("second arrivals code=%d stderr=%s", code, stderr.String())
	}
	raw, err = os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	doc = map[string]any{}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc["disabledModels"]; ok {
		t.Fatalf("enable was undone: %#v", doc["disabledModels"])
	}
}
