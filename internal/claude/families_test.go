package claude

import (
	"testing"
)

func TestReadFamilyRoutesPrefersTierModelsOverLegacyModelMap(t *testing.T) {
	got := ReadFamilyRoutes(map[string]any{
		"tierModels": map[string]any{"opus": "canonical-opus", "sonnet": "canonical-sonnet"},
		"modelMap": map[string]any{
			"opus":                   "legacy-opus",
			"haiku":                  "legacy-haiku",
			"claude-3-opus-20240229": "intercepted",
			"fable":                  "legacy-fable",
		},
	})
	if got.Opus != "canonical-opus" || got.Sonnet != "canonical-sonnet" {
		t.Fatalf("canonical must win when present: %+v", got)
	}
	if got.Haiku != "legacy-haiku" || got.Fable != "legacy-fable" {
		t.Fatalf("missing canonical families must fall back to modelMap: %+v", got)
	}
}

func TestReadFamilyRoutesIgnoresArbitraryModelMapKeys(t *testing.T) {
	got := ReadFamilyRoutes(map[string]any{
		"modelMap": map[string]any{"claude-3": "intercepted", "opus": "gpt-5.6-sol"},
	})
	if got.Opus != "gpt-5.6-sol" || got.Sonnet != "" || got.Haiku != "" || got.Fable != "" {
		t.Fatalf("%+v", got)
	}
}

func TestWriteFamilyRoutesStoresCanonicalAndDropsModelMap(t *testing.T) {
	block := map[string]any{
		"modelMap": map[string]any{"opus": "old", "claude-3": "intercepted"},
	}
	WriteFamilyRoutes(block, FamilyRoutes{Opus: "gpt-5.6-sol", Haiku: "gpt-5.6-mini"})
	if _, ok := block["modelMap"]; ok {
		t.Fatalf("writable modelMap must not remain: %v", block)
	}
	got := ReadFamilyRoutes(block)
	if got.Opus != "gpt-5.6-sol" || got.Haiku != "gpt-5.6-mini" || got.Sonnet != "" {
		t.Fatalf("%+v", got)
	}
	WriteFamilyRoutes(block, FamilyRoutes{})
	if _, ok := block["tierModels"]; ok {
		t.Fatalf("empty families must omit tierModels: %v", block)
	}
}

func TestParseFamilyObjectRejectsNonObjectAndNonStringFamilies(t *testing.T) {
	if _, err := ParseFamilyObject([]any{"opus"}); err == nil {
		t.Fatal("expected object error")
	}
	if _, err := ParseFamilyObject(map[string]any{"opus": 1}); err == nil {
		t.Fatal("expected string error")
	}
	got, err := ParseFamilyObject(map[string]any{
		"opus":   " gpt-5.6-sol ",
		"sonnet": "",
		"extra":  "ignored",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Opus != "gpt-5.6-sol" || got.Sonnet != "" {
		t.Fatalf("%+v", got)
	}
}

func TestProjectFamilyMapOmitsUnsetRoutes(t *testing.T) {
	got := ProjectFamilyMap(FamilyRoutes{Sonnet: "gpt-5.6-sol"})
	if len(got) != 1 || got["sonnet"] != "gpt-5.6-sol" {
		t.Fatalf("%v", got)
	}
	if empty := ProjectFamilyMap(FamilyRoutes{}); len(empty) != 0 {
		t.Fatalf("%v", empty)
	}
}
