package google

import "testing"

func TestThinkingLevelConsumesCatalogAndDoesNotInvent(t *testing.T) {
	if ThinkingLevel("high", nil) != "" {
		t.Fatal("empty catalog must not invent thinking")
	}
	if got := ThinkingLevel("high", []string{"low", "medium", "high"}); got != "high" {
		t.Fatalf("got=%s", got)
	}
	if got := ThinkingLevel("xhigh", []string{"low", "high"}); got != "high" {
		t.Fatalf("clamp=%s", got)
	}
	if ThinkingLevel("none", []string{"high"}) != "" {
		t.Fatal("none")
	}
}

func TestApplyThinkingWritesGenerationConfig(t *testing.T) {
	body := map[string]any{}
	ApplyThinking(body, "medium", []string{"low", "medium", "high"})
	gc := body["generationConfig"].(map[string]any)
	tc := gc["thinkingConfig"].(map[string]any)
	if tc["thinkingLevel"] != "medium" {
		t.Fatalf("%#v", body)
	}
}
