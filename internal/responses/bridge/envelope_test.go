package bridge

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	benesreasoning "github.com/Wibias/Benes/internal/responses/reasoning"
)

func newReplayTestBridge(hide bool) *Bridge {
	n := 0
	return New("test-model", Options{
		ResponseID:          "resp_test",
		CreatedAt:           1,
		HideThinkingSummary: hide,
		ID: func(prefix string) string {
			n++
			return prefix + string(rune('0'+n))
		},
	})
}

func handleAll(t *testing.T, b *Bridge, events ...protocol.Event) []Frame {
	t.Helper()
	frames := append([]Frame{}, b.Start()...)
	for _, event := range events {
		got, err := b.Handle(event)
		if err != nil {
			t.Fatalf("Handle(%s): %v", event.Type, err)
		}
		frames = append(frames, got...)
	}
	return frames
}

func outputDoneItems(frames []Frame) []map[string]any {
	var items []map[string]any
	for _, frame := range frames {
		if frame.Name != "response.output_item.done" {
			continue
		}
		if item, ok := frame.Data["item"].(map[string]any); ok {
			items = append(items, item)
		}
	}
	return items
}

func mustEnvelope(t *testing.T, item map[string]any) benesreasoning.Envelope {
	t.Helper()
	raw, ok := item["encrypted_content"].(string)
	if !ok || raw == "" {
		t.Fatalf("missing encrypted_content in %#v", item)
	}
	envelope, ok := benesreasoning.Decode(raw)
	if !ok {
		t.Fatalf("invalid Envelope envelope %q", raw)
	}
	return envelope
}

func TestVisibleReasoningCarriesSignatureAndRedactions(t *testing.T) {
	b := newReplayTestBridge(false)
	frames := handleAll(t, b,
		protocol.Event{Type: protocol.EventThinkingDelta, Thinking: "visible"},
		protocol.Event{Type: protocol.EventRedactedThinking, Data: "red-1"},
		protocol.Event{Type: protocol.EventThinkingSignature, Signature: "sig-old"},
		protocol.Event{Type: protocol.EventThinkingSignature, Signature: "sig-new"},
		protocol.Event{Type: protocol.EventRedactedThinking, Data: "red-2"},
		protocol.Event{Type: protocol.EventDone},
	)
	items := outputDoneItems(frames)
	if len(items) != 1 {
		t.Fatalf("got %d done items, want 1", len(items))
	}
	envelope := mustEnvelope(t, items[0])
	if envelope.Signature != "sig-new" {
		t.Fatalf("signature=%q want sig-new", envelope.Signature)
	}
	if len(envelope.Redacted) != 2 || envelope.Redacted[0] != "red-1" || envelope.Redacted[1] != "red-2" {
		t.Fatalf("redacted=%q", envelope.Redacted)
	}
	if envelope.Text != "" {
		t.Fatalf("visible reasoning leaked into txt: %q", envelope.Text)
	}
}

func TestRedactedOnlyTurnEmitsEnvelopeOnlyReasoning(t *testing.T) {
	b := newReplayTestBridge(false)
	frames := handleAll(t, b,
		protocol.Event{Type: protocol.EventRedactedThinking, Data: "opaque"},
		protocol.Event{Type: protocol.EventDone},
	)
	items := outputDoneItems(frames)
	if len(items) != 1 {
		t.Fatalf("got %d done items, want 1", len(items))
	}
	if got := items[0]["summary"].([]any); len(got) != 0 {
		t.Fatalf("summary=%#v, want empty", got)
	}
	envelope := mustEnvelope(t, items[0])
	if len(envelope.Redacted) != 1 || envelope.Redacted[0] != "opaque" {
		t.Fatalf("redacted=%q", envelope.Redacted)
	}
}

func TestHiddenThinkingNeverEmitsVisibleSummaryAndPreservesSignedText(t *testing.T) {
	b := newReplayTestBridge(true)
	frames := handleAll(t, b,
		protocol.Event{Type: protocol.EventThinkingDelta, Thinking: "secret"},
		protocol.Event{Type: protocol.EventThinkingSignature, Signature: "sig"},
		protocol.Event{Type: protocol.EventDone},
	)
	for _, frame := range frames {
		if frame.Name == "response.reasoning_summary_text.delta" || frame.Name == "response.reasoning_summary_text.done" {
			t.Fatalf("hidden reasoning emitted visible frame %s", frame.Name)
		}
	}
	items := outputDoneItems(frames)
	if len(items) != 1 {
		t.Fatalf("got %d done items, want 1", len(items))
	}
	envelope := mustEnvelope(t, items[0])
	if envelope.Signature != "sig" || envelope.Text != "secret" {
		t.Fatalf("envelope=%+v", envelope)
	}
}

func TestHiddenRawReasoningFlushesBeforeTextAsTxtOnlyEnvelope(t *testing.T) {
	b := newReplayTestBridge(true)
	frames := handleAll(t, b,
		protocol.Event{Type: protocol.EventReasoningRawDelta, Text: "raw-secret"},
		protocol.Event{Type: protocol.EventTextDelta, Text: "answer"},
		protocol.Event{Type: protocol.EventDone},
	)
	items := outputDoneItems(frames)
	if len(items) != 2 {
		t.Fatalf("got %d done items, want 2", len(items))
	}
	if items[0]["type"] != "reasoning" || items[1]["type"] != "message" {
		t.Fatalf("item order=%v,%v", items[0]["type"], items[1]["type"])
	}
	envelope := mustEnvelope(t, items[0])
	if envelope.Text != "raw-secret" || envelope.Signature != "" || len(envelope.Redacted) != 0 {
		t.Fatalf("envelope=%+v", envelope)
	}
}

func TestKiroEnvelopeLandsAfterAssistantMessage(t *testing.T) {
	b := newReplayTestBridge(false)
	frames := handleAll(t, b,
		protocol.Event{Type: protocol.EventTextDelta, Text: "answer"},
		protocol.Event{Type: protocol.EventKiroRedactedReasoning, Data: "kms-1"},
		protocol.Event{Type: protocol.EventKiroRedactedReasoning, Data: "kms-2"},
		protocol.Event{Type: protocol.EventDone},
	)
	items := outputDoneItems(frames)
	if len(items) != 2 {
		t.Fatalf("got %d done items, want 2", len(items))
	}
	if items[0]["type"] != "message" || items[1]["type"] != "reasoning" {
		t.Fatalf("item order=%v,%v", items[0]["type"], items[1]["type"])
	}
	envelope := mustEnvelope(t, items[1])
	if envelope.KiroRedacted != "kms-2" {
		t.Fatalf("krc=%q want kms-2", envelope.KiroRedacted)
	}
}

func TestIncompleteFlushesPendingRedactedReasoning(t *testing.T) {
	b := newReplayTestBridge(false)
	frames := handleAll(t, b,
		protocol.Event{Type: protocol.EventRedactedThinking, Data: "opaque"},
		protocol.Event{Type: protocol.EventIncomplete, Reason: "upstream_eof"},
	)
	items := outputDoneItems(frames)
	if len(items) != 1 {
		t.Fatalf("got %d done items, want 1", len(items))
	}
	if got := mustEnvelope(t, items[0]); len(got.Redacted) != 1 || got.Redacted[0] != "opaque" {
		t.Fatalf("envelope=%+v", got)
	}
	if frames[len(frames)-1].Name != "response.incomplete" {
		t.Fatalf("terminal=%s", frames[len(frames)-1].Name)
	}
}

func kiroEnvelopeObject(t *testing.T, item map[string]any) map[string]any {
	t.Helper()
	raw, ok := item["encrypted_content"].(string)
	if !ok || !strings.HasPrefix(raw, benesreasoning.Prefix) {
		t.Fatalf("missing Kiro envelope: %#v", item)
	}
	payload, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(raw, benesreasoning.Prefix))
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(payload, &object); err != nil {
		t.Fatal(err)
	}
	return object
}

func TestKiroSignatureEventPersistsMemberIdentityInEnvelope(t *testing.T) {
	b := newReplayTestBridge(false)
	frames := handleAll(t, b,
		protocol.Event{Type: protocol.EventKiroRedactedReasoning, Signature: "sig-1"},
		protocol.Event{Type: protocol.EventDone},
	)
	items := outputDoneItems(frames)
	if len(items) != 1 {
		t.Fatalf("got %d done items, want 1", len(items))
	}
	object := kiroEnvelopeObject(t, items[0])
	if object["krc"] != "sig-1" || object["krk"] != "signature" {
		t.Fatalf("envelope=%#v", object)
	}
}

func TestKiroRedactedEventPersistsExplicitLegacyMember(t *testing.T) {
	b := newReplayTestBridge(false)
	frames := handleAll(t, b,
		protocol.Event{Type: protocol.EventKiroRedactedReasoning, Data: "legacy"},
		protocol.Event{Type: protocol.EventDone},
	)
	items := outputDoneItems(frames)
	if len(items) != 1 {
		t.Fatalf("got %d done items, want 1", len(items))
	}
	object := kiroEnvelopeObject(t, items[0])
	if object["krc"] != "legacy" || object["krk"] != "redactedContent" {
		t.Fatalf("envelope=%#v", object)
	}
}

