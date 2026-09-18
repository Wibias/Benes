package openaichat

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
)

func decodeBody(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode body: %v\n%s", err, raw)
	}
	return body
}

func TestCompileUsesCanonicalRequestInsteadOfPreservedRaw(t *testing.T) {
	zero := 0.0
	req := protocol.ParsedRequest{
		ModelID:         "openai-compatible/client-selector",
		UpstreamModelID: "gpt-5.6",
		Stream:          true,
		Raw:             json.RawMessage(`{"model":"WRONG","temperature":2,"future_field":"must-not-leak"}`),
		Context: protocol.Context{
			SystemPrompt: []string{"system one", "system two"},
			Messages: []protocol.Message{{
				Role:    protocol.RoleUser,
				Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hello"}},
			}},
		},
		Options: protocol.RequestOptions{
			MaxOutputTokens:  intFloat(321),
			Temperature:      &zero,
			TopP:             floatPtr(0.75),
			StopSequences:    []string{"END"},
			Reasoning:        "high",
			PresencePenalty:  &zero,
			FrequencyPenalty: floatPtr(-0.5),
			PromptCacheKey:   stringPtr("cache-key"),
		},
	}
	raw, err := Compile(req, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	body := decodeBody(t, raw)
	if body["model"] != "gpt-5.6" || body["stream"] != true || body["temperature"] != float64(0) || body["top_p"] != 0.75 || body["max_tokens"] != float64(321) {
		t.Fatalf("body=%#v", body)
	}
	if body["reasoning_effort"] != "high" || body["presence_penalty"] != float64(0) || body["frequency_penalty"] != -0.5 || body["prompt_cache_key"] != "cache-key" {
		t.Fatalf("options=%#v", body)
	}
	if _, ok := body["future_field"]; ok {
		t.Fatalf("raw-only field leaked: %#v", body)
	}
	if opts, ok := body["stream_options"].(map[string]any); !ok || opts["include_usage"] != true {
		t.Fatalf("stream_options=%#v", body["stream_options"])
	}
	messages := body["messages"].([]any)
	if len(messages) != 2 || messages[0].(map[string]any)["role"] != "system" || messages[0].(map[string]any)["content"] != "system one\n\nsystem two" || messages[1].(map[string]any)["content"] != "hello" {
		t.Fatalf("messages=%#v", messages)
	}
}

func TestCompilePreservesNativeDeveloperAndUserImages(t *testing.T) {
	req := protocol.ParsedRequest{UpstreamModelID: "gpt-5.6", Context: protocol.Context{Messages: []protocol.Message{
		{Role: protocol.RoleDeveloper, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "developer rule"}}},
		{Role: protocol.RoleUser, Content: []protocol.ContentPart{
			{Type: protocol.ContentText, Text: "look"},
			{Type: protocol.ContentImage, ImageURL: "data:image/png;base64,AAAA", Detail: "high"},
		}},
	}}}
	raw, err := Compile(req, CompileOptions{NativeOpenAI: true})
	if err != nil {
		t.Fatal(err)
	}
	messages := decodeBody(t, raw)["messages"].([]any)
	if len(messages) != 2 || messages[0].(map[string]any)["role"] != "developer" {
		t.Fatalf("messages=%#v", messages)
	}
	parts := messages[1].(map[string]any)["content"].([]any)
	image := parts[1].(map[string]any)["image_url"].(map[string]any)
	if image["url"] != "data:image/png;base64,AAAA" || image["detail"] != "high" {
		t.Fatalf("parts=%#v", parts)
	}
	for _, message := range messages {
		if strings.Contains(asString(message.(map[string]any)["content"]), "AAAA") {
			t.Fatalf("image data flattened into text: %#v", messages)
		}
	}
}

func TestCompileFoldsCompatibleDeveloperTextIntoSystem(t *testing.T) {
	req := protocol.ParsedRequest{UpstreamModelID: "model-x", Context: protocol.Context{
		SystemPrompt: []string{"sys"},
		Messages: []protocol.Message{
			{Role: protocol.RoleDeveloper, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "dev"}}},
			{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "u"}}},
		},
	}}
	raw, err := Compile(req, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	messages := decodeBody(t, raw)["messages"].([]any)
	if len(messages) != 2 || messages[0].(map[string]any)["role"] != "system" || messages[0].(map[string]any)["content"] != "sys\n\ndev" || messages[1].(map[string]any)["role"] != "user" {
		t.Fatalf("messages=%#v", messages)
	}
}

func TestCompileAssistantReasoningToolCallAndResult(t *testing.T) {
	req := protocol.ParsedRequest{UpstreamModelID: "deepseek-r1", Context: protocol.Context{Messages: []protocol.Message{
		{Role: protocol.RoleAssistant, Content: []protocol.ContentPart{
			{Type: protocol.ContentThinking, Thinking: "think"},
			{Type: protocol.ContentToolCall, ToolCallID: "c1", ToolName: "search", ToolNamespace: "mcp", Arguments: map[string]any{"q": "x"}},
		}},
		{Role: protocol.RoleToolResult, ToolCallID: "c1", ToolName: "search", ToolNamespace: "mcp", Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "ok"}}},
	}}}
	raw, err := Compile(req, CompileOptions{PreserveReasoningContent: true})
	if err != nil {
		t.Fatal(err)
	}
	messages := decodeBody(t, raw)["messages"].([]any)
	if len(messages) != 2 {
		t.Fatalf("messages=%#v", messages)
	}
	a := messages[0].(map[string]any)
	if a["role"] != "assistant" || a["reasoning_content"] != "think" || a["content"] != "" {
		t.Fatalf("assistant=%#v", a)
	}
	calls := a["tool_calls"].([]any)
	fn := calls[0].(map[string]any)["function"].(map[string]any)
	if calls[0].(map[string]any)["id"] != "c1" || fn["name"] != "mcp__search" || fn["arguments"] != `{"q":"x"}` {
		t.Fatalf("calls=%#v", calls)
	}
	result := messages[1].(map[string]any)
	if result["role"] != "tool" || result["tool_call_id"] != "c1" || result["content"] != "ok" {
		t.Fatalf("result=%#v", result)
	}
}

func TestCompileRepairsMissingAndOrphanToolResults(t *testing.T) {
	req := protocol.ParsedRequest{UpstreamModelID: "model-x", Context: protocol.Context{Messages: []protocol.Message{
		{Role: protocol.RoleAssistant, Content: []protocol.ContentPart{{Type: protocol.ContentToolCall, ToolCallID: "lost", ToolName: "first", Arguments: map[string]any{}}}},
		{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "next"}}},
		{Role: protocol.RoleToolResult, ToolCallID: "orphan", ToolName: "second", Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "result"}}},
	}}}
	raw, err := Compile(req, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	messages := decodeBody(t, raw)["messages"].([]any)
	if len(messages) != 5 {
		t.Fatalf("messages=%#v", messages)
	}
	if messages[1].(map[string]any)["role"] != "tool" || messages[1].(map[string]any)["tool_call_id"] != "lost" || !strings.Contains(messages[1].(map[string]any)["content"].(string), "execution status unknown") {
		t.Fatalf("missing repair=%#v", messages[1])
	}
	if messages[2].(map[string]any)["content"] != "next" {
		t.Fatalf("barrier=%#v", messages[2])
	}
	orphanAssistant := messages[3].(map[string]any)
	if orphanAssistant["role"] != "assistant" || orphanAssistant["content"] != "" {
		t.Fatalf("orphan assistant=%#v", orphanAssistant)
	}
	if messages[4].(map[string]any)["role"] != "tool" || messages[4].(map[string]any)["tool_call_id"] != "orphan" {
		t.Fatalf("orphan result=%#v", messages[4])
	}
}

func TestCompileMovesToolResultImagesToFollowupUserMessage(t *testing.T) {
	req := protocol.ParsedRequest{UpstreamModelID: "model-x", Context: protocol.Context{Messages: []protocol.Message{
		{Role: protocol.RoleAssistant, Content: []protocol.ContentPart{{Type: protocol.ContentToolCall, ToolCallID: "c", ToolName: "view", Arguments: map[string]any{}}}},
		{Role: protocol.RoleToolResult, ToolCallID: "c", ToolName: "view", Content: []protocol.ContentPart{
			{Type: protocol.ContentText, Text: "image:"},
			{Type: protocol.ContentImage, ImageURL: "https://example.test/a.png", Detail: "low"},
		}},
	}}}
	raw, err := Compile(req, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	messages := decodeBody(t, raw)["messages"].([]any)
	if len(messages) != 3 || messages[1].(map[string]any)["content"] != "image:" || messages[2].(map[string]any)["role"] != "user" {
		t.Fatalf("messages=%#v", messages)
	}
	parts := messages[2].(map[string]any)["content"].([]any)
	if len(parts) != 2 || parts[1].(map[string]any)["type"] != "image_url" {
		t.Fatalf("parts=%#v", parts)
	}
}

func TestCompileToolsChoiceAndStructuredOutput(t *testing.T) {
	strictFalse := false
	parallelFalse := false
	req := protocol.ParsedRequest{
		UpstreamModelID: "gpt-5.6",
		Context: protocol.Context{Tools: []protocol.Tool{
			{Name: "search", Namespace: "mcp", Description: "find", Parameters: map[string]any{"properties": map[string]any{"q": map[string]any{"type": "string"}}}, Strict: &strictFalse},
			{Name: "other", Parameters: map[string]any{"type": "object"}},
		}},
		Options: protocol.RequestOptions{
			ToolChoice:        &protocol.ToolChoice{Kind: protocol.ToolChoiceAllowed, AllowedTools: []string{"mcp.search"}, AllowedMode: protocol.ToolChoiceModeRequired},
			ParallelToolCalls: &parallelFalse,
			TextFormat:        &protocol.TextFormat{Type: "json_schema", Name: "answer", Description: "shape", Schema: map[string]any{"type": "object"}, Strict: &strictFalse},
		},
	}
	raw, err := Compile(req, CompileOptions{NativeOpenAI: true})
	if err != nil {
		t.Fatal(err)
	}
	body := decodeBody(t, raw)
	tools := body["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("tools=%#v", tools)
	}
	fn := tools[0].(map[string]any)["function"].(map[string]any)
	params := fn["parameters"].(map[string]any)
	if fn["name"] != "mcp__search" || params["type"] != "object" || fn["strict"] != false {
		t.Fatalf("function=%#v", fn)
	}
	choice := body["tool_choice"].(map[string]any)["function"].(map[string]any)
	if choice["name"] != "mcp__search" || body["parallel_tool_calls"] != false {
		t.Fatalf("choice=%#v body=%#v", choice, body)
	}
	format := body["response_format"].(map[string]any)["json_schema"].(map[string]any)
	if format["name"] != "answer" || format["description"] != "shape" || format["strict"] != false {
		t.Fatalf("format=%#v", format)
	}
}

func TestCompileMintsStableMissingToolCallIDs(t *testing.T) {
	req := protocol.ParsedRequest{UpstreamModelID: "m", Context: protocol.Context{Messages: []protocol.Message{
		{Role: protocol.RoleAssistant, Content: []protocol.ContentPart{
			{Type: protocol.ContentToolCall, ToolName: "a", Arguments: map[string]any{}},
			{Type: protocol.ContentToolCall, ToolName: "b", Arguments: map[string]any{}},
		}},
	}}}
	raw, err := Compile(req, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	calls := decodeBody(t, raw)["messages"].([]any)[0].(map[string]any)["tool_calls"].([]any)
	if calls[0].(map[string]any)["id"] != "call_benes_minted_1" || calls[1].(map[string]any)["id"] != "call_benes_minted_2" {
		t.Fatalf("calls=%#v", calls)
	}
}

func TestCompileBlanksClinePassDeepSeekV4AssistantTextWhenReplayingToolCalls(t *testing.T) {
	req := protocol.ParsedRequest{UpstreamModelID: "deepseek-v4-flash", Context: protocol.Context{Messages: []protocol.Message{{
		Role: protocol.RoleAssistant,
		Content: []protocol.ContentPart{
			{Type: protocol.ContentText, Text: "I will call a tool"},
			{Type: protocol.ContentThinking, Thinking: "plan"},
			{Type: protocol.ContentToolCall, ToolCallID: "call_1", ToolName: "lookup", Arguments: map[string]any{"q": "x"}},
		},
	}}}}
	blanked, err := Compile(req, CompileOptions{PreserveReasoningContent: true, BlankAssistantContentWithTools: true})
	if err != nil {
		t.Fatal(err)
	}
	msg := decodeBody(t, blanked)["messages"].([]any)[0].(map[string]any)
	if msg["content"] != "" {
		t.Fatalf("target content=%#v", msg["content"])
	}
	if msg["reasoning_content"] != "plan" {
		t.Fatalf("reasoning=%#v", msg["reasoning_content"])
	}
	calls := msg["tool_calls"].([]any)
	if len(calls) != 1 || calls[0].(map[string]any)["id"] != "call_1" {
		t.Fatalf("calls=%#v", calls)
	}

	kept, err := Compile(req, CompileOptions{PreserveReasoningContent: true})
	if err != nil {
		t.Fatal(err)
	}
	other := decodeBody(t, kept)["messages"].([]any)[0].(map[string]any)
	if other["content"] != "I will call a tool" {
		t.Fatalf("non-target content=%#v", other["content"])
	}

	pro := req
	pro.UpstreamModelID = "deepseek-v4-pro"
	proBlanked, err := Compile(pro, CompileOptions{BlankAssistantContentWithTools: true})
	if err != nil {
		t.Fatal(err)
	}
	if decodeBody(t, proBlanked)["messages"].([]any)[0].(map[string]any)["content"] != "" {
		t.Fatal("pro replay must blank content")
	}
}

func TestCompileRejectsMissingUpstreamModel(t *testing.T) {
	if _, err := Compile(protocol.ParsedRequest{}, CompileOptions{}); err == nil {
		t.Fatal("expected error")
	}
}

func intFloat(v int) *float64     { x := float64(v); return &x }
func floatPtr(v float64) *float64 { return &v }
func stringPtr(v string) *string  { return &v }
func asString(v any) string       { s, _ := v.(string); return s }
