package codexcatalog

import (
	"testing"
)

func TestSanitizeRoutedDropsNativeEligibilityAndForcesAPI(t *testing.T) {
	entry := map[string]any{
		"slug":                   "google/gemini-2.5-pro",
		"supported_in_api":       false,
		"available_in_plans":     []any{"plus"},
		"minimal_client_version": "0.50.0",
		"availability_nux":       map[string]any{"title": "upgrade"},
		"upgrade":                map[string]any{"url": "https://chatgpt.com"},
		"context_window":         float64(128000),
	}
	SanitizeRouted(entry)
	if entry["supported_in_api"] != true {
		t.Fatalf("supported_in_api=%v", entry["supported_in_api"])
	}
	for _, key := range []string{"available_in_plans", "minimal_client_version", "availability_nux", "upgrade"} {
		if _, ok := entry[key]; ok {
			t.Fatalf("routed row kept %s: %#v", key, entry[key])
		}
	}
	if entry["context_window"] != float64(128000) {
		t.Fatalf("unrelated field rewritten: %#v", entry["context_window"])
	}
}

func TestSanitizeRoutedLeavesNativeEligibilityInPlace(t *testing.T) {
	entry := map[string]any{
		"slug":               "gpt-5.6-sol",
		"supported_in_api":   true,
		"available_in_plans": []any{"plus"},
		"availability_nux":   map[string]any{"title": "nux"},
	}
	SanitizeRouted(entry)
	if entry["supported_in_api"] != true {
		t.Fatalf("native supported_in_api=%v", entry["supported_in_api"])
	}
	if _, ok := entry["available_in_plans"]; !ok {
		t.Fatal("native available_in_plans stripped")
	}
	if _, ok := entry["availability_nux"]; !ok {
		t.Fatal("native availability_nux stripped")
	}
}

func TestFindSupportedNativeTemplateSkipsReserveAndUnknownBareRows(t *testing.T) {
	reserve := map[string]any{
		"slug":               "gpt-reserve",
		"base_instructions":  "Reserve fallback",
		"available_in_plans": []any{"plus"},
	}
	unknown := map[string]any{
		"slug":              "gpt-brand-new-unreleased",
		"base_instructions": "You are a helpful coding assistant.",
	}
	routed := map[string]any{
		"slug":              "google/gemini-2.5-pro",
		"base_instructions": "Routed via Benes",
	}
	if got := FindSupportedNativeTemplate([]map[string]any{reserve, unknown, routed}); got != nil {
		t.Fatalf("picked unsupported template %#v", got)
	}
	supported := map[string]any{
		"slug":               "gpt-5.6-sol",
		"base_instructions":  "You are a helpful coding assistant.",
		"available_in_plans": []any{"plus"},
	}
	got := FindSupportedNativeTemplate([]map[string]any{reserve, supported, unknown})
	if got == nil || got["slug"] != "gpt-5.6-sol" {
		t.Fatalf("got=%#v", got)
	}
	got["slug"] = "mutated"
	if supported["slug"] != "gpt-5.6-sol" {
		t.Fatal("template clone leaked into the source row")
	}
}
