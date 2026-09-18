package bridge

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
)

func TestBridge_AntigravityStartWithArgsAndSignatureSurvives(t *testing.T) {
	b := New("gemini", Options{})
	_ = b.Start()
	meta, _ := json.Marshal(map[string]any{"google": map[string]string{"thoughtSignature": "sig-live"}})
	args := `{"instruction":"CHILD_INSTRUCTION_SECRET"}`
	if _, err := b.Handle(protocol.Event{
		Type: protocol.EventToolCallStart, ID: "c9", Name: "benes_fabric_delegate",
		Arguments: args, ProviderMetadata: meta,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Handle(protocol.Event{
		Type: protocol.EventToolCallEnd, ID: "c9", Name: "benes_fabric_delegate",
		Arguments: args, ProviderMetadata: meta,
	}); err != nil {
		t.Fatal(err)
	}
	item := b.output[len(b.output)-1]
	if item["arguments"] != args {
		t.Fatalf("arguments=%v want %s", item["arguments"], args)
	}
	raw, _ := json.Marshal(item["extra_content"])
	if !strings.Contains(string(raw), "sig-live") {
		t.Fatalf("extra_content missing signature: %s", raw)
	}
}
