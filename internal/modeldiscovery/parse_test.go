package modeldiscovery

import (
	"encoding/json"
	"testing"
)

func TestParseOpenAIModelList(t *testing.T) {
	ids, ok := ParseModelIDs([]byte(`{"data":[{"id":"openai/gpt-5.6-sol"},{"id":"x-ai/grok-5"},{"id":""}]}`))
	if !ok {
		t.Fatal("expected a complete list")
	}
	if len(ids) != 2 || ids[0] != "openai/gpt-5.6-sol" || ids[1] != "x-ai/grok-5" {
		t.Fatalf("ids=%v", ids)
	}
}

func TestParseCatalogIDsReadsCodexSlugsAndSkipsHidden(t *testing.T) {
	ids, ok := ParseCatalogIDs([]byte(`{
		"models":[
			{"slug":"gpt-5.4","visibility":"show"},
			{"slug":"secret-lab","visibility":"hide"},
			{"slug":""},
			{"id":"gpt-5.4-mini"}
		]
	}`))
	if !ok {
		t.Fatal("expected a complete Codex list")
	}
	if len(ids) != 2 || ids[0] != "gpt-5.4" || ids[1] != "gpt-5.4-mini" {
		t.Fatalf("ids=%v", ids)
	}
}

func TestParseOpenAIModelListRejectsGarbage(t *testing.T) {
	if _, ok := ParseModelIDs([]byte(`{"error":"nope"}`)); ok {
		t.Fatal("missing data must be incomplete")
	}
	if _, ok := ParseModelIDs([]byte(`not-json`)); ok {
		t.Fatal("garbage must be incomplete")
	}
}

func TestParseCatalogReadsContextLength(t *testing.T) {
	entries, ok := ParseCatalog([]byte(`{
		"object":"list",
		"data":[
			{"id":"claude-sonnet-5","context_length":1000000},
			{"id":"gpt-5.6-sol","context_length":1050000,"max_input_tokens":922000},
			{"id":""}
		]
	}`))
	if !ok {
		t.Fatal("expected a complete list")
	}
	if len(entries) != 2 {
		t.Fatalf("entries=%v", entries)
	}
	if entries[0].ID != "claude-sonnet-5" || entries[0].ContextWindow != 1000000 {
		t.Fatalf("sonnet=%#v", entries[0])
	}
	if entries[1].ID != "gpt-5.6-sol" || entries[1].ContextWindow != 1050000 || entries[1].MaxInput != 922000 {
		t.Fatalf("sol=%#v", entries[1])
	}
}

func TestParseCatalogReadsGeminiNameAndInputTokenLimit(t *testing.T) {
	entries, ok := ParseCatalog([]byte(`{
		"models":[
			{"name":"models/gemini-3.7-flash","inputTokenLimit":1048576},
			{"id":"gemini-3.1-pro","contextWindow":1000000}
		]
	}`))
	if !ok {
		t.Fatal("expected a complete Gemini list")
	}
	if len(entries) != 2 || entries[0].ID != "gemini-3.7-flash" || entries[0].ContextWindow != 1048576 {
		t.Fatalf("entries=%#v", entries)
	}
	if entries[1].ID != "gemini-3.1-pro" || entries[1].ContextWindow != 1000000 {
		t.Fatalf("pro=%#v", entries[1])
	}
}

func TestParseStoredModelsReadsStringAndObjectForms(t *testing.T) {
	strings := ParseStoredModels(json.RawMessage(`["claude-sonnet-5","gpt-5.6-sol"]`))
	if len(strings) != 2 || strings[0].ContextWindow != 0 {
		t.Fatalf("strings=%#v", strings)
	}
	objects := ParseStoredModels(json.RawMessage(`[{"id":"claude-sonnet-5","contextWindow":1000000}]`))
	if len(objects) != 1 || objects[0].ID != "claude-sonnet-5" || objects[0].ContextWindow != 1000000 {
		t.Fatalf("objects=%#v", objects)
	}
}

func TestParseProviderCatalogFillsSeedWindows(t *testing.T) {
	entries := ParseProviderCatalog(json.RawMessage(`{
		"models":["claude-sonnet-5","gpt-5.6-sol"],
		"modelContextWindows":{"claude-sonnet-5":1000000,"gpt-5.6-sol":1050000},
		"modelMaxInputTokens":{"gpt-5.6-sol":922000}
	}`))
	if len(entries) != 2 {
		t.Fatalf("entries=%#v", entries)
	}
	if entries[0].ContextWindow != 1000000 || entries[1].ContextWindow != 1050000 || entries[1].MaxInput != 922000 {
		t.Fatalf("windows=%#v", entries)
	}
}

func TestParseCatalogPrefersMaxContextWindowAndLiftsGPT56(t *testing.T) {
	entries, ok := ParseCatalog([]byte(`{
		"models":[
			{"slug":"gpt-5.6","context_window":272000,"max_context_window":272000},
			{"slug":"gpt-5.4","context_window":272000,"max_context_window":272000}
		]
	}`))
	if !ok {
		t.Fatal("expected a complete Codex list")
	}
	if len(entries) != 2 {
		t.Fatalf("entries=%#v", entries)
	}
	if entries[0].ID != "gpt-5.6" || entries[0].ContextWindow != 1_050_000 || entries[0].MaxInput != 272_000 {
		t.Fatalf("gpt-5.6=%#v", entries[0])
	}
	if entries[1].ID != "gpt-5.4" || entries[1].ContextWindow != 272_000 || entries[1].MaxInput != 0 {
		t.Fatalf("gpt-5.4=%#v", entries[1])
	}
}

func TestParseProviderCatalogSkipsOperatorPresets(t *testing.T) {
	entries := ParseProviderCatalog(json.RawMessage(`{
		"models":["gpt-5.6","claude-sonnet-5"],
		"modelContextWindows":{"gpt-5.6":64000,"claude-sonnet-5":1000000}
	}`))
	if len(entries) != 2 {
		t.Fatalf("entries=%#v", entries)
	}
	if entries[0].ContextWindow != 0 {
		t.Fatalf("operator preset leaked into advertised: %#v", entries[0])
	}
	if entries[1].ContextWindow != 1000000 {
		t.Fatalf("seed window dropped: %#v", entries[1])
	}
}

func TestParseProviderCatalogLiftsStoredGPT56Windows(t *testing.T) {
	entries := ParseProviderCatalog(json.RawMessage(`{
		"models":[{"id":"gpt-5.6","contextWindow":272000}]
	}`))
	if len(entries) != 1 {
		t.Fatalf("entries=%#v", entries)
	}
	if entries[0].ContextWindow != 1_050_000 || entries[0].MaxInput != 272_000 {
		t.Fatalf("stored gpt-5.6=%#v", entries[0])
	}
}

func TestParseCatalogReadsCopilotNestedContextLimits(t *testing.T) {
	entries, ok := ParseCatalog([]byte(`{
		"data":[
			{
				"id":"gpt-4.1",
				"capabilities":{"limits":{"max_context_window_tokens":1048576,"max_prompt_tokens":128000,"max_output_tokens":32768}}
			},
			{
				"id":"claude-sonnet-4",
				"capabilities":{"limits":{"max_context_window_tokens":200000,"max_prompt_tokens":168000,"max_output_tokens":32000}}
			},
			{"id":"gpt-4o"},
			{
				"id":"gpt-5.6-luna",
				"capabilities":{"limits":{"max_context_window_tokens":272000,"max_prompt_tokens":128000}}
			},
			{
				"id":"broken-negative",
				"capabilities":{"limits":{"max_context_window_tokens":-1,"max_prompt_tokens":128000}}
			},
			{
				"id":"broken-overflow",
				"capabilities":{"limits":{"max_context_window_tokens":100000001,"max_prompt_tokens":128000}}
			}
		]
	}`))
	if !ok {
		t.Fatal("expected Copilot data envelope to parse")
	}
	if len(entries) != 6 {
		t.Fatalf("entries=%#v", entries)
	}
	if entries[0].ID != "gpt-4.1" || entries[0].ContextWindow != 1048576 || entries[0].MaxInput != 128000 {
		t.Fatalf("gpt-4.1=%#v", entries[0])
	}
	if entries[1].ID != "claude-sonnet-4" || entries[1].ContextWindow != 200000 || entries[1].MaxInput != 168000 {
		t.Fatalf("claude-sonnet-4=%#v", entries[1])
	}
	if entries[2].ID != "gpt-4o" || entries[2].ContextWindow != 0 || entries[2].MaxInput != 0 {
		t.Fatalf("absent nested field should stay unknown: %#v", entries[2])
	}
	if entries[3].ID != "gpt-5.6-luna" || entries[3].ContextWindow != 272000 || entries[3].MaxInput != 128000 {
		t.Fatalf("nested Copilot gpt-5.6 window must not lift to 1.05M: %#v", entries[3])
	}
	if entries[4].ContextWindow != 0 || entries[5].ContextWindow != 0 {
		t.Fatalf("invalid nested windows must stay unknown: %#v %#v", entries[4], entries[5])
	}
}

func TestCatalogIDPrefixesProviderOnce(t *testing.T) {
	if CatalogID("openrouter", "x-ai/grok-5") != "openrouter/x-ai/grok-5" {
		t.Fatal(CatalogID("openrouter", "x-ai/grok-5"))
	}
	if CatalogID("openrouter", "openrouter/x-ai/grok-5") != "openrouter/x-ai/grok-5" {
		t.Fatal("double prefix")
	}
}
