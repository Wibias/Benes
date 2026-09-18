package bridge

import (
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
)

func TestBridgePreservesStructuredToolIdentityAcrossFramesAndTerminalOutput(t *testing.T) {
	b := New("gpt", Options{
		ResponseID: "resp_1",
		CreatedAt:  1,
		ID:         func(string) string { return "fc_1" },
		Tools: []protocol.Tool{{
			Namespace: "exec",
			Name:      "exec",
			Parameters: map[string]any{
				"type": "object",
			},
		}},
	})
	_ = b.Start()

	added, err := b.Handle(protocol.Event{Type: protocol.EventToolCallStart, ID: "call_1", Namespace: "exec", Name: "exec"})
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 1 {
		t.Fatalf("added frames=%d", len(added))
	}
	assertBridgeToolIdentity(t, added[0].Data["item"], "exec", "exec")

	if _, err := b.Handle(protocol.Event{Type: protocol.EventToolCallDelta, ID: "call_1", Arguments: `{}`}); err != nil {
		t.Fatal(err)
	}
	done, err := b.Handle(protocol.Event{Type: protocol.EventToolCallEnd, ID: "call_1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(done) != 2 {
		t.Fatalf("done frames=%d", len(done))
	}
	assertBridgeToolIdentity(t, done[1].Data["item"], "exec", "exec")

	terminal, err := b.Handle(protocol.Event{Type: protocol.EventDone})
	if err != nil {
		t.Fatal(err)
	}
	if len(terminal) == 0 {
		t.Fatal("missing terminal frame")
	}
	response, ok := terminal[len(terminal)-1].Data["response"].(map[string]any)
	if !ok {
		t.Fatalf("terminal response=%#v", terminal[len(terminal)-1].Data["response"])
	}
	output, ok := response["output"].([]map[string]any)
	if !ok || len(output) != 1 {
		t.Fatalf("terminal output=%#v", response["output"])
	}
	assertBridgeToolIdentity(t, output[0], "exec", "exec")
}

func TestBridgeKeepsDefaultToolIdentityBare(t *testing.T) {
	b := New("gpt", Options{ResponseID: "resp_1", CreatedAt: 1, ID: func(string) string { return "fc_1" }})
	_ = b.Start()
	frames, err := b.Handle(protocol.Event{Type: protocol.EventToolCallStart, ID: "call_1", Name: "exec"})
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 1 {
		t.Fatalf("frames=%d", len(frames))
	}
	item, ok := frames[0].Data["item"].(map[string]any)
	if !ok {
		t.Fatalf("item=%#v", frames[0].Data["item"])
	}
	if item["name"] != "exec" {
		t.Fatalf("name=%#v", item["name"])
	}
	if _, exists := item["namespace"]; exists {
		t.Fatalf("default tool unexpectedly gained namespace=%#v", item["namespace"])
	}
}

func assertBridgeToolIdentity(t *testing.T, raw any, namespace, name string) {
	t.Helper()
	item, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("tool item=%#v", raw)
	}
	if item["name"] != name || item["namespace"] != namespace {
		t.Fatalf("tool identity namespace=%#v name=%#v, want namespace=%q name=%q", item["namespace"], item["name"], namespace, name)
	}
}
