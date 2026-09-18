package outbound

import (
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
)

func TestEncoderProjectsMiniMaxReasoningDetails(t *testing.T) {
	enc, err := New("minimax/MiniMax-M2.7", Options{IDGenerator: func() string { return "chatcmpl-fixed" }, CreatedUnix: 1})
	if err != nil {
		t.Fatal(err)
	}
	index := 0
	meta := protocol.EncodeMiniMaxReasoningDetail(protocol.MiniMaxReasoningDetail{
		Type:   "reasoning.text",
		ID:     "rs_1",
		Format: "MiniMax-response-v1",
		Index:  &index,
	})
	for _, event := range []protocol.Event{
		{Type: protocol.EventReasoningRawDelta, Text: "The user", ProviderMetadata: meta},
		{Type: protocol.EventReasoningRawDelta, Text: " is asking", ProviderMetadata: meta},
		{Type: protocol.EventTextDelta, Text: "hello"},
		{Type: protocol.EventDone},
	} {
		if _, handleErr := enc.Handle(event); handleErr != nil {
			t.Fatalf("Handle(%s): %v", event.Type, handleErr)
		}
	}
	completion, err := enc.Completion()
	if err != nil {
		t.Fatal(err)
	}
	msg := completion["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)
	if msg["content"] != "hello" || msg["reasoning_content"] != "The user is asking" {
		t.Fatalf("message=%#v", msg)
	}
	details, ok := msg["reasoning_details"].([]any)
	if !ok || len(details) != 1 {
		t.Fatalf("details=%#v", msg["reasoning_details"])
	}
	detail := details[0].(map[string]any)
	if detail["id"] != "rs_1" || detail["text"] != "The user is asking" || detail["type"] != "reasoning.text" {
		t.Fatalf("detail=%#v", detail)
	}
}

func TestEncoderDoesNotCopyReasoningDetailsIntoContent(t *testing.T) {
	enc, err := New("minimax/MiniMax-M2.7", Options{IDGenerator: func() string { return "chatcmpl-fixed" }, CreatedUnix: 1})
	if err != nil {
		t.Fatal(err)
	}
	meta := protocol.EncodeMiniMaxReasoningDetail(protocol.MiniMaxReasoningDetail{ID: "rs_1", Text: ""})
	_, _ = enc.Handle(protocol.Event{Type: protocol.EventReasoningRawDelta, Text: "secret plan", ProviderMetadata: meta})
	_, _ = enc.Handle(protocol.Event{Type: protocol.EventTextDelta, Text: "answer"})
	_, _ = enc.Handle(protocol.Event{Type: protocol.EventDone})
	completion, err := enc.Completion()
	if err != nil {
		t.Fatal(err)
	}
	msg := completion["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)
	if msg["content"] != "answer" {
		t.Fatalf("content leaked reasoning: %#v", msg)
	}
}
