package outbound

import (
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
)

func names(frames []Frame) []string {
	out := make([]string, len(frames))
	for i := range frames {
		out[i] = frames[i].Name
	}
	return out
}

func TestEncoderExactThinkingTextToolSequenceAndMessage(t *testing.T) {
	e, err := New("provider/model", Options{IDGenerator: func() string { return "msg_test" }, SignatureGenerator: func() string { return "sig_synth" }})
	if err != nil {
		t.Fatal(err)
	}

	frames, _ := e.Handle(protocol.Event{Type: protocol.EventThinkingDelta, Thinking: "hmm"})
	want := []string{"message_start", "ping", "content_block_start", "content_block_delta"}
	if got := names(frames); !equalStrings(got, want) {
		t.Fatalf("thinking names=%#v", got)
	}
	if frames[2].Payload["content_block"].(map[string]any)["type"] != "thinking" {
		t.Fatalf("start=%#v", frames[2])
	}

	frames, _ = e.Handle(protocol.Event{Type: protocol.EventTextDelta, Text: "Hello"})
	want = []string{"content_block_delta", "content_block_stop", "content_block_start", "content_block_delta"}
	if got := names(frames); !equalStrings(got, want) {
		t.Fatalf("text names=%#v", got)
	}
	if frames[0].Payload["delta"].(map[string]any)["signature"] != "sig_synth" {
		t.Fatalf("signature=%#v", frames[0])
	}

	frames, _ = e.Handle(protocol.Event{Type: protocol.EventToolCallStart, ID: "toolu_1", Name: "Read"})
	want = []string{"content_block_stop", "content_block_start"}
	if got := names(frames); !equalStrings(got, want) {
		t.Fatalf("tool start names=%#v", got)
	}
	block := frames[1].Payload["content_block"].(map[string]any)
	if block["type"] != "tool_use" || block["id"] != "toolu_1" || block["name"] != "Read" {
		t.Fatalf("block=%#v", block)
	}

	frames, _ = e.Handle(protocol.Event{Type: protocol.EventToolCallDelta, ID: "toolu_1", Arguments: `{"path":"/x"}`})
	if got := names(frames); !equalStrings(got, []string{"content_block_delta"}) {
		t.Fatalf("tool delta names=%#v", got)
	}
	if frames[0].Payload["delta"].(map[string]any)["partial_json"] != `{"path":"/x"}` {
		t.Fatalf("delta=%#v", frames[0])
	}

	frames, _ = e.Handle(protocol.Event{Type: protocol.EventToolCallEnd, ID: "toolu_1"})
	if got := names(frames); !equalStrings(got, []string{"content_block_stop"}) {
		t.Fatalf("tool end=%#v", got)
	}
	frames, _ = e.Handle(protocol.Event{Type: protocol.EventHeartbeat})
	if got := names(frames); !equalStrings(got, []string{"ping"}) {
		t.Fatalf("ping=%#v", got)
	}

	frames, _ = e.Handle(protocol.Event{Type: protocol.EventDone, Usage: &protocol.Usage{InputTokens: 120, OutputTokens: 30, CachedInputTokens: 100, CacheCreationInputTokens: 5}})
	want = []string{"message_delta", "message_stop"}
	if got := names(frames); !equalStrings(got, want) {
		t.Fatalf("done names=%#v", got)
	}
	delta := frames[0].Payload["delta"].(map[string]any)
	if delta["stop_reason"] != "tool_use" || delta["stop_sequence"] != nil {
		t.Fatalf("delta=%#v", delta)
	}
	usage := frames[0].Payload["usage"].(map[string]any)
	if usage["input_tokens"] != int64(15) || usage["output_tokens"] != int64(30) || usage["cache_read_input_tokens"] != int64(100) || usage["cache_creation_input_tokens"] != int64(5) {
		t.Fatalf("usage=%#v", usage)
	}

	message, err := e.Message()
	if err != nil {
		t.Fatal(err)
	}
	if message["id"] != "msg_test" || message["model"] != "provider/model" || message["stop_reason"] != "tool_use" {
		t.Fatalf("message=%#v", message)
	}
	content := message["content"].([]any)
	if len(content) != 3 {
		t.Fatalf("content=%#v", content)
	}
	thinking := content[0].(map[string]any)
	if thinking["type"] != "thinking" || thinking["thinking"] != "hmm" || thinking["signature"] != "sig_synth" {
		t.Fatalf("thinking=%#v", thinking)
	}
	text := content[1].(map[string]any)
	if text["type"] != "text" || text["text"] != "Hello" {
		t.Fatalf("text=%#v", text)
	}
	tool := content[2].(map[string]any)
	if tool["type"] != "tool_use" || tool["id"] != "toolu_1" || tool["name"] != "Read" || tool["input"].(map[string]any)["path"] != "/x" {
		t.Fatalf("tool=%#v", tool)
	}
}

func TestEncoderUsesCanonicalThinkingSignatureWhenProvided(t *testing.T) {
	e, _ := New("m", Options{IDGenerator: func() string { return "msg" }, SignatureGenerator: func() string { return "synthetic" }})
	_, _ = e.Handle(protocol.Event{Type: protocol.EventThinkingDelta, Thinking: "a"})
	frames, _ := e.Handle(protocol.Event{Type: protocol.EventThinkingSignature, Signature: "actual"})
	if len(frames) != 1 || frames[0].Payload["delta"].(map[string]any)["signature"] != "actual" {
		t.Fatalf("frames=%#v", frames)
	}
	frames, _ = e.Handle(protocol.Event{Type: protocol.EventTextDelta, Text: "b"})
	if len(frames) == 0 || frames[0].Name != "content_block_stop" {
		t.Fatalf("duplicate synthetic signature emitted: %#v", frames)
	}
}

func TestEncoderInitialErrorDoesNotManufactureMessageStart(t *testing.T) {
	e, _ := New("m", Options{IDGenerator: func() string { return "msg" }})
	frames, _ := e.Handle(protocol.Event{Type: protocol.EventError, Message: "busy", HTTPStatus: 529})
	if got := names(frames); !equalStrings(got, []string{"error"}) {
		t.Fatalf("names=%#v", got)
	}
	errBody := frames[0].Payload["error"].(map[string]any)
	if errBody["type"] != "overloaded_error" || errBody["message"] != "busy" {
		t.Fatalf("error=%#v", errBody)
	}
	if _, err := e.Message(); err == nil {
		t.Fatal("failed stream produced a successful Message")
	}
}

func TestEncoderIncompleteMaxTokensProducesSuccessfulAnthropicStop(t *testing.T) {
	e, _ := New("m", Options{IDGenerator: func() string { return "msg" }})
	_, _ = e.Handle(protocol.Event{Type: protocol.EventTextDelta, Text: "cut"})
	frames, _ := e.Handle(protocol.Event{Type: protocol.EventIncomplete, Reason: "max_output_tokens", Usage: &protocol.Usage{InputTokens: 2, OutputTokens: 3}})
	if got := names(frames); !equalStrings(got, []string{"content_block_stop", "message_delta", "message_stop"}) {
		t.Fatalf("names=%#v", got)
	}
	if frames[1].Payload["delta"].(map[string]any)["stop_reason"] != "max_tokens" {
		t.Fatalf("delta=%#v", frames[1])
	}
}

func TestEncoderWebSearchEmitsServerToolPairAndUsage(t *testing.T) {
	e, _ := New("m", Options{IDGenerator: func() string { return "msg" }})
	begin, err := e.Handle(protocol.Event{Type: protocol.EventWebSearchCallBegin, ID: "ws_1"})
	if err != nil || len(begin) != 0 {
		t.Fatalf("begin=%#v err=%v", begin, err)
	}
	frames, err := e.Handle(protocol.Event{
		Type:    protocol.EventWebSearchCallEnd,
		ID:      "ws_1",
		Status:  "completed",
		Queries: []string{"latest rust release"},
		Sources: []protocol.URLCitation{{URL: "https://example.com/blog", Title: "Example Blog"}, {URL: "https://example.com/releases"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := names(frames); !equalStrings(got, []string{
		"message_start", "ping",
		"content_block_start", "content_block_delta", "content_block_stop",
		"content_block_start", "content_block_stop",
	}) {
		t.Fatalf("names=%#v", got)
	}
	block := frames[2].Payload["content_block"].(map[string]any)
	if block["type"] != "server_tool_use" || block["id"] != "ws_1" || block["name"] != "web_search" {
		t.Fatalf("server tool=%#v", block)
	}
	if frames[3].Payload["delta"].(map[string]any)["partial_json"] != `{"query":"latest rust release"}` {
		t.Fatalf("delta=%#v", frames[3])
	}
	result := frames[5].Payload["content_block"].(map[string]any)
	if result["type"] != "web_search_tool_result" || result["tool_use_id"] != "ws_1" {
		t.Fatalf("result=%#v", result)
	}
	hits := result["content"].([]any)
	if len(hits) != 2 {
		t.Fatalf("hits=%#v", hits)
	}
	first := hits[0].(map[string]any)
	if first["type"] != "web_search_result" || first["url"] != "https://example.com/blog" || first["title"] != "Example Blog" {
		t.Fatalf("first=%#v", first)
	}

	_, _ = e.Handle(protocol.Event{Type: protocol.EventTextDelta, Text: "answer"})
	done, err := e.Handle(protocol.Event{Type: protocol.EventDone, Usage: &protocol.Usage{InputTokens: 9, OutputTokens: 3}})
	if err != nil {
		t.Fatal(err)
	}
	usage := done[1].Payload["usage"].(map[string]any)
	if usage["server_tool_use"].(map[string]any)["web_search_requests"] != 1 {
		t.Fatalf("usage=%#v", usage)
	}
	if done[1].Payload["delta"].(map[string]any)["stop_reason"] != "end_turn" {
		t.Fatalf("delta=%#v", done[1])
	}
	message, err := e.Message()
	if err != nil {
		t.Fatal(err)
	}
	content := message["content"].([]any)
	if content[0].(map[string]any)["type"] != "server_tool_use" || content[1].(map[string]any)["type"] != "web_search_tool_result" {
		t.Fatalf("content=%#v", content)
	}
	if message["usage"].(map[string]any)["server_tool_use"].(map[string]any)["web_search_requests"] != 1 {
		t.Fatalf("message usage=%#v", message["usage"])
	}
}

func TestEncoderWebSearchFailedSearchIsNotCounted(t *testing.T) {
	e, _ := New("m", Options{IDGenerator: func() string { return "msg" }})
	frames, err := e.Handle(protocol.Event{Type: protocol.EventWebSearchCallEnd, ID: "ws_9", Status: "failed"})
	if err != nil {
		t.Fatal(err)
	}
	result := frames[5].Payload["content_block"].(map[string]any)
	content := result["content"].(map[string]any)
	if content["type"] != "web_search_tool_result_error" || content["error_code"] != "unavailable" {
		t.Fatalf("result=%#v", result)
	}
	done, _ := e.Handle(protocol.Event{Type: protocol.EventDone})
	if _, exists := done[0].Payload["usage"].(map[string]any)["server_tool_use"]; exists {
		t.Fatalf("failed search counted: %#v", done[0].Payload["usage"])
	}
}

func TestEncoderWebSearchBatchQueriesKeepPluralForm(t *testing.T) {
	e, _ := New("m", Options{IDGenerator: func() string { return "msg" }})
	frames, err := e.Handle(protocol.Event{Type: protocol.EventWebSearchCallEnd, ID: "ws_2", Status: "completed", Queries: []string{"a", "b"}})
	if err != nil {
		t.Fatal(err)
	}
	if frames[3].Payload["delta"].(map[string]any)["partial_json"] != `{"queries":["a","b"]}` {
		t.Fatalf("delta=%#v", frames[3])
	}
}

func TestAnthropicUsageMarksUnconfirmedXAIPriority(t *testing.T) {
	e, err := New("xai/grok-4.6", Options{IDGenerator: func() string { return "msg" }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Handle(protocol.Event{Type: protocol.EventDone, Usage: &protocol.Usage{InputTokens: 9, OutputTokens: 1}}); err != nil {
		t.Fatal(err)
	}
	message, err := e.Message()
	if err != nil {
		t.Fatal(err)
	}
	usage := message["usage"].(map[string]any)
	if usage["xai_priority_applied"] != false || usage["xai_lower_bound"] != false {
		t.Fatalf("usage=%#v", usage)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
