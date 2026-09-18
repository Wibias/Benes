package zcode

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMergeProviderOwnsOnlyBenesFragment(t *testing.T) {
	existing := []byte(`{
  "login": {"vendor": "zai", "token": "user-session"},
  "provider": {
    "zai": {"kind": "openai", "apiKey": "keep-me"},
    "benes": {"kind": "stale", "baseURL": "http://old"}
  }
}`)
	fragment := map[string]any{
		"kind":    "openai-compatible",
		"baseURL": "http://127.0.0.1:1455/v1",
		"apiKey":  placeholderKey,
	}
	got, err := MergeProvider(existing, fragment)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(got, &doc); err != nil {
		t.Fatal(err)
	}
	login, _ := doc["login"].(map[string]any)
	if login["vendor"] != "zai" || login["token"] != "user-session" {
		t.Fatalf("login mutated: %#v", doc["login"])
	}
	providers, _ := doc["provider"].(map[string]any)
	zai, _ := providers["zai"].(map[string]any)
	if zai["apiKey"] != "keep-me" {
		t.Fatalf("unrelated provider mutated: %#v", providers["zai"])
	}
	owned, _ := providers["benes"].(map[string]any)
	if owned["kind"] != "openai-compatible" || owned["baseURL"] != "http://127.0.0.1:1455/v1" {
		t.Fatalf("owned fragment=%#v", owned)
	}
	if owned["kind"] == "stale" {
		t.Fatal("stale benes fragment survived")
	}
}

func TestMergeProviderCreatesDocumentWhenMissing(t *testing.T) {
	fragment := map[string]any{"kind": "openai-compatible", "apiKey": placeholderKey}
	got, err := MergeProvider(nil, fragment)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(got, &doc); err != nil {
		t.Fatal(err)
	}
	providers, _ := doc["provider"].(map[string]any)
	if _, ok := providers["benes"]; !ok {
		t.Fatalf("missing owned fragment: %s", got)
	}
}

func TestMergeProviderPreservesKnownZCodeModelMetadata(t *testing.T) {
	existing := []byte(`{
  "provider": {
    "benes": {
      "kind": "stale",
      "baseURL": "http://127.0.0.1:1/v1/messages",
      "apiKey": "benes-zcode",
      "models": {
        "openai/gpt-5.6": {
          "name": "openai/gpt-5.6",
          "input": ["text"],
          "contextWindow": 8000,
          "reasoning": {"enabled": true, "variants": ["low", "high"], "defaultVariant": "low"},
          "limit": {"context": 8000, "output": 32000}
        }
      }
    }
  }
}`)
	fragment := map[string]any{
		"kind":    "openai-compatible",
		"baseURL": "http://127.0.0.1:1455/v1",
		"apiKey":  placeholderKey,
		"models": map[string]any{
			"openai/gpt-5.6": map[string]any{
				"name":          "openai/gpt-5.6",
				"input":         []string{"text"},
				"contextWindow": 128000,
			},
		},
	}
	got, err := MergeProvider(existing, fragment)
	if err != nil {
		t.Fatal(err)
	}
	owned := ownedFragment(t, got)
	if owned["kind"] != "openai-compatible" || owned["baseURL"] != "http://127.0.0.1:1455/v1" {
		t.Fatalf("benes-owned connection lost: %#v", owned)
	}
	row := ownedModel(t, owned, "openai/gpt-5.6")
	if row["contextWindow"] != float64(128000) {
		t.Fatalf("benes-owned contextWindow=%v", row["contextWindow"])
	}
	reasoning, _ := row["reasoning"].(map[string]any)
	if reasoning["enabled"] != true || reasoning["defaultVariant"] != "low" {
		t.Fatalf("reasoning lost: %#v", row["reasoning"])
	}
	limit, _ := row["limit"].(map[string]any)
	if limit["output"] != float64(32000) {
		t.Fatalf("limit.output lost: %#v", row["limit"])
	}
	if _, has := limit["context"]; has {
		t.Fatal("copied ZCode limit.context over Benes contextWindow")
	}
}

func TestMergeProviderPreservesLimitContextWhenBenesOmitsContextWindow(t *testing.T) {
	existing := []byte(`{
  "provider": {
    "benes": {
      "kind": "anthropic",
      "models": {
        "anthropic/claude": {
          "name": "anthropic/claude",
          "input": ["text"],
          "limit": {"context": 200000, "output": 8192}
        }
      }
    }
  }
}`)
	fragment := map[string]any{
		"kind":   "openai-compatible",
		"apiKey": placeholderKey,
		"models": map[string]any{
			"anthropic/claude": map[string]any{
				"name":  "anthropic/claude",
				"input": []string{"text"},
			},
		},
	}
	got, err := MergeProvider(existing, fragment)
	if err != nil {
		t.Fatal(err)
	}
	limit, _ := ownedModel(t, ownedFragment(t, got), "anthropic/claude")["limit"].(map[string]any)
	if limit["context"] != float64(200000) || limit["output"] != float64(8192) {
		t.Fatalf("limit=%#v", limit)
	}
}

func TestMergeProviderDropsUnknownModelAndProviderKeys(t *testing.T) {
	existing := []byte(`{
  "provider": {
    "benes": {
      "kind": "anthropic",
      "headers": {"X-Debug": "1"},
      "models": {
        "openai/gpt-5.6": {
          "name": "openai/gpt-5.6",
          "input": ["text"],
          "hack": true,
          "reasoning": {"enabled": true}
        }
      }
    }
  }
}`)
	fragment := map[string]any{
		"kind":   "openai-compatible",
		"apiKey": placeholderKey,
		"models": map[string]any{
			"openai/gpt-5.6": map[string]any{
				"name":  "openai/gpt-5.6",
				"input": []string{"text"},
			},
		},
	}
	got, err := MergeProvider(existing, fragment)
	if err != nil {
		t.Fatal(err)
	}
	owned := ownedFragment(t, got)
	if _, has := owned["headers"]; has {
		t.Fatal("preserved unknown provider key")
	}
	row := ownedModel(t, owned, "openai/gpt-5.6")
	if _, has := row["hack"]; has {
		t.Fatal("preserved unknown model key")
	}
	if _, has := row["reasoning"]; !has {
		t.Fatal("dropped known-safe reasoning")
	}
}

func TestMergeProviderDropsModelsBenesNoLongerOwns(t *testing.T) {
	existing := []byte(`{
  "provider": {
    "benes": {
      "models": {
        "gone/old": {"name": "gone/old", "reasoning": {"enabled": true}},
        "keep/new": {"name": "keep/new", "reasoning": {"enabled": true}}
      }
    }
  }
}`)
	fragment := map[string]any{
		"kind": "openai-compatible",
		"models": map[string]any{
			"keep/new": map[string]any{"name": "keep/new", "input": []string{"text"}},
		},
	}
	got, err := MergeProvider(existing, fragment)
	if err != nil {
		t.Fatal(err)
	}
	models, _ := ownedFragment(t, got)["models"].(map[string]any)
	if _, ok := models["gone/old"]; ok {
		t.Fatal("stale model membership survived")
	}
	row := ownedModel(t, ownedFragment(t, got), "keep/new")
	if _, has := row["reasoning"]; !has {
		t.Fatal("dropped remaining model's reasoning")
	}
}

func TestMergeProviderRejectsSecondConnectionEnvelope(t *testing.T) {
	existing := []byte(`{
  "provider": {
    "benes": {
      "kind": "openai-compatible",
      "options": {"baseURL": "http://127.0.0.1:9/v1", "apiKey": "other"}
    }
  }
}`)
	_, err := MergeProvider(existing, map[string]any{
		"kind":    "openai-compatible",
		"baseURL": "http://127.0.0.1:1455/v1",
		"apiKey":  placeholderKey,
	})
	if err == nil {
		t.Fatal("second connection envelope accepted")
	}
}

func TestMergeProviderRejectsUnparseableReasoning(t *testing.T) {
	existing := []byte(`{"provider":{"benes":{"models":{"m":{"reasoning":"high"}}}}}`)
	_, err := MergeProvider(existing, map[string]any{
		"kind": "openai-compatible",
		"models": map[string]any{
			"m": map[string]any{"name": "m", "input": []string{"text"}},
		},
	})
	if err == nil {
		t.Fatal("non-object reasoning accepted")
	}
}

func TestMergeProviderRejectsNonObjectModels(t *testing.T) {
	existing := []byte(`{"provider":{"benes":{"models":["nope"]}}}`)
	_, err := MergeProvider(existing, map[string]any{
		"kind": "openai-compatible",
		"models": map[string]any{
			"m": map[string]any{"name": "m", "input": []string{"text"}},
		},
	})
	if err == nil {
		t.Fatal("array models accepted")
	}
}

func ownedFragment(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	providers, _ := doc["provider"].(map[string]any)
	owned, _ := providers["benes"].(map[string]any)
	if owned == nil {
		t.Fatalf("missing provider.benes: %s", raw)
	}
	return owned
}

func ownedModel(t *testing.T, owned map[string]any, id string) map[string]any {
	t.Helper()
	models, _ := owned["models"].(map[string]any)
	row, _ := models[id].(map[string]any)
	if row == nil {
		t.Fatalf("missing model %s: %#v", id, owned["models"])
	}
	return row
}

func TestMergeProviderRejectsUnparseableDocument(t *testing.T) {
	if _, err := MergeProvider([]byte("{not-json"), map[string]any{"kind": "openai-compatible"}); err == nil {
		t.Fatal("unparseable document accepted")
	}
}

func TestMergeProviderRejectsNonObjectRoot(t *testing.T) {
	if _, err := MergeProvider([]byte(`["nope"]`), map[string]any{"kind": "openai-compatible"}); err == nil {
		t.Fatal("array root accepted")
	}
}

func TestMergeProviderRejectsNonObjectProvider(t *testing.T) {
	if _, err := MergeProvider([]byte(`{"provider":[]}`), map[string]any{"kind": "openai-compatible"}); err == nil {
		t.Fatal("array provider accepted")
	}
}

func TestMergeProviderPreservesSiblingRawBytes(t *testing.T) {
	existing := []byte(`{"note":"keep exactly","provider":{"zai":{"kind":"openai"}}}`)
	got, err := MergeProvider(existing, map[string]any{"kind": "openai-compatible"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `"note":"keep exactly"`) && !strings.Contains(string(got), `"note": "keep exactly"`) {
		t.Fatalf("sibling field lost: %s", got)
	}
}
