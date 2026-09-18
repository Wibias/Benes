package bridge

import (
	"encoding/json"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
)

func TestBridgeFunctionCallUsesExtraContentNotProviderMetadata(t *testing.T) {
	meta, _ := json.Marshal(map[string]any{"google": map[string]string{"thoughtSignature": "sig-real"}})
	b := New("m", Options{ResponseID: "resp_bridge_1", ID: func(prefix string) string { return prefix + "1" }})
	_ = b.Start()
	frames, err := b.Handle(protocol.Event{Type: protocol.EventToolCallStart, ID: "call_1", Name: "lookup"})
	if err != nil {
		t.Fatal(err)
	}
	_ = frames
	_, err = b.Handle(protocol.Event{Type: protocol.EventToolCallDelta, Arguments: "{}"})
	if err != nil {
		t.Fatal(err)
	}
	frames, err = b.Handle(protocol.Event{Type: protocol.EventToolCallEnd, ID: "call_1", ProviderMetadata: meta})
	if err != nil {
		t.Fatal(err)
	}
	var item map[string]any
	for _, f := range frames {
		if f.Name == "response.output_item.done" {
			item, _ = f.Data["item"].(map[string]any)
		}
	}
	if item == nil {
		t.Fatal("missing function_call item")
	}
	if _, ok := item["provider_metadata"]; ok {
		t.Fatalf("provider_metadata must not appear on public bridge wire: %#v", item)
	}
	if _, ok := item["providerMetadata"]; ok {
		t.Fatalf("providerMetadata must not appear on public bridge wire: %#v", item)
	}
	extra, ok := item["extra_content"]
	if !ok {
		t.Fatalf("expected extra_content: %#v", item)
	}
	raw, _ := json.Marshal(extra)
	var decoded map[string]any
	if json.Unmarshal(raw, &decoded) != nil {
		t.Fatalf("extra=%s", raw)
	}
	google, _ := decoded["google"].(map[string]any)
	if google["thought_signature"] != "sig-real" {
		t.Fatalf("extra_content=%s", raw)
	}
}
