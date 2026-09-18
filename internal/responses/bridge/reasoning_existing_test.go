package bridge

import (
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
)

func TestThinkingSummaryLifecycle(t *testing.T) {
	ids := []string{"rs_1"}
	b := New("gpt-5", Options{ResponseID: "resp_1", ID: func(string) string {
		id := ids[0]
		ids = ids[1:]
		return id
	}})
	_ = b.Start()
	frames := append([]Frame{}, mustHandle(t, b, protocol.Event{Type: protocol.EventThinkingDelta, Thinking: "plan "})...)
	frames = append(frames, mustHandle(t, b, protocol.Event{Type: protocol.EventThinkingDelta, Thinking: "carefully"})...)
	frames = append(frames, mustHandle(t, b, protocol.Event{Type: protocol.EventDone})...)
	assertFrameTypes(t, frames, []string{
		"response.output_item.added",
		"response.reasoning_summary_part.added",
		"response.reasoning_summary_text.delta",
		"response.reasoning_summary_text.delta",
		"response.reasoning_summary_text.done",
		"response.reasoning_summary_part.done",
		"response.output_item.done",
		"response.completed",
	})
	item := object(t, frames[6].Data["item"])
	if item["type"] != "reasoning" {
		t.Fatalf("item=%#v", item)
	}
	summary := array(t, item["summary"])
	if object(t, summary[0])["text"] != "plan carefully" {
		t.Fatalf("summary=%#v", summary)
	}
}

func TestRawReasoningLifecycleUsesSummaryChannel(t *testing.T) {
	b := New("gpt-5", Options{ResponseID: "resp_1", ID: func(string) string { return "rs_raw" }})
	_ = b.Start()
	frames := append([]Frame{}, mustHandle(t, b, protocol.Event{Type: protocol.EventReasoningRawDelta, Text: "raw"})...)
	frames = append(frames, mustHandle(t, b, protocol.Event{Type: protocol.EventTextDelta, Text: "answer"})...)
	assertFrameTypes(t, frames, []string{
		"response.output_item.added",
		"response.reasoning_summary_part.added",
		"response.reasoning_summary_text.delta",
		"response.reasoning_summary_text.done",
		"response.reasoning_summary_part.done",
		"response.output_item.done",
		"response.output_item.added",
		"response.content_part.added",
		"response.output_text.delta",
	})
	item := object(t, frames[5].Data["item"])
	if _, exists := item["content"]; exists {
		t.Fatalf("item retained raw content=%#v", item)
	}
	summary := array(t, item["summary"])
	if object(t, summary[0])["type"] != "summary_text" || object(t, summary[0])["text"] != "raw" {
		t.Fatalf("item=%#v", item)
	}
}

func TestThinkingToTextClosesReasoningBeforeMessage(t *testing.T) {
	ids := []string{"rs_1", "msg_1"}
	b := New("gpt-5", Options{ResponseID: "resp_1", ID: func(string) string {
		id := ids[0]
		ids = ids[1:]
		return id
	}})
	_ = b.Start()
	_ = mustHandle(t, b, protocol.Event{Type: protocol.EventThinkingDelta, Thinking: "work"})
	frames := mustHandle(t, b, protocol.Event{Type: protocol.EventTextDelta, Text: "answer"})
	assertFrameTypes(t, frames, []string{
		"response.reasoning_summary_text.done",
		"response.reasoning_summary_part.done",
		"response.output_item.done",
		"response.output_item.added",
		"response.content_part.added",
		"response.output_text.delta",
	})
}
