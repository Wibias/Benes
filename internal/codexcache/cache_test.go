package codexcache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestInvalidateWritesStaleCache(t *testing.T) {
	home := t.TempDir()
	catalog := []byte(`{"models":[{"slug":"openai/gpt-5","display_name":"GPT-5"}]}`)
	if err := os.WriteFile(filepath.Join(home, "benes-catalog.json"), catalog, 0o600); err != nil {
		t.Fatal(err)
	}
	ok, err := Invalidate(home)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	raw, err := os.ReadFile(filepath.Join(home, "models_cache.json"))
	if err != nil {
		t.Fatal(err)
	}
	var wrapper map[string]any
	if json.Unmarshal(raw, &wrapper) != nil {
		t.Fatalf("cache=%s", raw)
	}
	if wrapper["fetched_at"] != staleFetchedAt {
		t.Fatalf("fetched_at=%v", wrapper["fetched_at"])
	}
	models, _ := wrapper["models"].([]any)
	if len(models) != 1 {
		t.Fatalf("models=%v", models)
	}
}

func TestInvalidateSkipsMissingCatalog(t *testing.T) {
	ok, err := Invalidate(t.TempDir())
	if err != nil || ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}

func TestInvalidateStripsNativeEligibilityFromRoutedRows(t *testing.T) {
	home := t.TempDir()
	catalog := []byte(`{"models":[
		{"slug":"gpt-5.6-sol","available_in_plans":["plus"],"supported_in_api":true},
		{"slug":"google/gemini-2.5-pro","supported_in_api":false,"available_in_plans":["plus"],"availability_nux":{"title":"nux"},"upgrade":{"url":"https://chatgpt.com"},"minimal_client_version":"0.50.0"}
	]}`)
	if err := os.WriteFile(filepath.Join(home, "benes-catalog.json"), catalog, 0o600); err != nil {
		t.Fatal(err)
	}
	ok, err := Invalidate(home)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	raw, err := os.ReadFile(filepath.Join(home, "models_cache.json"))
	if err != nil {
		t.Fatal(err)
	}
	var wrapper struct {
		Models []map[string]any `json:"models"`
	}
	if json.Unmarshal(raw, &wrapper) != nil {
		t.Fatalf("cache=%s", raw)
	}
	bySlug := map[string]map[string]any{}
	for _, model := range wrapper.Models {
		slug, _ := model["slug"].(string)
		bySlug[slug] = model
	}
	native := bySlug["gpt-5.6-sol"]
	if native == nil {
		t.Fatalf("native missing: %s", raw)
	}
	if _, ok := native["available_in_plans"]; !ok {
		t.Fatal("native eligibility stripped during cache write")
	}
	routed := bySlug["google/gemini-2.5-pro"]
	if routed == nil {
		t.Fatalf("routed missing: %s", raw)
	}
	if routed["supported_in_api"] != true {
		t.Fatalf("supported_in_api=%v", routed["supported_in_api"])
	}
	for _, key := range []string{"available_in_plans", "availability_nux", "upgrade", "minimal_client_version"} {
		if _, ok := routed[key]; ok {
			t.Fatalf("cache kept %s on routed row", key)
		}
	}
}
