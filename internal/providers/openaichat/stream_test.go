package openaichat

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/responses/sse"
)

func chatSSE(payloads ...string) string {
	var builder strings.Builder
	for _, payload := range payloads {
		builder.WriteString("data: ")
		builder.WriteString(payload)
		builder.WriteString("\n\n")
	}
	return builder.String()
}

func collectChatEvents(t *testing.T, body string) []protocol.Event {
	t.Helper()
	stream := NewStream(strings.NewReader(body), sse.Limits{MaxLineBytes: 1 << 20, MaxEventBytes: 1 << 20})
	var events []protocol.Event
	for {
		event, err := stream.Next()
		if errors.Is(err, io.EOF) {
			return events
		}
		if err != nil {
			t.Fatalf("Next(): %v", err)
		}
		events = append(events, event)
	}
}

func TestStreamMapsTextReasoningUsageAndLengthFinish(t *testing.T) {
	events := collectChatEvents(t, chatSSE(
		`{"choices":[{"delta":{"reasoning_content":"think ","content":"hello"},"finish_reason":null}]}`,
		`{"choices":[{"delta":{"reasoning":"more"},"finish_reason":"length"}],"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8,"prompt_tokens_details":{"cached_tokens":2},"completion_tokens_details":{"reasoning_tokens":1}}}`,
		`[DONE]`,
	))
	want := []protocol.EventType{protocol.EventReasoningRawDelta, protocol.EventTextDelta, protocol.EventReasoningRawDelta, protocol.EventDone}
	if len(events) != len(want) {
		t.Fatalf("events=%#v", events)
	}
	for index := range want {
		if events[index].Type != want[index] {
			t.Fatalf("event[%d]=%s want=%s", index, events[index].Type, want[index])
		}
	}
	if events[0].Text != "think " || events[1].Text != "hello" || events[2].Text != "more" {
		t.Fatalf("events=%#v", events)
	}
	done := events[3]
	if done.StopReason != "max_tokens" || done.Usage == nil || done.Usage.InputTokens != 5 || done.Usage.OutputTokens != 3 || done.Usage.TotalTokens != 8 || done.Usage.CachedInputTokens != 2 || done.Usage.ReasoningOutputTokens != 1 {
		t.Fatalf("done=%#v", done)
	}
}

func TestStreamCapturesConfirmedServiceTier(t *testing.T) {
	events := collectChatEvents(t, chatSSE(
		`{"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":1,"total_tokens":6},"service_tier":"priority"}`,
		`[DONE]`,
	))
	var done protocol.Event
	for _, event := range events {
		if event.Type == protocol.EventDone {
			done = event
		}
	}
	if done.Usage == nil || done.Usage.ServiceTier != "priority" {
		t.Fatalf("done=%#v", done)
	}
}

func TestStreamAccumulatesToolCallAcrossChunks(t *testing.T) {
	events := collectChatEvents(t, chatSSE(
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"q\":"}}]},"finish_reason":null}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":null,"arguments":"\"x\"}"}}]},"finish_reason":"tool_calls"}]}`,
		`[DONE]`,
	))
	want := []protocol.EventType{protocol.EventToolCallStart, protocol.EventToolCallDelta, protocol.EventToolCallEnd, protocol.EventDone}
	if len(events) != len(want) {
		t.Fatalf("events=%#v", events)
	}
	for index := range want {
		if events[index].Type != want[index] {
			t.Fatalf("event[%d]=%s want=%s", index, events[index].Type, want[index])
		}
	}
	if events[0].ID != "call_1" || events[0].Name != "lookup" || events[1].Arguments != `{"q":"x"}` {
		t.Fatalf("events=%#v", events)
	}
}

func TestStreamKeepsParallelToolCallsSeparateByIndex(t *testing.T) {
	events := collectChatEvents(t, chatSSE(
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"a","function":{"name":"first","arguments":"{"}},{"index":1,"id":"b","function":{"name":"second","arguments":"{"}}]},"finish_reason":null}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":1,"function":{"arguments":"}"}},{"index":0,"function":{"arguments":"}"}}]},"finish_reason":"tool_calls"}]}`,
		`[DONE]`,
	))
	if len(events) != 7 {
		t.Fatalf("events=%#v", events)
	}
	if events[0].ID != "a" || events[0].Name != "first" || events[1].Arguments != `{}` || events[3].ID != "b" || events[3].Name != "second" || events[4].Arguments != `{}` || events[6].Type != protocol.EventDone {
		t.Fatalf("events=%#v", events)
	}
}

func TestStreamMintsMissingToolCallID(t *testing.T) {
	events := collectChatEvents(t, chatSSE(
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"lookup","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`,
		`[DONE]`,
	))
	if len(events) != 4 || events[0].Type != protocol.EventToolCallStart || events[0].ID != "call_1" || events[0].Name != "lookup" {
		t.Fatalf("events=%#v", events)
	}
}

func TestStreamFailsClosedOnMalformedToolShape(t *testing.T) {
	events := collectChatEvents(t, chatSSE(`{"choices":[{"delta":{"tool_calls":{"id":"bad"}},"finish_reason":null}]}`))
	if len(events) != 1 || events[0].Type != protocol.EventError || events[0].HTTPStatus != 502 || events[0].ErrorType != "upstream_error" || !strings.Contains(events[0].Message, "invalid tool calls") {
		t.Fatalf("events=%#v", events)
	}
}

func TestStreamFailsClosedOnBlankToolName(t *testing.T) {
	events := collectChatEvents(t, chatSSE(
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c","function":{"name":"   ","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`,
	))
	if len(events) != 1 || events[0].Type != protocol.EventError || !strings.Contains(events[0].Message, "without a function name") {
		t.Fatalf("events=%#v", events)
	}
}

func TestStreamFailsClosedOnMalformedJSON(t *testing.T) {
	events := collectChatEvents(t, chatSSE(`{"choices":`))
	if len(events) != 1 || events[0].Type != protocol.EventError || events[0].Message != "malformed upstream SSE data frame" {
		t.Fatalf("events=%#v", events)
	}
}

func TestStreamMapsUpstreamErrorWithoutEchoingPayload(t *testing.T) {
	events := collectChatEvents(t, chatSSE(`{"error":{"message":"SECRET-UPSTREAM-BODY","code":"bad_request"}}`))
	if len(events) != 1 || events[0].Type != protocol.EventError || events[0].Message != "OpenAI Chat upstream failed" || strings.Contains(events[0].Message, "SECRET") {
		t.Fatalf("events=%#v", events)
	}
}

func TestStreamRejectsInvalidChoicesShape(t *testing.T) {
	events := collectChatEvents(t, chatSSE(`{"choices":{"bad":true}}`))
	if len(events) != 1 || events[0].Type != protocol.EventError || !strings.Contains(events[0].Message, "invalid choices") {
		t.Fatalf("events=%#v", events)
	}
}

func TestStreamDetectsTruncatedPendingToolCall(t *testing.T) {
	events := collectChatEvents(t, chatSSE(`{"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7},"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c","function":{"name":"lookup","arguments":"{"}}]},"finish_reason":null}]}`))
	if len(events) != 1 || events[0].Type != protocol.EventError || !strings.Contains(events[0].Message, "mid tool call") || events[0].Usage == nil || events[0].Usage.TotalTokens != 7 {
		t.Fatalf("events=%#v", events)
	}
}

func TestStreamDetectsEmptyUnterminatedStream(t *testing.T) {
	events := collectChatEvents(t, chatSSE(`{"usage":{"prompt_tokens":1,"completion_tokens":0,"total_tokens":1},"choices":[]}`))
	if len(events) != 1 || events[0].Type != protocol.EventError || !strings.Contains(events[0].Message, "without a terminal signal") || events[0].Usage == nil || events[0].Usage.TotalTokens != 1 {
		t.Fatalf("events=%#v", events)
	}
}

func TestStreamRejectsTextEOFWithoutExplicitTerminal(t *testing.T) {
	events := collectChatEvents(t, chatSSE(`{"usage":{"prompt_tokens":4,"completion_tokens":1,"total_tokens":5},"choices":[{"delta":{"content":"hello"},"finish_reason":null}]}`))
	if len(events) != 2 || events[0].Type != protocol.EventTextDelta || events[0].Text != "hello" || events[1].Type != protocol.EventError || !strings.Contains(events[1].Message, "without a terminal signal") || events[1].Usage == nil || events[1].Usage.TotalTokens != 5 {
		t.Fatalf("events=%#v", events)
	}
}

func TestStreamErrorFinishCarriesCurrentTurnUsage(t *testing.T) {
	events := collectChatEvents(t, chatSSE(`{"usage":{"prompt_tokens":9,"completion_tokens":3,"total_tokens":12},"choices":[{"delta":{},"finish_reason":"error"}]}`))
	if len(events) != 1 || events[0].Type != protocol.EventError || events[0].Usage == nil || events[0].Usage.TotalTokens != 12 {
		t.Fatalf("events=%#v", events)
	}
}

func TestStreamDoneMapsContentFilterFinish(t *testing.T) {
	events := collectChatEvents(t, chatSSE(
		`{"choices":[{"delta":{"content":"x"},"finish_reason":"content_filter"}]}`,
		`[DONE]`,
	))
	if len(events) != 2 || events[1].Type != protocol.EventDone || events[1].StopReason != "content_filter" {
		t.Fatalf("events=%#v", events)
	}
}
