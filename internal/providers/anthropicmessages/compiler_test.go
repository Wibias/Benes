package anthropicmessages

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
)

func TestCompileCanonicalRequestWithoutRawPassthrough(t *testing.T) {
	maxTokens := float64(123)
	temperature := 0.2
	topP := 0.8
	parallel := false
	req := protocol.ParsedRequest{
		UpstreamModelID: "minimax-m3",
		Stream:          true,
		Raw:             json.RawMessage(`{"raw_escape":"must-not-survive"}`),
		Context: protocol.Context{
			SystemPrompt: []string{"system one", "system two"},
			Tools: []protocol.Tool{{
				Namespace:   "fs",
				Name:        "read",
				Description: "Read a file",
				Parameters:  map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}},
			}},
			Messages: []protocol.Message{
				{Role: protocol.RoleUser, Content: []protocol.ContentPart{
					{Type: protocol.ContentText, Text: "hello"},
					{Type: protocol.ContentImage, ImageURL: "data:image/png;base64,AAAA"},
				}},
				{Role: protocol.RoleAssistant, Content: []protocol.ContentPart{
					{Type: protocol.ContentText, Text: "checking"},
					{Type: protocol.ContentToolCall, ToolCallID: "call_1", ToolNamespace: "fs", ToolName: "read", Arguments: map[string]any{"path": "/README.md"}},
				}},
				{Role: protocol.RoleToolResult, ToolCallID: "call_1", ToolNamespace: "fs", ToolName: "read", Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "ok"}}},
			},
		},
		Options: protocol.RequestOptions{
			MaxOutputTokens:   &maxTokens,
			Temperature:       &temperature,
			TopP:              &topP,
			StopSequences:     []string{"STOP"},
			ParallelToolCalls: &parallel,
			ToolChoice:        &protocol.ToolChoice{Kind: protocol.ToolChoiceNamed, Name: "fs.read"},
		},
	}

	body, err := Compile(req)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if _, leaked := got["raw_escape"]; leaked || strings.Contains(string(body), "must-not-survive") {
		t.Fatalf("ParsedRequest.Raw escaped into wire body: %s", body)
	}
	if got["model"] != "minimax-m3" || got["max_tokens"] != float64(123) || got["stream"] != true {
		t.Fatalf("core fields=%#v", got)
	}
	if got["system"] != "system one\n\nsystem two" || got["temperature"] != 0.2 || got["top_p"] != 0.8 {
		t.Fatalf("options/system=%#v", got)
	}
	stops, _ := got["stop_sequences"].([]any)
	if len(stops) != 1 || stops[0] != "STOP" {
		t.Fatalf("stop_sequences=%#v", got["stop_sequences"])
	}
	wireTools, _ := got["tools"].([]any)
	if len(wireTools) != 1 {
		t.Fatalf("tools=%#v", got["tools"])
	}
	tool := wireTools[0].(map[string]any)
	if tool["name"] != "fs__read" || tool["description"] != "Read a file" {
		t.Fatalf("tool=%#v", tool)
	}
	if _, ok := tool["input_schema"].(map[string]any); !ok {
		t.Fatalf("input_schema=%#v", tool["input_schema"])
	}
	choice := got["tool_choice"].(map[string]any)
	if choice["type"] != "tool" || choice["name"] != "fs__read" || choice["disable_parallel_tool_use"] != true {
		t.Fatalf("tool_choice=%#v", choice)
	}

	messages, _ := got["messages"].([]any)
	if len(messages) != 3 {
		t.Fatalf("messages=%#v", got["messages"])
	}
	userContent := messages[0].(map[string]any)["content"].([]any)
	image := userContent[1].(map[string]any)
	source := image["source"].(map[string]any)
	if image["type"] != "image" || source["type"] != "base64" || source["media_type"] != "image/png" || source["data"] != "AAAA" {
		t.Fatalf("image=%#v", image)
	}
	assistantContent := messages[1].(map[string]any)["content"].([]any)
	call := assistantContent[1].(map[string]any)
	if call["type"] != "tool_use" || call["id"] != "call_1" || call["name"] != "fs__read" {
		t.Fatalf("tool_use=%#v", call)
	}
	resultContent := messages[2].(map[string]any)["content"].([]any)
	result := resultContent[0].(map[string]any)
	if result["type"] != "tool_result" || result["tool_use_id"] != "call_1" || result["content"] != "ok" {
		t.Fatalf("tool_result=%#v", result)
	}
}

func TestCompileSuppliesDeterministicDefaultMaxTokens(t *testing.T) {
	body, err := Compile(protocol.ParsedRequest{
		UpstreamModelID: "qwen3.8-max",
		Context: protocol.Context{Messages: []protocol.Message{{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if got["max_tokens"] != float64(DefaultMaxOutputTokens) {
		t.Fatalf("max_tokens=%#v want %d", got["max_tokens"], DefaultMaxOutputTokens)
	}
}

func TestCompileAcceptsZeroMaxTokensForCacheWarmup(t *testing.T) {
	zero := float64(0)
	body, err := Compile(protocol.ParsedRequest{
		UpstreamModelID: "m",
		Context: protocol.Context{Messages: []protocol.Message{{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}}}}},
		Options: protocol.RequestOptions{MaxOutputTokens: &zero},
	})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if got["max_tokens"] != float64(0) {
		t.Fatalf("max_tokens=%#v", got["max_tokens"])
	}
}

func TestCompileUsesCurrentNativeOutputConfigShape(t *testing.T) {
	req := protocol.ParsedRequest{
		UpstreamModelID: "m",
		Context: protocol.Context{Messages: []protocol.Message{{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}}}}},
		Options: protocol.RequestOptions{
			Reasoning: "xhigh",
			TextFormat: &protocol.TextFormat{
				Type:        "json_schema",
				Name:        "neutral-name-must-not-escape",
				Description: "neutral-description-must-not-escape",
				Schema: map[string]any{
					"type": "object",
					"properties": map[string]any{"answer": map[string]any{"type": "string"}},
				},
			},
		},
	}
	body, err := Compile(req)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	output := got["output_config"].(map[string]any)
	if output["effort"] != "xhigh" {
		t.Fatalf("effort=%#v", output["effort"])
	}
	format := output["format"].(map[string]any)
	if format["type"] != "json_schema" {
		t.Fatalf("format=%#v", format)
	}
	if _, ok := format["schema"].(map[string]any); !ok {
		t.Fatalf("format schema=%#v", format["schema"])
	}
	if _, exists := format["name"]; exists {
		t.Fatalf("neutral format name escaped to Anthropic wire: %#v", format)
	}
	if _, exists := format["description"]; exists {
		t.Fatalf("neutral format description escaped to Anthropic wire: %#v", format)
	}
}

func TestCompileOmitsNonAnthropicEffortLevels(t *testing.T) {
	for _, effort := range []string{"minimal", "ultra"} {
		body, err := Compile(protocol.ParsedRequest{
			UpstreamModelID: "m",
			Context: protocol.Context{Messages: []protocol.Message{{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}}}}},
			Options: protocol.RequestOptions{Reasoning: effort},
		})
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]any
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatal(err)
		}
		if output, ok := got["output_config"].(map[string]any); ok {
			if _, exists := output["effort"]; exists {
				t.Fatalf("unsupported effort %q escaped to Anthropic wire: %#v", effort, output)
			}
		}
	}
}

func TestCompileToolChoiceNoneDoesNotCarryParallelField(t *testing.T) {
	parallel := false
	body, err := Compile(protocol.ParsedRequest{
		UpstreamModelID: "m",
		Context: protocol.Context{
			Tools: []protocol.Tool{{Name: "lookup", Parameters: map[string]any{"type": "object"}}},
			Messages: []protocol.Message{{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}}}},
		},
		Options: protocol.RequestOptions{
			ToolChoice:        &protocol.ToolChoice{Kind: protocol.ToolChoiceNone},
			ParallelToolCalls: &parallel,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	choice := got["tool_choice"].(map[string]any)
	if choice["type"] != "none" || len(choice) != 1 {
		t.Fatalf("tool_choice=%#v", choice)
	}
}

func TestCompilePreservesStrictToolDefinition(t *testing.T) {
	strict := true
	body, err := Compile(protocol.ParsedRequest{
		UpstreamModelID: "m",
		Context: protocol.Context{
			Tools: []protocol.Tool{{Name: "lookup", Strict: &strict, Parameters: map[string]any{"type": "object"}}},
			Messages: []protocol.Message{{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	tools := got["tools"].([]any)
	tool := tools[0].(map[string]any)
	if tool["strict"] != true {
		t.Fatalf("strict=%#v tool=%#v", tool["strict"], tool)
	}
}

func TestCompileRejectsUnsupportedHostedAndFreeformTools(t *testing.T) {
	for _, tool := range []protocol.Tool{
		{Name: "web_search", HostedWebSearch: true},
		{Name: "exec", Freeform: true},
	} {
		_, err := Compile(protocol.ParsedRequest{
			UpstreamModelID: "m",
			Context: protocol.Context{Tools: []protocol.Tool{tool}, Messages: []protocol.Message{{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}}}}},
		})
		if err == nil {
			t.Fatalf("Compile accepted unsupported tool %#v", tool)
		}
	}
}

func TestCompileRejectsUnsupportedImageSource(t *testing.T) {
	_, err := Compile(protocol.ParsedRequest{
		UpstreamModelID: "m",
		Context: protocol.Context{Messages: []protocol.Message{{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentImage, ImageURL: "file:///secret.png"}}}}},
	})
	if err == nil {
		t.Fatal("Compile accepted unsupported image source")
	}
}

func TestCompileRejectsUnsupportedBase64ImageMediaType(t *testing.T) {
	_, err := Compile(protocol.ParsedRequest{
		UpstreamModelID: "m",
		Context: protocol.Context{Messages: []protocol.Message{{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentImage, ImageURL: "data:image/svg+xml;base64,PHN2Zz48L3N2Zz4="}}}}},
	})
	if err == nil {
		t.Fatal("Compile accepted unsupported Anthropic base64 image media type")
	}
}
