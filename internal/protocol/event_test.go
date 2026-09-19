package protocol

import (
	"encoding/json"
	"testing"
)

func TestEventJSONRoundTripPreservesToolCallStart(t *testing.T) {
	metadata := json.RawMessage(`{"google":{"thoughtSignature":"sig"}}`)
	original := Event{
		Type:             EventToolCallStart,
		ID:               "call_123",
		Name:             "shell",
		ProviderMetadata: metadata,
	}

	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}

	var decoded Event
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}

	if decoded.Type != EventToolCallStart || decoded.ID != original.ID || decoded.Name != original.Name {
		t.Fatalf("tool-call identity changed: %#v", decoded)
	}
	if string(decoded.ProviderMetadata) != string(metadata) {
		t.Fatalf("provider metadata = %s, want %s", decoded.ProviderMetadata, metadata)
	}
}

func TestEventJSONRoundTripPreservesIncompleteOutcome(t *testing.T) {
	retryable := true
	endTurn := false
	original := Event{
		Type:      EventIncomplete,
		Reason:    "upstream_stall",
		Message:   "provider stopped producing bytes",
		Retryable: &retryable,
		EndTurn:   &endTurn,
		Usage: &Usage{
			InputTokens:           100,
			OutputTokens:          20,
			CachedInputTokens:     40,
			ReasoningOutputTokens: 5,
		},
		ProviderState: map[string]json.RawMessage{
			"cursor": json.RawMessage(`{"conversationId":"c-1"}`),
		},
	}

	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}

	var decoded Event
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}

	if decoded.Type != EventIncomplete || decoded.Reason != original.Reason || decoded.Message != original.Message {
		t.Fatalf("outcome changed: %#v", decoded)
	}
	if decoded.Retryable == nil || !*decoded.Retryable {
		t.Fatalf("retryable = %#v, want true", decoded.Retryable)
	}
	if decoded.Usage == nil || decoded.Usage.CachedInputTokens != 40 || decoded.Usage.ReasoningOutputTokens != 5 {
		t.Fatalf("usage changed: %#v", decoded.Usage)
	}
	if string(decoded.ProviderState["cursor"]) != `{"conversationId":"c-1"}` {
		t.Fatalf("provider state changed: %s", decoded.ProviderState["cursor"])
	}
}

func TestEventValidateRejectsIncompleteWithoutReason(t *testing.T) {
	event := Event{Type: EventIncomplete}
	if err := event.Validate(); err == nil {
		t.Fatal("Validate() accepted an incomplete event without a reason")
	}
}

func TestEventValidateRejectsToolCallStartWithoutIdentity(t *testing.T) {
	event := Event{Type: EventToolCallStart}
	if err := event.Validate(); err == nil {
		t.Fatal("Validate() accepted a tool-call start without id and name")
	}
}

func TestEventValidateAllowsHeartbeat(t *testing.T) {
	if err := (Event{Type: EventHeartbeat}).Validate(); err != nil {
		t.Fatalf("Validate() heartbeat: %v", err)
	}
}

func TestKiroOpaqueReasoningEventRequiresExactlyOneMember(t *testing.T) {
	if err := (Event{Type: EventKiroRedactedReasoning, Data: "opaque"}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (Event{Type: EventKiroRedactedReasoning, Signature: "sig"}).Validate(); err != nil {
		t.Fatal(err)
	}
	for _, event := range []Event{
		{Type: EventKiroRedactedReasoning},
		{Type: EventKiroRedactedReasoning, Data: "opaque", Signature: "sig"},
	} {
		if err := event.Validate(); err == nil {
			t.Fatalf("event=%#v accepted", event)
		}
	}
	if !KiroReasoningRedactedContent.Valid() || !KiroReasoningSignature.Valid() || KiroReasoningMember("unknown").Valid() {
		t.Fatal("Kiro reasoning member validation mismatch")
	}
}

