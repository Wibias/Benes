package codexcatalog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Wibias/Benes/internal/catalog"
	"github.com/Wibias/Benes/internal/router"
)

func TestWriteMergesNativeRowsAndSanitizesRoutedClones(t *testing.T) {
	home := t.TempDir()
	cache := map[string]any{
		"fetched_at": "2026-08-01T00:00:00Z",
		"models": []any{
			map[string]any{
				"slug":               "gpt-reserve",
				"base_instructions":  "Luna Reserve",
				"available_in_plans": []any{"plus"},
				"supported_in_api":   false,
			},
			map[string]any{
				"slug":               "gpt-5.6-sol",
				"base_instructions":  "You are a helpful coding assistant.",
				"available_in_plans": []any{"plus"},
				"availability_nux":   map[string]any{"title": "nux"},
				"upgrade":            map[string]any{"url": "https://chatgpt.com"},
				"context_window":     float64(272000),
			},
			map[string]any{
				"slug":               "google/old-stale",
				"available_in_plans": []any{"plus"},
				"supported_in_api":   false,
			},
		},
	}
	raw, err := json.Marshal(cache)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "models_cache.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}

	wrote, err := Write(home, []catalog.Model{
		{ID: "google/gemini-2.5-pro", Context: catalog.ContextWindow{Tokens: 1000000}},
	})
	if err != nil || !wrote {
		t.Fatalf("wrote=%v err=%v", wrote, err)
	}

	body, err := os.ReadFile(filepath.Join(home, "benes-catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Models []map[string]any `json:"models"`
	}
	if json.Unmarshal(body, &file) != nil {
		t.Fatalf("catalog=%s", body)
	}
	bySlug := map[string]map[string]any{}
	for _, model := range file.Models {
		bySlug[slugOf(model)] = model
	}
	if _, ok := bySlug["gpt-5.6-sol"]; !ok {
		t.Fatalf("native row dropped: %s", body)
	}
	if _, ok := bySlug["gpt-reserve"]; !ok {
		t.Fatalf("client reserve row dropped: %s", body)
	}
	if _, ok := bySlug["google/old-stale"]; ok {
		t.Fatal("stale routed row survived rebuild")
	}
	routed := bySlug["google/gemini-2.5-pro"]
	if routed == nil {
		t.Fatalf("routed row missing: %s", body)
	}
	if routed["supported_in_api"] != true {
		t.Fatalf("routed supported_in_api=%v", routed["supported_in_api"])
	}
	if routed["visibility"] != "list" {
		t.Fatalf("visibility=%v", routed["visibility"])
	}
	if routed["base_instructions"] != "You are a helpful coding assistant." {
		t.Fatalf("did not clone supported native template: %#v", routed["base_instructions"])
	}
	for _, key := range []string{"available_in_plans", "availability_nux", "upgrade", "minimal_client_version"} {
		if _, ok := routed[key]; ok {
			t.Fatalf("routed clone kept %s", key)
		}
	}
	native := bySlug["gpt-5.6-sol"]
	if _, ok := native["available_in_plans"]; !ok {
		t.Fatal("native eligibility stripped")
	}
}

func TestWriteProjectsUnambiguousAliasIntoDisplayNameWithoutChangingSlug(t *testing.T) {
	home := t.TempDir()
	aliases := router.AliasTable{
		Providers:       map[string]struct{}{"google-antigravity": {}},
		ProviderByAlias: map[string]string{"ga": "google-antigravity"},
		ModelByProvider: map[string]map[string]string{"google-antigravity": {"gemini": "gemini-3.7-flash"}},
	}
	wrote, err := WriteWithAliases(home, []catalog.Model{
		{ID: "google-antigravity/gemini-3.7-flash", DisplayName: ""},
	}, aliases)
	if err != nil || !wrote {
		t.Fatalf("wrote=%v err=%v", wrote, err)
	}
	body, err := os.ReadFile(filepath.Join(home, "benes-catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Models []map[string]any `json:"models"`
	}
	if json.Unmarshal(body, &file) != nil || len(file.Models) != 1 {
		t.Fatalf("catalog=%s", body)
	}
	entry := file.Models[0]
	if entry["slug"] != "google-antigravity/gemini-3.7-flash" {
		t.Fatalf("canonical slug changed: %#v", entry["slug"])
	}
	if entry["display_name"] != "ga/gemini" {
		t.Fatalf("display_name=%#v", entry["display_name"])
	}

	wrote, err = WriteWithAliases(home, []catalog.Model{
		{ID: "google-antigravity/gemini-3.7-flash", DisplayName: "Gemini Flash"},
	}, aliases)
	if err != nil || !wrote {
		t.Fatalf("explicit wrote=%v err=%v", wrote, err)
	}
	body, err = os.ReadFile(filepath.Join(home, "benes-catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	if json.Unmarshal(body, &file) != nil || len(file.Models) != 1 || file.Models[0]["display_name"] != "Gemini Flash" || file.Models[0]["slug"] != "google-antigravity/gemini-3.7-flash" {
		t.Fatalf("explicit catalog=%s", body)
	}
}

func TestWriteUsesSyntheticTemplateWhenNoSupportedNativeExists(t *testing.T) {
	home := t.TempDir()
	wrote, err := Write(home, []catalog.Model{{ID: "anthropic/claude-opus-5"}})
	if err != nil || !wrote {
		t.Fatalf("wrote=%v err=%v", wrote, err)
	}
	body, err := os.ReadFile(filepath.Join(home, "benes-catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Models []map[string]any `json:"models"`
	}
	if json.Unmarshal(body, &file) != nil || len(file.Models) != 1 {
		t.Fatalf("catalog=%s", body)
	}
	entry := file.Models[0]
	if entry["slug"] != "anthropic/claude-opus-5" || entry["supported_in_api"] != true || entry["visibility"] != "list" {
		t.Fatalf("entry=%#v", entry)
	}
	if _, ok := entry["available_in_plans"]; ok {
		t.Fatal("synthetic routed row carried plan gating")
	}
}

func TestWriteSkipsEmptyHomeAndEmptyProjection(t *testing.T) {
	ok, err := Write("", []catalog.Model{{ID: "google/gemini-2.5-pro"}})
	if err != nil || ok {
		t.Fatalf("empty home wrote=%v err=%v", ok, err)
	}
	home := t.TempDir()
	ok, err = Write(home, nil)
	if err != nil || ok {
		t.Fatalf("empty models wrote=%v err=%v", ok, err)
	}
	if _, err := os.Stat(filepath.Join(home, "benes-catalog.json")); !os.IsNotExist(err) {
		t.Fatalf("unexpected catalog err=%v", err)
	}
}
