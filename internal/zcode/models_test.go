package zcode

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Wibias/Benes/internal/catalog"
	"github.com/Wibias/Benes/internal/managedfs"
)

func TestModelSelectorsCarryAuthoritativeContextAndTextImageOnly(t *testing.T) {
	got := ModelSelectors([]catalog.Model{
		{
			ID:           "openai/gpt-5.6",
			Context:      catalog.ContextWindow{Tokens: 200000, Source: catalog.ContextDiscovered},
			Vision:       catalog.CapabilityTrue,
			Availability: catalog.Availability{Selectable: true},
		},
		{
			ID:           "anthropic/claude",
			Context:      catalog.ContextWindow{Tokens: catalog.ConservativeContextWindow, Source: catalog.ContextConservativeDefault},
			Vision:       catalog.CapabilityUnknown,
			Availability: catalog.Availability{Selectable: true},
		},
		{
			ID:           "gone/credit",
			Context:      catalog.ContextWindow{Tokens: 100000, Source: catalog.ContextOperator},
			Availability: catalog.Availability{Selectable: false, Reason: "no_credit"},
		},
	})
	vision, ok := got["openai/gpt-5.6"].(map[string]any)
	if !ok {
		t.Fatalf("missing vision model: %#v", got)
	}
	if vision["name"] != "openai/gpt-5.6" {
		t.Fatalf("name=%v", vision["name"])
	}
	if vision["contextWindow"] != 200000 {
		t.Fatalf("contextWindow=%v", vision["contextWindow"])
	}
	if _, hasOutput := vision["maxTokens"]; hasOutput {
		t.Fatal("invented maxTokens")
	}
	if _, hasLimit := vision["limit"]; hasLimit {
		t.Fatal("invented limit/output budget")
	}
	if _, hasOutput := vision["output"]; hasOutput {
		t.Fatal("invented output budget")
	}
	input, _ := vision["input"].([]string)
	if len(input) != 2 || input[0] != "text" || input[1] != "image" {
		// JSON round-trip uses []any; accept either
		raw, _ := vision["input"].([]any)
		if len(raw) != 2 || raw[0] != "text" || raw[1] != "image" {
			t.Fatalf("input=%#v", vision["input"])
		}
	}

	text, ok := got["anthropic/claude"].(map[string]any)
	if !ok {
		t.Fatalf("missing text-floor model: %#v", got)
	}
	if _, has := text["contextWindow"]; has {
		t.Fatalf("conservative window leaked: %#v", text)
	}
	floor, _ := text["input"].([]string)
	if len(floor) != 1 || floor[0] != "text" {
		raw, _ := text["input"].([]any)
		if len(raw) != 1 || raw[0] != "text" {
			t.Fatalf("text floor=%#v", text["input"])
		}
	}

	if _, ok := got["gone/credit"]; ok {
		t.Fatal("unselectable model included")
	}
}

func TestModelSelectorsDropIncompatibleModalities(t *testing.T) {
	if got := inputForZCode([]string{"audio"}); got != nil {
		t.Fatalf("audio-only kept: %#v", got)
	}
	if got := inputForZCode([]string{"video"}); got != nil {
		t.Fatalf("video-only kept: %#v", got)
	}
	got := inputForZCode([]string{"text", "audio", "image"})
	if len(got) != 2 || got[0] != "text" || got[1] != "image" {
		t.Fatalf("filtered=%#v", got)
	}
	if got := inputForZCode(nil); len(got) != 1 || got[0] != "text" {
		t.Fatalf("unknown floor=%#v", got)
	}
}

func TestApplyWithModelsWritesSelectorsOnOwnedFragment(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ZCODE_DATA_DIR", dir)
	models := []catalog.Model{{
		ID:           "openai/gpt-5.6",
		Context:      catalog.ContextWindow{Tokens: 128000, Source: catalog.ContextStatic},
		Vision:       catalog.CapabilityFalse,
		Availability: catalog.Availability{Selectable: true},
	}}
	if err := ApplyWithModels(context.Background(), "http://127.0.0.1:1455", models); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "v2", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	providers, _ := doc["provider"].(map[string]any)
	owned, _ := providers["benes"].(map[string]any)
	selectors, _ := owned["models"].(map[string]any)
	row, _ := selectors["openai/gpt-5.6"].(map[string]any)
	if row["contextWindow"] != float64(128000) {
		t.Fatalf("row=%#v", row)
	}
	if _, has := row["maxTokens"]; has {
		t.Fatal("invented maxTokens on disk")
	}
}

func TestApplyWithModelsPreservesZCodeRuntimeMetadataAfterPeerEdit(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ZCODE_DATA_DIR", dir)
	models := []catalog.Model{{
		ID:           "openai/gpt-5.6",
		Context:      catalog.ContextWindow{Tokens: 128000, Source: catalog.ContextStatic},
		Vision:       catalog.CapabilityFalse,
		Availability: catalog.Availability{Selectable: true},
	}}
	if err := ApplyWithModels(context.Background(), "http://127.0.0.1:1455", models); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "v2", "config.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	providers, _ := doc["provider"].(map[string]any)
	owned, _ := providers["benes"].(map[string]any)
	selectors, _ := owned["models"].(map[string]any)
	row, _ := selectors["openai/gpt-5.6"].(map[string]any)
	row["reasoning"] = map[string]any{"enabled": true, "defaultVariant": "high"}
	row["limit"] = map[string]any{"output": 32000.0}
	selectors["openai/gpt-5.6"] = row
	owned["models"] = selectors
	providers["benes"] = owned
	doc["provider"] = providers
	updated, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, updated, 0o600); err != nil {
		t.Fatal(err)
	}
	models[0].Context.Tokens = 200000
	if err := ApplyWithModels(context.Background(), "http://127.0.0.1:23100", models); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	providers, _ = doc["provider"].(map[string]any)
	owned, _ = providers["benes"].(map[string]any)
	if owned["baseURL"] != "http://127.0.0.1:23100/v1" {
		t.Fatalf("benes-owned baseURL=%v", owned["baseURL"])
	}
	selectors, _ = owned["models"].(map[string]any)
	row, _ = selectors["openai/gpt-5.6"].(map[string]any)
	if row["contextWindow"] != float64(200000) {
		t.Fatalf("benes-owned contextWindow=%v", row["contextWindow"])
	}
	reasoning, _ := row["reasoning"].(map[string]any)
	if reasoning["defaultVariant"] != "high" {
		t.Fatalf("reasoning lost: %#v", row["reasoning"])
	}
	limit, _ := row["limit"].(map[string]any)
	if limit["output"] != float64(32000) {
		t.Fatalf("limit.output lost: %#v", row["limit"])
	}
	coord, err := managedfs.New(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if got := coord.Classify(); got != managedfs.OwnershipCurrent {
		t.Fatalf("ownership=%s", got)
	}
}
