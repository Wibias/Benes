package outbound

import (
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
)

func TestEncoderBoundsThinkingSignatures(t *testing.T) {
	e, _ := New("m", Options{IDGenerator: func() string { return "msg" }, MaxTurnBytes: 8})
	_, _ = e.Handle(protocol.Event{Type: protocol.EventThinkingDelta, Thinking: "a"})
	frames, _ := e.Handle(protocol.Event{Type: protocol.EventThinkingSignature, Signature: "signature-too-large"})
	if got := names(frames); !equalStrings(got, []string{"error"}) {
		t.Fatalf("names=%#v", got)
	}
	if frames[0].Payload["error"].(map[string]any)["code"] != "translation_buffer_limit" {
		t.Fatalf("error=%#v", frames[0])
	}
}

func TestEncoderRejectsInvalidCompletedToolJSON(t *testing.T) {
	e, _ := New("m", Options{IDGenerator: func() string { return "msg" }})
	_, _ = e.Handle(protocol.Event{Type: protocol.EventToolCallStart, ID: "toolu_1", Name: "Read"})
	_, _ = e.Handle(protocol.Event{Type: protocol.EventToolCallDelta, ID: "toolu_1", Arguments: `{"x":`})
	frames, _ := e.Handle(protocol.Event{Type: protocol.EventToolCallEnd, ID: "toolu_1"})
	if got := names(frames); !equalStrings(got, []string{"error"}) {
		t.Fatalf("names=%#v", got)
	}
	if frames[0].Payload["error"].(map[string]any)["code"] != "upstream_tool_call_invalid" {
		t.Fatalf("error=%#v", frames[0])
	}
}

func TestEncoderRejectsDuplicateThinkingSignature(t *testing.T) {
	e, _ := New("m", Options{IDGenerator: func() string { return "msg" }})
	_, _ = e.Handle(protocol.Event{Type: protocol.EventThinkingSignature, Signature: "one"})
	frames, _ := e.Handle(protocol.Event{Type: protocol.EventThinkingSignature, Signature: "two"})
	if got := names(frames); !equalStrings(got, []string{"error"}) {
		t.Fatalf("names=%#v", got)
	}
}

func TestEncoderUnknownIncompleteFailsInsteadOfInventingStopReason(t *testing.T) {
	e, _ := New("m", Options{IDGenerator: func() string { return "msg" }})
	frames, _ := e.Handle(protocol.Event{Type: protocol.EventIncomplete, Reason: "provider_abort"})
	if got := names(frames); !equalStrings(got, []string{"error"}) {
		t.Fatalf("names=%#v", got)
	}
	if frames[0].Payload["error"].(map[string]any)["code"] != "upstream_incomplete" {
		t.Fatalf("error=%#v", frames[0])
	}
}

func TestEncoderDoneWithoutContentStillProducesValidLifecycle(t *testing.T) {
	e, _ := New("m", Options{IDGenerator: func() string { return "msg" }})
	frames, _ := e.Handle(protocol.Event{Type: protocol.EventDone})
	want := []string{"message_start", "ping", "message_delta", "message_stop"}
	if got := names(frames); !equalStrings(got, want) {
		t.Fatalf("names=%#v", got)
	}
	message, err := e.Message()
	if err != nil {
		t.Fatal(err)
	}
	if len(message["content"].([]any)) != 0 || message["stop_reason"] != "end_turn" {
		t.Fatalf("message=%#v", message)
	}
}

func TestEncoderBoundsSyntheticThinkingSignature(t *testing.T) {
	e, _ := New("m", Options{
		IDGenerator:        func() string { return "msg" },
		SignatureGenerator: func() string { return "signature-too-large" },
		MaxTurnBytes:       8,
	})
	_, _ = e.Handle(protocol.Event{Type: protocol.EventThinkingDelta, Thinking: "a"})
	frames, _ := e.Handle(protocol.Event{Type: protocol.EventTextDelta, Text: "b"})
	if got := names(frames); !equalStrings(got, []string{"error"}) {
		t.Fatalf("names=%#v", got)
	}
	if frames[0].Payload["error"].(map[string]any)["code"] != "translation_buffer_limit" {
		t.Fatalf("error=%#v", frames[0])
	}
}
