package bridge

import (
	"encoding/json"
	"testing"

	"github.com/Wibias/Benes/internal/compaction"
	"github.com/Wibias/Benes/internal/protocol"
)

func TestTextLifecycleMatchesResponsesOrdering(t *testing.T) {
	ids := []string{"msg_1"}
	b := New("gpt-5", Options{ResponseID: "resp_1", CreatedAt: 123, ID: func(string) string {
		id := ids[0]
		ids = ids[1:]
		return id
	}})
	frames := append([]Frame{}, b.Start()...)
	frames = append(frames, mustHandle(t, b, protocol.Event{Type: protocol.EventTextDelta, Text: "hel"})...)
	frames = append(frames, mustHandle(t, b, protocol.Event{Type: protocol.EventTextDelta, Text: "lo"})...)
	frames = append(frames, mustHandle(t, b, protocol.Event{Type: protocol.EventDone})...)
	assertFrameTypes(t, frames, []string{
		"response.created", "response.output_item.added", "response.content_part.added",
		"response.output_text.delta", "response.output_text.delta", "response.output_text.done",
		"response.content_part.done", "response.output_item.done", "response.completed",
	})
	for i, frame := range frames {
		if got := number(t, frame.Data["sequence_number"]); got != int64(i) {
			t.Fatalf("frame %d sequence=%d", i, got)
		}
		if frame.Data["type"] != frame.Name {
			t.Fatalf("frame %d type=%v name=%s", i, frame.Data["type"], frame.Name)
		}
	}
	completed := object(t, frames[len(frames)-1].Data["response"])
	if completed["status"] != "completed" {
		t.Fatalf("status=%v", completed["status"])
	}
	output := array(t, completed["output"])
	if len(output) != 1 {
		t.Fatalf("output=%#v", output)
	}
	content := array(t, object(t, output[0])["content"])
	if object(t, content[0])["text"] != "hello" {
		t.Fatalf("content=%#v", content)
	}
}

func TestSnapshotBackfillsCanonicalLifecycleFields(t *testing.T) {
	parallel := false
	b := New("gpt-5", Options{
		ResponseID:        "resp_1",
		CreatedAt:         123,
		ID:                func(string) string { return "msg_1" },
		ParallelToolCalls: &parallel,
		ToolChoice:        &protocol.ToolChoice{Kind: protocol.ToolChoiceNamed, Name: "lookup"},
		Tools:             []protocol.Tool{{Name: "lookup", Description: "find", Parameters: map[string]any{"type": "object"}}},
	})
	frames := append([]Frame{}, b.Start()...)
	frames = append(frames, mustHandle(t, b, protocol.Event{Type: protocol.EventTextDelta, Text: "hello"})...)
	frames = append(frames, mustHandle(t, b, protocol.Event{Type: protocol.EventDone})...)
	created := object(t, frames[0].Data["response"])
	if created["parallel_tool_calls"] != false {
		t.Fatalf("created parallel_tool_calls=%v", created["parallel_tool_calls"])
	}
	if object(t, created["tool_choice"])["name"] != "lookup" {
		t.Fatalf("created tool_choice=%#v", created["tool_choice"])
	}
	tools := array(t, created["tools"])
	if len(tools) != 1 || object(t, tools[0])["name"] != "lookup" {
		t.Fatalf("created tools=%#v", tools)
	}
	delta := frames[3]
	if delta.Name != "response.output_text.delta" {
		t.Fatalf("delta frame=%s", delta.Name)
	}
	if _, ok := delta.Data["logprobs"].([]any); !ok {
		t.Fatalf("delta logprobs=%#v", delta.Data["logprobs"])
	}
	completed := object(t, frames[len(frames)-1].Data["response"])
	if completed["parallel_tool_calls"] != false || completed["status"] != "completed" {
		t.Fatalf("completed=%#v", completed)
	}
	done := frames[4]
	if done.Name != "response.output_text.done" {
		t.Fatalf("done frame=%s", done.Name)
	}
	if _, ok := done.Data["logprobs"].([]any); !ok {
		t.Fatalf("done logprobs=%#v", done.Data["logprobs"])
	}
}

func TestSnapshotDefaultsWhenRequestOmitsToolControls(t *testing.T) {
	b := New("gpt-5", Options{ResponseID: "resp_1", CreatedAt: 1, ID: func(string) string { return "msg_1" }})
	created := object(t, b.Start()[0].Data["response"])
	if created["parallel_tool_calls"] != true {
		t.Fatalf("parallel_tool_calls=%v", created["parallel_tool_calls"])
	}
	if created["tool_choice"] != "auto" {
		t.Fatalf("tool_choice=%v", created["tool_choice"])
	}
	if tools := array(t, created["tools"]); len(tools) != 0 {
		t.Fatalf("tools=%#v", tools)
	}
}

func TestPhaseChangeClosesMessageAsCommentary(t *testing.T) {
	commentary := protocol.PhaseCommentary
	finalAnswer := protocol.PhaseFinalAnswer
	ids := []string{"msg_1", "msg_2"}
	b := New("gpt-5", Options{ResponseID: "resp_1", ID: func(string) string {
		id := ids[0]
		ids = ids[1:]
		return id
	}})
	_ = b.Start()
	_ = mustHandle(t, b, protocol.Event{Type: protocol.EventTextDelta, Text: "work", Phase: &commentary})
	frames := mustHandle(t, b, protocol.Event{Type: protocol.EventTextDelta, Text: "answer", Phase: &finalAnswer})
	assertFrameTypes(t, frames, []string{
		"response.output_text.done", "response.content_part.done", "response.output_item.done",
		"response.output_item.added", "response.content_part.added", "response.output_text.delta",
	})
	if object(t, frames[2].Data["item"])["phase"] != "commentary" {
		t.Fatalf("item=%#v", frames[2].Data["item"])
	}
}

func TestFunctionCallLifecycleAndEmptyArguments(t *testing.T) {
	b := New("gpt-5", Options{ResponseID: "resp_1", ID: func(string) string { return "fc_1" }})
	_ = b.Start()
	frames := append([]Frame{}, mustHandle(t, b, protocol.Event{Type: protocol.EventToolCallStart, ID: "call_1", Name: "lookup"})...)
	frames = append(frames, mustHandle(t, b, protocol.Event{Type: protocol.EventToolCallEnd, ID: "call_1"})...)
	assertFrameTypes(t, frames, []string{
		"response.output_item.added", "response.function_call_arguments.done", "response.output_item.done",
	})
	item := object(t, frames[2].Data["item"])
	if item["arguments"] != "{}" || item["status"] != "completed" {
		t.Fatalf("item=%#v", item)
	}
}

func TestFunctionCallDeltasStreamAndAssemble(t *testing.T) {
	b := New("gpt-5", Options{ResponseID: "resp_1", ID: func(string) string { return "fc_1" }})
	_ = b.Start()
	_ = mustHandle(t, b, protocol.Event{Type: protocol.EventToolCallStart, ID: "call_1", Name: "lookup"})
	frames := mustHandle(t, b, protocol.Event{Type: protocol.EventToolCallDelta, Arguments: `{"q":"x"}`})
	assertFrameTypes(t, frames, []string{"response.function_call_arguments.delta"})
	if frames[0].Data["delta"] != `{"q":"x"}` {
		t.Fatalf("delta=%v", frames[0].Data["delta"])
	}
	frames = mustHandle(t, b, protocol.Event{Type: protocol.EventToolCallEnd})
	if object(t, frames[len(frames)-1].Data["item"])["arguments"] != `{"q":"x"}` {
		t.Fatalf("item=%#v", frames[len(frames)-1].Data["item"])
	}
}

func TestMalformedCompletedFunctionArgumentsFailClosed(t *testing.T) {
	b := New("gpt-5", Options{ResponseID: "resp_1", ID: func(string) string { return "fc_1" }})
	_ = b.Start()
	_ = mustHandle(t, b, protocol.Event{Type: protocol.EventToolCallStart, ID: "call_1", Name: "lookup"})
	_ = mustHandle(t, b, protocol.Event{Type: protocol.EventToolCallDelta, Arguments: `{"q":`})
	frames, err := b.Handle(protocol.Event{Type: protocol.EventToolCallEnd})
	if err == nil {
		t.Fatal("Handle accepted malformed tool arguments")
	}
	assertFrameTypes(t, frames, []string{"response.output_item.done", "response.failed"})
	if object(t, frames[0].Data["item"])["status"] != "incomplete" {
		t.Fatalf("item=%#v", frames[0].Data["item"])
	}
}

func TestIncompleteFailsOpenToolWithoutArgumentsDone(t *testing.T) {
	b := New("gpt-5", Options{ResponseID: "resp_1", ID: func(string) string { return "fc_1" }})
	_ = b.Start()
	_ = mustHandle(t, b, protocol.Event{Type: protocol.EventToolCallStart, ID: "call_1", Name: "lookup"})
	_ = mustHandle(t, b, protocol.Event{Type: protocol.EventToolCallDelta, Arguments: `{"q":`})
	frames := mustHandle(t, b, protocol.Event{Type: protocol.EventIncomplete, Reason: "max_output_tokens"})
	assertFrameTypes(t, frames, []string{"response.output_item.done", "response.incomplete"})
	if object(t, frames[0].Data["item"])["status"] != "incomplete" {
		t.Fatalf("item=%#v", frames[0].Data["item"])
	}
}

func TestAdapterEOFSynthesizesIncompleteExactlyOnce(t *testing.T) {
	b := New("gpt-5", Options{ResponseID: "resp_1"})
	_ = b.Start()
	frames := b.End()
	assertFrameTypes(t, frames, []string{"response.incomplete"})
	response := object(t, frames[0].Data["response"])
	if object(t, response["incomplete_details"])["reason"] != "adapter_eof" {
		t.Fatalf("response=%#v", response)
	}
	if second := b.End(); len(second) != 0 {
		t.Fatalf("second End emitted %#v", second)
	}
}

func TestUsageAlwaysContainsStrictDetailObjects(t *testing.T) {
	b := New("gpt-5", Options{ResponseID: "resp_1"})
	_ = b.Start()
	frames := mustHandle(t, b, protocol.Event{Type: protocol.EventDone, Usage: &protocol.Usage{InputTokens: 10, OutputTokens: 3}})
	usage := object(t, object(t, frames[len(frames)-1].Data["response"])["usage"])
	if _, ok := usage["input_tokens_details"]; !ok {
		t.Fatal("usage missing input_tokens_details")
	}
	if _, ok := usage["output_tokens_details"]; !ok {
		t.Fatal("usage missing output_tokens_details")
	}
}

func TestMalformedToolTransitionToTextEmitsFailed(t *testing.T) {
	b := New("gpt-5", Options{ResponseID: "resp_1", ID: func(string) string { return "fc_1" }})
	_ = b.Start()
	_ = mustHandle(t, b, protocol.Event{Type: protocol.EventToolCallStart, ID: "call_1", Name: "lookup"})
	_ = mustHandle(t, b, protocol.Event{Type: protocol.EventToolCallDelta, Arguments: `{"q":`})
	frames, err := b.Handle(protocol.Event{Type: protocol.EventTextDelta, Text: "should-not-continue"})
	if err == nil {
		t.Fatal("transition accepted malformed tool arguments")
	}
	assertFrameTypes(t, frames, []string{"response.output_item.done", "response.failed"})
}

func TestMalformedToolTransitionToNextToolEmitsFailed(t *testing.T) {
	ids := []string{"fc_1", "fc_2"}
	b := New("gpt-5", Options{ResponseID: "resp_1", ID: func(string) string {
		id := ids[0]
		ids = ids[1:]
		return id
	}})
	_ = b.Start()
	_ = mustHandle(t, b, protocol.Event{Type: protocol.EventToolCallStart, ID: "call_1", Name: "lookup"})
	_ = mustHandle(t, b, protocol.Event{Type: protocol.EventToolCallDelta, Arguments: `{"q":`})
	frames, err := b.Handle(protocol.Event{Type: protocol.EventToolCallStart, ID: "call_2", Name: "next"})
	if err == nil {
		t.Fatal("transition accepted malformed prior tool arguments")
	}
	assertFrameTypes(t, frames, []string{"response.output_item.done", "response.failed"})
}

func mustHandle(t *testing.T, b *Bridge, event protocol.Event) []Frame {
	t.Helper()
	frames, err := b.Handle(event)
	if err != nil {
		t.Fatalf("Handle(%s): %v", event.Type, err)
	}
	return frames
}

func assertFrameTypes(t *testing.T, frames []Frame, want []string) {
	t.Helper()
	if len(frames) != len(want) {
		t.Fatalf("frame count=%d want=%d frames=%#v", len(frames), len(want), frames)
	}
	for i := range want {
		if frames[i].Name != want[i] {
			t.Fatalf("frame[%d]=%s want=%s", i, frames[i].Name, want[i])
		}
	}
}

func object(t *testing.T, value any) map[string]any {
	t.Helper()
	if direct, ok := value.(map[string]any); ok {
		return direct
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(encoded, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func array(t *testing.T, value any) []any {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var result []any
	if err := json.Unmarshal(encoded, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func number(t *testing.T, value any) int64 {
	t.Helper()
	switch n := value.(type) {
	case int:
		return int64(n)
	case int64:
		return n
	case float64:
		return int64(n)
	default:
		t.Fatalf("not a number: %#v", value)
		return 0
	}
}

func TestResponsesUsageMarksUnconfirmedXAIPriority(t *testing.T) {
	b := New("xai/grok-4.6", Options{ResponseID: "resp_1", CreatedAt: 1, ID: func(string) string { return "msg_1" }})
	_ = b.Start()
	frames := mustHandle(t, b, protocol.Event{Type: protocol.EventDone, Usage: &protocol.Usage{InputTokens: 9, OutputTokens: 1}})
	usage := object(t, object(t, frames[len(frames)-1].Data["response"])["usage"])
	if usage["xai_priority_applied"] != false || usage["xai_lower_bound"] != false {
		t.Fatalf("usage=%#v", usage)
	}
}

func TestResponsesUsageMarksRequestedLongContextXAIPriorityAsLowerBound(t *testing.T) {
	tier := "priority"
	b := New("xai/grok-4.6", Options{ResponseID: "resp_1", CreatedAt: 1, ID: func(string) string { return "msg_1" }, RequestedServiceTier: &tier})
	_ = b.Start()
	frames := mustHandle(t, b, protocol.Event{Type: protocol.EventDone, Usage: &protocol.Usage{InputTokens: 200_000, OutputTokens: 1}})
	usage := object(t, object(t, frames[len(frames)-1].Data["response"])["usage"])
	if usage["xai_priority_applied"] != false || usage["xai_lower_bound"] != true {
		t.Fatalf("usage=%#v", usage)
	}
}

func TestResponsesUsageBillsConfirmedXAIPriority(t *testing.T) {
	b := New("xai/grok-4.6", Options{ResponseID: "resp_1", CreatedAt: 1, ID: func(string) string { return "msg_1" }})
	_ = b.Start()
	frames := mustHandle(t, b, protocol.Event{Type: protocol.EventDone, Usage: &protocol.Usage{InputTokens: 9, OutputTokens: 1, ServiceTier: "priority"}})
	usage := object(t, object(t, frames[len(frames)-1].Data["response"])["usage"])
	if usage["xai_priority_applied"] != true || usage["xai_lower_bound"] != false {
		t.Fatalf("usage=%#v", usage)
	}
}

func TestPauseTurnDoesNotCompleteOrReplaceHistory(t *testing.T) {
	b := New("gpt-5", Options{ResponseID: "resp_1", CreatedAt: 1, ID: func(string) string { return "msg_1" }})
	_ = b.Start()
	_ = mustHandle(t, b, protocol.Event{Type: protocol.EventTextDelta, Text: "partial"})
	frames := mustHandle(t, b, protocol.Event{Type: protocol.EventDone, StopReason: "pause_turn"})
	last := frames[len(frames)-1]
	if last.Name != "response.incomplete" {
		t.Fatalf("frames=%#v", frames)
	}
	response := object(t, last.Data["response"])
	if response["status"] != "incomplete" {
		t.Fatalf("status=%v", response["status"])
	}
	details := object(t, response["incomplete_details"])
	if details["reason"] != "pause_turn" {
		t.Fatalf("details=%#v", details)
	}
}

func TestProviderErrorDoesNotInstallReplacementHistory(t *testing.T) {
	b := New("gpt-5", Options{ResponseID: "resp_1", CreatedAt: 1, ID: func(string) string { return "msg_1" }})
	_ = b.Start()
	frames := mustHandle(t, b, protocol.Event{Type: protocol.EventError, Message: "boom", Usage: &protocol.Usage{InputTokens: 3, OutputTokens: 1}})
	last := frames[len(frames)-1]
	if last.Name == "response.completed" {
		t.Fatalf("error completed: %#v", frames)
	}
	if last.Name != "response.failed" && last.Name != "error" && last.Name != "response.incomplete" {
		t.Fatalf("error frames=%#v", frames)
	}
}

func TestOpenToolAtEOFIsIncompleteNotCompleted(t *testing.T) {
	b := New("gpt-5", Options{ResponseID: "resp_1", CreatedAt: 1, ID: func(string) string { return "fc_1" }})
	_ = b.Start()
	_ = mustHandle(t, b, protocol.Event{Type: protocol.EventToolCallStart, ID: "call_1", Name: "lookup"})
	frames := mustHandle(t, b, protocol.Event{Type: protocol.EventDone})
	last := frames[len(frames)-1]
	if last.Name != "response.incomplete" {
		t.Fatalf("open tool completed: %#v", frames)
	}
}

func TestTruncationStopsAreIncompleteNotCompleted(t *testing.T) {
	for _, reason := range []string{"max_tokens", "length", "content_filter", "model_context_window_exceeded"} {
		t.Run(reason, func(t *testing.T) {
			b := New("gpt-5", Options{ResponseID: "resp_1", CreatedAt: 1, ID: func(string) string { return "msg_1" }})
			_ = b.Start()
			_ = mustHandle(t, b, protocol.Event{Type: protocol.EventTextDelta, Text: "x"})
			frames := mustHandle(t, b, protocol.Event{Type: protocol.EventDone, StopReason: reason})
			last := frames[len(frames)-1]
			if last.Name != "response.incomplete" {
				t.Fatalf("%s completed: %#v", reason, frames)
			}
		})
	}
}

func TestCompactionModeEmitsExactlyOneItemAndNoAssistantMessage(t *testing.T) {
	ids := []string{"cmp_1"}
	b := New("gpt-5", Options{ResponseID: "resp_1", CreatedAt: 1, CompactionRequest: true, ID: func(string) string {
		id := ids[0]
		ids = ids[1:]
		return id
	}})
	frames := append([]Frame{}, b.Start()...)
	frames = append(frames, mustHandle(t, b, protocol.Event{Type: protocol.EventTextDelta, Text: "summary"})...)
	frames = append(frames, mustHandle(t, b, protocol.Event{Type: protocol.EventDone})...)
	assertFrameTypes(t, frames, []string{
		"response.created", "response.output_item.added", "response.output_item.done", "response.completed",
	})
	completed := object(t, frames[len(frames)-1].Data["response"])
	output := array(t, completed["output"])
	if len(output) != 1 {
		t.Fatalf("output=%#v", output)
	}
	item := object(t, output[0])
	if item["type"] != "compaction" {
		t.Fatalf("item=%#v", item)
	}
	encrypted, _ := item["encrypted_content"].(string)
	got, ok := compaction.DecodeSummary(encrypted)
	if !ok || got != "summary" {
		t.Fatalf("encrypted=%q got=%q ok=%v", encrypted, got, ok)
	}
}

func TestCompactionModeFailedTurnEmitsNoCompactionItem(t *testing.T) {
	b := New("gpt-5", Options{ResponseID: "resp_1", CreatedAt: 1, CompactionRequest: true, ID: func(string) string { return "cmp_1" }})
	_ = b.Start()
	frames := mustHandle(t, b, protocol.Event{Type: protocol.EventError, Message: "boom"})
	last := frames[len(frames)-1]
	if last.Name != "response.failed" {
		t.Fatalf("frames=%#v", frames)
	}
	response := object(t, last.Data["response"])
	output := array(t, response["output"])
	if len(output) != 0 {
		t.Fatalf("output=%#v", output)
	}
}

func TestNativeCompactionEventPassesThroughOpaqueBlob(t *testing.T) {
	b := New("gpt-5", Options{ResponseID: "resp_1", CreatedAt: 1, ID: func(string) string { return "cmp_x" }})
	frames := append([]Frame{}, b.Start()...)
	frames = append(frames, mustHandle(t, b, protocol.Event{Type: protocol.EventCompaction, ID: "cmp_native", Data: "native-blob"})...)
	frames = append(frames, mustHandle(t, b, protocol.Event{Type: protocol.EventDone})...)
	completed := object(t, frames[len(frames)-1].Data["response"])
	output := array(t, completed["output"])
	if len(output) != 1 {
		t.Fatalf("output=%#v", output)
	}
	item := object(t, output[0])
	if item["type"] != "compaction" || item["encrypted_content"] != "native-blob" {
		t.Fatalf("item=%#v", item)
	}
}
