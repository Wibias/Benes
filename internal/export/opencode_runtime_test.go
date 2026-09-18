package export

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMergeOpencodeRuntimeReplacesOnlyOurProvider(t *testing.T) {
	block := map[string]any{"name": "Benes"}
	merged, err := MergeOpencodeRuntime(`{"provider":{"other":{"name":"keep"},"benes":{"name":"old"}},"mcp":{"a":true}}`, block)
	if err != nil {
		t.Fatal(err)
	}
	providers := merged["provider"].(map[string]any)
	if _, ok := providers["other"]; !ok {
		t.Fatal(providers)
	}
	raw, _ := json.Marshal(providers[ProviderID])
	if !strings.Contains(string(raw), "Benes") || strings.Contains(string(raw), "old") {
		t.Fatalf("%s", raw)
	}
	if merged["mcp"] == nil {
		t.Fatal(merged)
	}
}

func TestMergeOpencodeRuntimeRejectsNonObject(t *testing.T) {
	if _, err := MergeOpencodeRuntime(`[]`, map[string]any{}); err == nil {
		t.Fatal("expected error")
	}
}
