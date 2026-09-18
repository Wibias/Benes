package outbound

import (
	"errors"
	"reflect"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
)

func TestEncoderMapsTextReasoningToolsUsageAndTerminal(t *testing.T) {
	enc, err := New("client/model", Options{IDGenerator: func() string { return "chatcmpl-fixed" }, CreatedUnix: 123})
	if err != nil {
		t.Fatal(err)
	}
	events := []protocol.Event{
		{Type: protocol.EventTextDelta, Text: "hel"},
		{Type: protocol.EventTextDelta, Text: "lo"},
		{Type: protocol.EventThinkingDelta, Thinking: "summary"},
		{Type: protocol.EventReasoningRawDelta, Text: " raw"},
		{Type: protocol.EventToolCallStart, ID: "call_1", Name: "lookup"},
		{Type: protocol.EventToolCallDelta, ID: "call_1", Arguments: `{"q":"`},
		{Type: protocol.EventToolCallDelta, ID: "call_1", Arguments: `x"}`},
		{Type: protocol.EventToolCallEnd, ID: "call_1"},
		{Type: protocol.EventDone, Usage: &protocol.Usage{InputTokens: 5, OutputTokens: 7, TotalTokens: 99, CachedInputTokens: 2, ReasoningOutputTokens: 3}},
	}
	var frames []Frame
	for _, event := range events {
		got, handleErr := enc.Handle(event)
		if handleErr != nil {
			t.Fatalf("Handle(%s): %v", event.Type, handleErr)
		}
		frames = append(frames, got...)
	}
	if len(frames) != 8 {
		t.Fatalf("frames=%d %#v", len(frames), frames)
	}
	assertChoiceDelta(t, frames[0], map[string]any{"role": "assistant", "content": ""}, nil)
	assertChoiceDelta(t, frames[1], map[string]any{"content": "hel"}, nil)
	assertChoiceDelta(t, frames[2], map[string]any{"content": "lo"}, nil)
	assertChoiceDelta(t, frames[3], map[string]any{"reasoning_content": "summary"}, nil)
	assertChoiceDelta(t, frames[4], map[string]any{"reasoning_content": " raw"}, nil)
	choice := firstChoice(t, frames[5])
	delta := choice["delta"].(map[string]any)
	calls := delta["tool_calls"].([]any)
	call := calls[0].(map[string]any)
	if call["index"] != 0 || call["id"] != "call_1" || call["type"] != "function" {
		t.Fatalf("call=%#v", call)
	}
	fn := call["function"].(map[string]any)
	if fn["name"] != "lookup" || fn["arguments"] != `{"q":"x"}` {
		t.Fatalf("function=%#v", fn)
	}
	terminal := firstChoice(t, frames[6])
	if terminal["finish_reason"] != "tool_calls" {
		t.Fatalf("terminal=%#v", terminal)
	}
	usage := frames[6].Payload["usage"].(map[string]any)
	wantUsage := map[string]any{
		"prompt_tokens": int64(5), "completion_tokens": int64(7), "total_tokens": int64(12),
		"prompt_tokens_details":     map[string]any{"cached_tokens": int64(2)},
		"completion_tokens_details": map[string]any{"reasoning_tokens": int64(3)},
	}
	if !reflect.DeepEqual(usage, wantUsage) {
		t.Fatalf("usage=%#v", usage)
	}
	if !frames[7].Done {
		t.Fatalf("last=%#v", frames[7])
	}

	completion, err := enc.Completion()
	if err != nil {
		t.Fatal(err)
	}
	if completion["id"] != "chatcmpl-fixed" || completion["object"] != "chat.completion" || completion["created"] != int64(123) || completion["model"] != "client/model" {
		t.Fatalf("completion=%#v", completion)
	}
	cchoice := completion["choices"].([]any)[0].(map[string]any)
	if cchoice["finish_reason"] != "tool_calls" || cchoice["logprobs"] != nil {
		t.Fatalf("choice=%#v", cchoice)
	}
	msg := cchoice["message"].(map[string]any)
	if msg["role"] != "assistant" || msg["content"] != "hello" || msg["reasoning_content"] != "summary raw" {
		t.Fatalf("message=%#v", msg)
	}
	tcalls := msg["tool_calls"].([]any)
	if len(tcalls) != 1 {
		t.Fatalf("tool_calls=%#v", tcalls)
	}
}

func TestEncoderEmitsRoleImmediatelyForToolOnlyStream(t *testing.T) {
	enc, _ := New("m", Options{IDGenerator: func() string { return "id" }, CreatedUnix: 1})
	frames, err := enc.Handle(protocol.Event{Type: protocol.EventToolCallStart, ID: "c", Name: "lookup"})
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 1 {
		t.Fatalf("frames=%#v", frames)
	}
	assertChoiceDelta(t, frames[0], map[string]any{"role": "assistant", "content": ""}, nil)
}

func TestEncoderMapsEarlyTerminalReasonsTruthfully(t *testing.T) {
	tests := []struct{ name, reason, want string }{
		{"done max tokens", "max_tokens", "length"},
		{"done content filter", "content_filter", "content_filter"},
		{"incomplete max output", "max_output_tokens", "length"},
		{"incomplete max tokens", "max_tokens", "length"},
		{"incomplete content filter", "content_filter", "content_filter"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enc, _ := New("m", Options{IDGenerator: func() string { return "id" }, CreatedUnix: 1})
			typ := protocol.EventDone
			if len(tt.name) >= 10 && tt.name[:10] == "incomplete" {
				typ = protocol.EventIncomplete
			}
			frames, err := enc.Handle(protocol.Event{Type: typ, StopReason: tt.reason, Reason: tt.reason})
			if err != nil {
				t.Fatal(err)
			}
			if len(frames) != 3 {
				t.Fatalf("frames=%#v", frames)
			}
			if got := firstChoice(t, frames[1])["finish_reason"]; got != tt.want {
				t.Fatalf("finish=%#v", got)
			}
			if !frames[2].Done {
				t.Fatal("missing [DONE]")
			}
		})
	}
}

func TestEncoderUnknownIncompleteAndErrorAreErrorFramesWithoutDone(t *testing.T) {
	tests := []protocol.Event{
		{Type: protocol.EventIncomplete, Reason: "adapter_eof", Message: "ended early"},
		{Type: protocol.EventError, Message: "provider failed", HTTPStatus: 429, ErrorType: "rate_limit_error", Code: "rate_limit_exceeded"},
	}
	for _, event := range tests {
		enc, _ := New("m", Options{IDGenerator: func() string { return "id" }, CreatedUnix: 1})
		frames, err := enc.Handle(event)
		if err != nil {
			t.Fatal(err)
		}
		if len(frames) != 1 || frames[0].Done {
			t.Fatalf("frames=%#v", frames)
		}
		body := frames[0].Payload["error"].(map[string]any)
		if body["message"] == "" || body["param"] != nil {
			t.Fatalf("error=%#v", body)
		}
		if _, err := enc.Completion(); err == nil {
			t.Fatal("failed stream produced success completion")
		}
	}
}

func TestEncoderRejectsMalformedToolSequenceAsTypedFailure(t *testing.T) {
	enc, _ := New("m", Options{IDGenerator: func() string { return "id" }, CreatedUnix: 1})
	frames, err := enc.Handle(protocol.Event{Type: protocol.EventToolCallDelta, ID: "missing", Arguments: "{}"})
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 1 || frames[0].Done {
		t.Fatalf("frames=%#v", frames)
	}
	failure := frames[0].Payload["error"].(map[string]any)
	if failure["type"] != "upstream_error" {
		t.Fatalf("failure=%#v", failure)
	}
}

func TestEncoderBoundsToolArgumentsAndWholeTurn(t *testing.T) {
	enc, _ := New("m", Options{IDGenerator: func() string { return "id" }, CreatedUnix: 1, MaxCallArgumentBytes: 5, MaxTurnBytes: 64})
	if _, err := enc.Handle(protocol.Event{Type: protocol.EventToolCallStart, ID: "c", Name: "f"}); err != nil {
		t.Fatal(err)
	}
	frames, err := enc.Handle(protocol.Event{Type: protocol.EventToolCallDelta, ID: "c", Arguments: "123456"})
	if err != nil {
		t.Fatal(err)
	}
	assertBufferFailure(t, frames)

	enc2, _ := New("m", Options{IDGenerator: func() string { return "id2" }, CreatedUnix: 1, MaxCallArgumentBytes: 64, MaxTurnBytes: 5})
	frames, err = enc2.Handle(protocol.Event{Type: protocol.EventTextDelta, Text: "123456"})
	if err != nil {
		t.Fatal(err)
	}
	assertBufferFailure(t, frames)
}

func TestEncoderIgnoresNonUserVisibleControlEvents(t *testing.T) {
	enc, _ := New("m", Options{IDGenerator: func() string { return "id" }, CreatedUnix: 1})
	for _, event := range []protocol.Event{{Type: protocol.EventHeartbeat}, {Type: protocol.EventThinkingSignature, Signature: "secret"}, {Type: protocol.EventRedactedThinking, Data: "secret"}, {Type: protocol.EventKiroRedactedReasoning, Data: "secret"}, {Type: protocol.EventAssistantBoundary}} {
		frames, err := enc.Handle(event)
		if err != nil || len(frames) != 0 {
			t.Fatalf("event=%s frames=%#v err=%v", event.Type, frames, err)
		}
	}
}

func TestEncoderBindsWebSearchSourcesAsUrlCitations(t *testing.T) {
	enc, _ := New("m", Options{IDGenerator: func() string { return "id" }, CreatedUnix: 1})
	begin, err := enc.Handle(protocol.Event{Type: protocol.EventWebSearchCallBegin, ID: "ws"})
	if err != nil || len(begin) != 0 {
		t.Fatalf("begin frames=%#v err=%v", begin, err)
	}
	end, err := enc.Handle(protocol.Event{
		Type:    protocol.EventWebSearchCallEnd,
		ID:      "ws",
		Status:  "completed",
		Sources: []protocol.URLCitation{{URL: "https://example.com/a", Title: "Example A"}, {URL: "https://example.com/a", Title: "Duplicate"}, {URL: "", Title: "blank"}},
	})
	if err != nil || len(end) != 0 {
		t.Fatalf("end frames=%#v err=%v", end, err)
	}
	if _, err := enc.Handle(protocol.Event{Type: protocol.EventTextDelta, Text: "hello"}); err != nil {
		t.Fatal(err)
	}
	if _, err := enc.Handle(protocol.Event{Type: protocol.EventDone}); err != nil {
		t.Fatal(err)
	}
	completion, err := enc.Completion()
	if err != nil {
		t.Fatal(err)
	}
	msg := completion["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)
	annotations, ok := msg["annotations"].([]any)
	if !ok || len(annotations) != 1 {
		t.Fatalf("annotations=%#v", msg["annotations"])
	}
	citation := annotations[0].(map[string]any)
	if citation["type"] != "url_citation" || citation["url"] != "https://example.com/a" || citation["title"] != "Example A" {
		t.Fatalf("citation=%#v", citation)
	}
	if citation["start_index"] != 0 || citation["end_index"] != 0 {
		t.Fatalf("indexes=%#v", citation)
	}
}

func TestNewRejectsBlankModelAndInvalidLimits(t *testing.T) {
	if _, err := New(" ", Options{}); err == nil {
		t.Fatal("blank model accepted")
	}
	if _, err := New("m", Options{MaxCallArgumentBytes: -1}); err == nil {
		t.Fatal("negative call limit accepted")
	}
	if _, err := New("m", Options{MaxTurnBytes: -1}); err == nil {
		t.Fatal("negative turn limit accepted")
	}
}

func assertChoiceDelta(t *testing.T, frame Frame, want map[string]any, finish any) {
	t.Helper()
	choice := firstChoice(t, frame)
	if !reflect.DeepEqual(choice["delta"], want) || choice["finish_reason"] != finish {
		t.Fatalf("choice=%#v want delta=%#v finish=%#v", choice, want, finish)
	}
}
func firstChoice(t *testing.T, frame Frame) map[string]any {
	t.Helper()
	if frame.Done {
		t.Fatal("expected data frame")
	}
	if frame.Payload["id"] != "chatcmpl-fixed" && frame.Payload["id"] != "id" && frame.Payload["id"] != "id2" {
		t.Fatalf("id=%#v", frame.Payload["id"])
	}
	choices, ok := frame.Payload["choices"].([]any)
	if !ok || len(choices) != 1 {
		t.Fatalf("payload=%#v", frame.Payload)
	}
	return choices[0].(map[string]any)
}
func assertBufferFailure(t *testing.T, frames []Frame) {
	t.Helper()
	if len(frames) != 1 || frames[0].Done {
		t.Fatalf("frames=%#v", frames)
	}
	errBody := frames[0].Payload["error"].(map[string]any)
	if errBody["code"] != "translation_buffer_limit" || errBody["type"] != "upstream_error" {
		t.Fatalf("error=%#v", errBody)
	}
}

func TestCompletionRequiresSuccessfulTerminal(t *testing.T) {
	enc, _ := New("m", Options{})
	if _, err := enc.Completion(); !errors.Is(err, ErrNotTerminal) {
		t.Fatalf("err=%v", err)
	}
}

func TestChatUsageMarksUnconfirmedXAIPriority(t *testing.T) {
	enc, err := New("xai/grok-4.6", Options{IDGenerator: func() string { return "id" }, CreatedUnix: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := enc.Handle(protocol.Event{Type: protocol.EventDone, Usage: &protocol.Usage{InputTokens: 9, OutputTokens: 1}}); err != nil {
		t.Fatal(err)
	}
	completion, err := enc.Completion()
	if err != nil {
		t.Fatal(err)
	}
	usage := completion["usage"].(map[string]any)
	if usage["xai_priority_applied"] != false || usage["xai_lower_bound"] != false {
		t.Fatalf("usage=%#v", usage)
	}
}

func TestChatUsageMarksRequestedLongContextXAIPriorityAsLowerBound(t *testing.T) {
	tier := "priority"
	enc, err := New("xai/grok-4.6", Options{IDGenerator: func() string { return "id" }, CreatedUnix: 1, RequestedServiceTier: &tier})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := enc.Handle(protocol.Event{Type: protocol.EventDone, Usage: &protocol.Usage{InputTokens: 200_000, OutputTokens: 1}}); err != nil {
		t.Fatal(err)
	}
	completion, err := enc.Completion()
	if err != nil {
		t.Fatal(err)
	}
	usage := completion["usage"].(map[string]any)
	if usage["xai_priority_applied"] != false || usage["xai_lower_bound"] != true {
		t.Fatalf("usage=%#v", usage)
	}
}

func TestChatUsageBillsConfirmedXAIPriority(t *testing.T) {
	enc, err := New("xai/grok-4.6", Options{IDGenerator: func() string { return "id" }, CreatedUnix: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := enc.Handle(protocol.Event{Type: protocol.EventDone, Usage: &protocol.Usage{InputTokens: 9, OutputTokens: 1, ServiceTier: "priority"}}); err != nil {
		t.Fatal(err)
	}
	completion, err := enc.Completion()
	if err != nil {
		t.Fatal(err)
	}
	usage := completion["usage"].(map[string]any)
	if usage["xai_priority_applied"] != true || usage["xai_lower_bound"] != false {
		t.Fatalf("usage=%#v", usage)
	}
}
