package bridge

import (
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
)

func TestRawReasoningUsesExpandableSummaryLifecycle(t *testing.T) {
	b := New("deepseek-v4", Options{
		ResponseID: "resp_1",
		ID:         func(string) string { return "rs_raw" },
	})
	_ = b.Start()

	frames := append([]Frame{}, mustHandle(t, b, protocol.Event{Type: protocol.EventReasoningRawDelta, Text: "raw detail"})...)
	frames = append(frames, mustHandle(t, b, protocol.Event{Type: protocol.EventDone})...)

	assertFrameTypes(t, frames, []string{
		"response.output_item.added",
		"response.reasoning_summary_part.added",
		"response.reasoning_summary_text.delta",
		"response.reasoning_summary_text.done",
		"response.reasoning_summary_part.done",
		"response.output_item.done",
		"response.completed",
	})
	for _, frame := range frames {
		if frame.Name == "response.reasoning_text.delta" || frame.Name == "response.reasoning_text.done" {
			t.Fatalf("raw reasoning leaked through content channel: %#v", frame)
		}
	}
	if got := frames[2].Data["summary_index"]; number(t, got) != 0 {
		t.Fatalf("summary_index=%v", got)
	}
	item := object(t, frames[5].Data["item"])
	if _, exists := item["content"]; exists {
		t.Fatalf("reasoning item retained visible content channel: %#v", item)
	}
	summary := array(t, item["summary"])
	if len(summary) != 1 || object(t, summary[0])["type"] != "summary_text" || object(t, summary[0])["text"] != "raw detail" {
		t.Fatalf("summary=%#v", summary)
	}
}

func TestHiddenRawReasoningRemainsEnvelopeOnly(t *testing.T) {
	ids := []string{"rs_hidden", "msg_1"}
	b := New("deepseek-v4", Options{
		ResponseID:          "resp_1",
		HideThinkingSummary: true,
		ID: func(string) string {
			id := ids[0]
			ids = ids[1:]
			return id
		},
	})
	_ = b.Start()

	if frames := mustHandle(t, b, protocol.Event{Type: protocol.EventReasoningRawDelta, Text: "private thought"}); len(frames) != 0 {
		t.Fatalf("hidden raw reasoning emitted visible frames: %#v", frames)
	}
	frames := mustHandle(t, b, protocol.Event{Type: protocol.EventTextDelta, Text: "visible"})
	assertFrameTypes(t, frames, []string{
		"response.output_item.added",
		"response.output_item.done",
		"response.output_item.added",
		"response.content_part.added",
		"response.output_text.delta",
	})
	hidden := object(t, frames[1].Data["item"])
	if _, exists := hidden["content"]; exists {
		t.Fatalf("hidden reasoning exposed content: %#v", hidden)
	}
	if summary := array(t, hidden["summary"]); len(summary) != 0 {
		t.Fatalf("hidden reasoning exposed summary: %#v", summary)
	}
	encrypted, _ := hidden["encrypted_content"].(string)
	if !strings.HasPrefix(encrypted, "benesr1:") {
		t.Fatalf("hidden reasoning missing replay envelope: %#v", hidden)
	}
}

func TestRawReasoningClosesBeforeToolWithoutLosingCallIdentity(t *testing.T) {
	ids := []string{"rs_1", "fc_1"}
	b := New("deepseek-v4", Options{
		ResponseID: "resp_1",
		ID: func(string) string {
			id := ids[0]
			ids = ids[1:]
			return id
		},
	})
	_ = b.Start()
	_ = mustHandle(t, b, protocol.Event{Type: protocol.EventReasoningRawDelta, Text: "choose tool"})

	frames := mustHandle(t, b, protocol.Event{Type: protocol.EventToolCallStart, ID: "call_42", Name: "lookup"})
	assertFrameTypes(t, frames, []string{
		"response.reasoning_summary_text.done",
		"response.reasoning_summary_part.done",
		"response.output_item.done",
		"response.output_item.added",
	})
	tool := object(t, frames[3].Data["item"])
	if tool["type"] != "function_call" || tool["call_id"] != "call_42" || tool["name"] != "lookup" {
		t.Fatalf("tool identity changed across reasoning close: %#v", tool)
	}

	_ = mustHandle(t, b, protocol.Event{Type: protocol.EventToolCallDelta, ID: "call_42", Arguments: `{"q":"x"}`})
	closed := mustHandle(t, b, protocol.Event{Type: protocol.EventToolCallEnd, ID: "call_42"})
	item := object(t, closed[len(closed)-1].Data["item"])
	if item["call_id"] != "call_42" || item["arguments"] != `{"q":"x"}` {
		t.Fatalf("tool continuation changed: %#v", item)
	}
}
