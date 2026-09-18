package conformance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers/kiro"
)

func TestConformancePackageHasNoProductionGoFiles(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") {
			t.Errorf("conformance harness must stay outside production dependency paths: %s", filepath.Join("internal/conformance", name))
		}
	}
}

func TestProvenanceRejectsMissingSourceVersionAndReviewer(t *testing.T) {
	cases := []Provenance{
		{},
		{Source: "synthetic:openai-responses/text", CapturedAt: "2026-04-01T00:00:00Z", Reviewer: "harness"},
		{Source: "synthetic:openai-responses/text@v1", Reviewer: "harness"},
		{Source: "synthetic:openai-responses/text@v1", CapturedAt: "2026-04-01T00:00:00Z"},
	}
	for _, p := range cases {
		if err := p.Validate(); err == nil {
			t.Fatalf("Validate accepted incomplete provenance %#v", p)
		}
	}
}

func TestResponsesTextAndTerminalSemantics(t *testing.T) {
	p := Provenance{Source: "synthetic:openai-responses/text-terminal@v1", CapturedAt: "2026-04-01T00:00:00Z", Reviewer: "harness"}
	mustProvenance(t, p)
	parsed := parseResponses(t, `{
		"model":"openai-apikey/gpt-5.6",
		"store":false,
		"stream":true,
		"input":[{"role":"user","content":"hi"}]
	}`, "gpt-5.6")
	captured, events, err := openResponses(t, parsed, sseBytes(
		`{"type":"response.output_text.delta","delta":"hello"}`,
		`{"type":"response.completed","response":{"usage":{"input_tokens":3,"output_tokens":1,"total_tokens":4}}}`,
	))
	if err != nil {
		t.Fatal(err)
	}
	if captured.Method != "POST" || !strings.Contains(captured.URL, "/v1/responses") {
		t.Fatalf("captured HTTP=%s %s", captured.Method, captured.URL)
	}
	requireJSONSubset(t, captured.Body, map[string]any{
		"model":  "gpt-5.6",
		"stream": true,
		"store":  false,
		"input": []any{
			map[string]any{"type": "message", "role": "user"},
		},
	})
	requireEventTypes(t, events, protocol.EventTextDelta, protocol.EventDone)
	if concatText(events) != "hello" {
		t.Fatalf("text=%q events=%#v", concatText(events), events)
	}
	if events[1].Usage == nil || events[1].Usage.TotalTokens != 4 {
		t.Fatalf("done=%#v", events[1])
	}
}

func TestResponsesToolRoundTripAndStream(t *testing.T) {
	p := Provenance{Source: "synthetic:openai-responses/tools-roundtrip@v1", CapturedAt: "2026-04-01T00:00:00Z", Reviewer: "harness"}
	mustProvenance(t, p)
	parsed := parseResponses(t, `{
		"model":"openai-apikey/gpt-5.6",
		"store":false,
		"input":[
			{"role":"user","content":"lookup x"},
			{"type":"function_call","call_id":"call_1","name":"lookup","arguments":"{\"q\":\"x\"}"},
			{"type":"function_call_output","call_id":"call_1","output":"result"}
		],
		"tools":[{"type":"function","name":"lookup","description":"look up","parameters":{"type":"object"}}]
	}`, "gpt-5.6")
	captured, events, err := openResponses(t, parsed, sseBytes(
		`{"type":"response.output_item.added","item":{"type":"function_call","id":"fc_1","call_id":"call_2","name":"lookup","arguments":""}}`,
		`{"type":"response.function_call_arguments.delta","item_id":"fc_1","delta":"{\"q\":\"y\"}"}`,
		`{"type":"response.function_call_arguments.done","item_id":"fc_1","arguments":"{\"q\":\"y\"}"}`,
		`{"type":"response.completed","response":{"usage":{"input_tokens":8,"output_tokens":2,"total_tokens":10}}}`,
	))
	if err != nil {
		t.Fatal(err)
	}
	requireJSONSubset(t, captured.Body, map[string]any{
		"model": "gpt-5.6",
		"tools": []any{map[string]any{"name": "lookup"}},
		"input": []any{
			map[string]any{"role": "user"},
			map[string]any{"type": "function_call", "call_id": "call_1", "name": "lookup"},
			map[string]any{"type": "function_call_output", "call_id": "call_1", "output": "result"},
		},
	})
	requireEventTypes(t, events, protocol.EventToolCallStart, protocol.EventToolCallDelta, protocol.EventToolCallEnd, protocol.EventDone)
	if events[0].ID != "call_2" || events[0].Name != "lookup" {
		t.Fatalf("start=%#v", events[0])
	}
	if events[1].Arguments != `{"q":"y"}` {
		t.Fatalf("delta=%#v", events[1])
	}
}

func TestResponsesStructuredReasoningImageAndReplay(t *testing.T) {
	p := Provenance{Source: "synthetic:openai-responses/structured-reasoning-image-replay@v1", CapturedAt: "2026-04-01T00:00:00Z", Reviewer: "harness"}
	mustProvenance(t, p)
	parsed := parseResponses(t, `{
		"model":"openai-apikey/gpt-5.6",
		"store":false,
		"previous_response_id":"resp_prev",
		"reasoning":{"effort":"high"},
		"text":{"format":{"type":"json_schema","name":"answer","schema":{"type":"object"},"strict":true}},
		"input":[{"role":"user","content":[
			{"type":"input_text","text":"describe"},
			{"type":"input_image","image_url":"https://img.example/a.png","detail":"high"}
		]}]
	}`, "gpt-5.6")
	captured, events, err := openResponses(t, parsed, sseBytes(
		`{"type":"response.reasoning_summary_text.delta","delta":"plan"}`,
		`{"type":"response.output_text.delta","delta":"{\"ok\":true}"}`,
		`{"type":"response.completed","response":{"id":"resp_next","usage":{"input_tokens":9,"output_tokens":3,"total_tokens":12}}}`,
	))
	if err != nil {
		t.Fatal(err)
	}
	requireJSONSubset(t, captured.Body, map[string]any{
		"previous_response_id": "resp_prev",
		"reasoning":            map[string]any{"effort": "high", "summary": "none"},
		"text":                 map[string]any{"format": map[string]any{"type": "json_schema", "name": "answer", "strict": true}},
		"input": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "input_text", "text": "describe"},
					map[string]any{"type": "input_image", "image_url": "https://img.example/a.png", "detail": "high"},
				},
			},
		},
	})
	requireEventTypes(t, events, protocol.EventThinkingDelta, protocol.EventTextDelta, protocol.EventDone)
	if events[0].Thinking != "plan" {
		t.Fatalf("thinking=%#v", events[0])
	}
}

func TestResponsesMalformedStreamAndTerminalError(t *testing.T) {
	p := Provenance{Source: "synthetic:openai-responses/malformed-terminal@v1", CapturedAt: "2026-04-01T00:00:00Z", Reviewer: "harness"}
	mustProvenance(t, p)
	parsed := parseResponses(t, `{"model":"openai-apikey/gpt-5.6","store":false,"input":[{"role":"user","content":"hi"}]}`, "gpt-5.6")

	_, _, err := openResponses(t, parsed, sseBytes(`{"type":"response.output_item.added","item":{"type":"not_a_migrated_item"}}`))
	if err == nil {
		t.Fatal("malformed/unsupported stream item must fail")
	}

	captured, events, err := openResponses(t, parsed, sseBytes(
		`{"type":"response.output_text.delta","delta":"partial"}`,
		`{"type":"response.failed","response":{"status":"failed","error":{"message":"provider failed"},"usage":{"input_tokens":7,"output_tokens":2,"total_tokens":9}}}`,
	))
	if err != nil {
		t.Fatal(err)
	}
	if len(captured.Body) == 0 {
		t.Fatal("terminal-error scenario captured no request")
	}
	requireEventTypes(t, events, protocol.EventTextDelta, protocol.EventError)
	if events[1].Message == "" || events[1].Usage == nil || events[1].Usage.TotalTokens != 9 {
		t.Fatalf("error=%#v", events[1])
	}
}

func TestChatCompletionsTextToolsAndTerminal(t *testing.T) {
	p := Provenance{Source: "synthetic:openai-chat/tools-roundtrip@v1", CapturedAt: "2026-04-01T00:00:00Z", Reviewer: "harness"}
	mustProvenance(t, p)
	parsed := parseChat(t, `{
		"model":"gpt-4o",
		"stream":true,
		"messages":[
			{"role":"user","content":"lookup x"},
			{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"q\":\"x\"}"}}]},
			{"role":"tool","tool_call_id":"call_1","content":"result"}
		],
		"tools":[{"type":"function","function":{"name":"lookup","description":"look up","parameters":{"type":"object"}}}]
	}`, "gpt-4o")
	captured, events, err := openChat(t, parsed, sseBytes(
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_2","type":"function","function":{"name":"lookup","arguments":"{\"q\":"}}]},"finish_reason":null}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"y\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":8,"completion_tokens":2,"total_tokens":10}}`,
		`[DONE]`,
	))
	if err != nil {
		t.Fatal(err)
	}
	requireJSONSubset(t, captured.Body, map[string]any{
		"model":  "gpt-4o",
		"stream": true,
		"tools":  []any{map[string]any{"type": "function", "function": map[string]any{"name": "lookup"}}},
		"messages": []any{
			map[string]any{"role": "user"},
			map[string]any{"role": "assistant"},
			map[string]any{"role": "tool", "tool_call_id": "call_1"},
		},
	})
	requireEventTypes(t, events, protocol.EventToolCallStart, protocol.EventToolCallDelta, protocol.EventToolCallEnd, protocol.EventDone)
	if firstToolName(events) != "lookup" {
		t.Fatalf("tool=%q events=%#v", firstToolName(events), events)
	}
}

func TestAnthropicInboundCompileAndOutboundFrames(t *testing.T) {
	p := Provenance{Source: "synthetic:anthropic-messages/tools-roundtrip@v1", CapturedAt: "2026-04-01T00:00:00Z", Reviewer: "harness"}
	mustProvenance(t, p)
	parsed := parseAnthropic(t, `{
		"model":"claude-sonnet-4",
		"max_tokens":64,
		"stream":true,
		"messages":[{"role":"user","content":"lookup x"}],
		"tools":[{"name":"lookup","description":"look up","input_schema":{"type":"object"}}]
	}`, "gpt-5.6")
	captured, events, err := openResponses(t, parsed, sseBytes(
		`{"type":"response.output_text.delta","delta":"calling"}`,
		`{"type":"response.output_item.added","item":{"type":"function_call","id":"fc_1","call_id":"toolu_1","name":"lookup","arguments":""}}`,
		`{"type":"response.function_call_arguments.delta","item_id":"fc_1","delta":"{\"q\":\"x\"}"}`,
		`{"type":"response.function_call_arguments.done","item_id":"fc_1","arguments":"{\"q\":\"x\"}"}`,
		`{"type":"response.completed","response":{"usage":{"input_tokens":5,"output_tokens":3,"total_tokens":8}}}`,
	))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Source != protocol.RequestSourceAnthropicMessages {
		t.Fatalf("source=%s", parsed.Source)
	}
	requireJSONSubset(t, captured.Body, map[string]any{
		"model":  "gpt-5.6",
		"stream": true,
		"tools":  []any{map[string]any{"name": "lookup"}},
		"input":  []any{map[string]any{"role": "user"}},
	})
	frames := encodeAnthropic(t, parsed.ModelID, events)
	var names []string
	for _, frame := range frames {
		names = append(names, frame.Name)
	}
	joined := strings.Join(names, ",")
	if !strings.Contains(joined, "message_start") || !strings.Contains(joined, "content_block_start") || !strings.Contains(joined, "message_stop") {
		t.Fatalf("anthropic frames=%v", names)
	}
}

func TestGoogleCompileCaptureAndStream(t *testing.T) {
	p := Provenance{Source: "synthetic:google-generatecontent/tools-roundtrip@v1", CapturedAt: "2026-04-01T00:00:00Z", Reviewer: "harness"}
	mustProvenance(t, p)
	parsed := googleParsed("lookup x", []protocol.Tool{{Name: "lookup", Description: "look up", Parameters: map[string]any{"type": "object"}}},
		protocol.Message{Role: protocol.RoleAssistant, Content: []protocol.ContentPart{{
			Type: protocol.ContentToolCall, ToolCallID: "c1", ToolName: "lookup", Arguments: map[string]any{"q": "x"},
		}}},
		protocol.Message{Role: protocol.RoleToolResult, ToolCallID: "c1", ToolName: "lookup", Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "result"}}},
	)
	captured, events, err := openGoogle(t, parsed, sseBytes(
		`{"candidates":[{"content":{"parts":[{"functionCall":{"name":"lookup","id":"c2","args":{"q":"y"}}}]},"finishReason":"STOP"}]}`,
	))
	if err != nil {
		t.Fatal(err)
	}
	if captured.Method != "POST" || !strings.Contains(captured.URL, "gemini-3.7-flash") {
		t.Fatalf("captured HTTP=%s %s", captured.Method, captured.URL)
	}
	requireJSONSubset(t, captured.Body, map[string]any{
		"contents": []any{
			map[string]any{"role": "user"},
			map[string]any{"role": "model"},
			map[string]any{"role": "user"},
		},
		"tools": []any{map[string]any{"functionDeclarations": []any{map[string]any{"name": "lookup"}}}},
	})
	requireEventTypes(t, events, protocol.EventToolCallEnd, protocol.EventDone)
	if events[0].Name != "lookup" || events[0].ID != "c2" {
		t.Fatalf("tool=%#v", events[0])
	}
}

func TestKiroCompileCaptureAndStream(t *testing.T) {
	p := Provenance{Source: "synthetic:kiro-generate/tools-roundtrip@v1", CapturedAt: "2026-04-01T00:00:00Z", Reviewer: "harness"}
	mustProvenance(t, p)
	parsed := kiroParsed("lookup x", []protocol.Tool{{Name: "lookup", Description: "look up", Parameters: map[string]any{"type": "object"}}},
		protocol.Message{Role: protocol.RoleAssistant, Content: []protocol.ContentPart{{
			Type: protocol.ContentToolCall, ToolCallID: "c1", ToolName: "lookup", Arguments: map[string]any{"q": "x"},
		}}},
		protocol.Message{Role: protocol.RoleToolResult, ToolCallID: "c1", ToolName: "lookup", Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "result"}}},
	)
	start := kiro.EncodeEventStreamMessage(map[string]string{":event-type": "toolUseEvent"}, []byte(`{"name":"lookup","toolUseId":"c2"}`))
	delta := kiro.EncodeEventStreamMessage(map[string]string{":event-type": "toolUseEvent"}, []byte(`{"name":"lookup","toolUseId":"c2","input":"{\"q\":\"y\"}"}`))
	end := kiro.EncodeEventStreamMessage(map[string]string{":event-type": "toolUseEvent"}, []byte(`{"name":"lookup","toolUseId":"c2","input":"{\"q\":\"y\"}","stop":true}`))
	meta := kiro.EncodeEventStreamMessage(map[string]string{":event-type": "metadataEvent"}, []byte(`{"usage":{"inputTokens":6,"outputTokens":2}}`))
	stream := append(append(append(start, delta...), end...), meta...)
	captured, events, err := openKiro(t, parsed, stream)
	if err != nil {
		t.Fatal(err)
	}
	if captured.Header.Get("x-amz-target") != "AmazonCodeWhispererStreamingService.GenerateAssistantResponse" {
		t.Fatalf("target=%q", captured.Header.Get("x-amz-target"))
	}
	requireJSONSubset(t, captured.Body, map[string]any{
		"conversationState": map[string]any{
			"chatTriggerType": "MANUAL",
			"history": []any{
				map[string]any{"userInputMessage": map[string]any{"content": "lookup x"}},
				map[string]any{"assistantResponseMessage": map[string]any{
					"toolUses": []any{map[string]any{"name": "lookup", "toolUseId": "c1"}},
				}},
			},
			"currentMessage": map[string]any{
				"userInputMessage": map[string]any{
					"userInputMessageContext": map[string]any{
						"toolResults": []any{map[string]any{"toolUseId": "c1", "status": "success"}},
						"tools":       []any{map[string]any{"toolSpecification": map[string]any{"name": "lookup"}}},
					},
				},
			},
		},
	})
	requireEventTypes(t, events, protocol.EventToolCallStart, protocol.EventToolCallDelta, protocol.EventToolCallEnd, protocol.EventDone)
	if firstToolName(events) != "lookup" {
		t.Fatalf("tool=%q events=%#v", firstToolName(events), events)
	}
}

func TestMutatedCompilerFailsCorrespondingScenario(t *testing.T) {
	parsed := parseResponses(t, `{
		"model":"openai-apikey/gpt-5.6",
		"store":false,
		"input":[{"role":"user","content":"lookup x"}],
		"tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}]
	}`, "gpt-5.6")
	body := compileResponses(t, parsed)
	want := map[string]any{"tools": []any{map[string]any{"name": "lookup"}}}
	if err := jsonSubset(mustObject(t, body), want); err != nil {
		t.Fatalf("baseline compiler: %v", err)
	}
	mutated := mutateDropKey(body, "tools")
	if err := jsonSubset(mustObject(t, mutated), want); err == nil {
		t.Fatal("dropping tools from a mutated compiler must fail the tools assertion")
	}
}

func TestMutatedParserFailsCorrespondingScenario(t *testing.T) {
	parsed := parseResponses(t, `{"model":"openai-apikey/gpt-5.6","store":false,"input":[{"role":"user","content":"hi"}]}`, "gpt-5.6")
	_, events, err := openResponses(t, parsed, sseBytes(
		`{"type":"response.output_text.delta","delta":"hello"}`,
		`{"type":"response.completed","response":{"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`,
	))
	if err != nil {
		t.Fatal(err)
	}
	if concatText(events) != "hello" {
		t.Fatalf("baseline parser text=%q", concatText(events))
	}
	mutatedStream := mutateReplaceText(
		sseBytes(`{"type":"response.output_text.delta","delta":"hello"}`, `{"type":"response.completed","response":{"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`),
		"hello",
		"nope",
	)
	_, mutated, err := openResponses(t, parsed, mutatedStream)
	if err != nil {
		t.Fatal(err)
	}
	if concatText(mutated) == "hello" {
		t.Fatal("mutated parser still produced the original text; assertion is vacuous")
	}
	if concatText(mutated) != "nope" {
		t.Fatalf("mutated parser text=%q", concatText(mutated))
	}
}

func TestChatMalformedStreamFailsClosed(t *testing.T) {
	parsed := parseChat(t, `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`, "gpt-4o")
	_, events, err := openChat(t, parsed, sseBytes(`{"choices":[{"delta":{"tool_calls":{"id":"bad"}},"finish_reason":null}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type != protocol.EventError {
		t.Fatalf("malformed chat stream=%#v", events)
	}
}

func mustObject(t *testing.T, raw []byte) any {
	t.Helper()
	var got any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	return got
}
