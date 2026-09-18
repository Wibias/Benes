package openairesponses

import (
	"encoding/json"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
)

func TestPrepareCanonicalBodyAnthropicSourceIgnoresAnthropicRawSemantics(t *testing.T) {
	req := protocol.ParsedRequest{
		Source:          protocol.RequestSourceAnthropicMessages,
		UpstreamModelID: "gpt-real",
		Stream:          false,
		Raw:             json.RawMessage(`{"model":"claude-wire","store":true,"conversation":"must-not-be-read","tools":[{"type":"web_search_20250305"}],"messages":[{"role":"user","content":"raw"}]}`),
		Context: protocol.Context{Messages: []protocol.Message{{
			Role:    protocol.RoleUser,
			Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "canonical"}},
		}}},
		Options: protocol.RequestOptions{Temperature: f64(0.25)},
	}

	body, err := prepareCanonicalBody(req)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if got["model"] != "gpt-real" || got["stream"] != true || got["temperature"] != 0.25 {
		t.Fatalf("body=%#v", got)
	}
	if _, exists := got["conversation"]; exists {
		t.Fatalf("Anthropic raw field leaked into Responses body: %#v", got)
	}
	input := got["input"].([]any)
	content := input[0].(map[string]any)["content"].([]any)
	if content[0].(map[string]any)["text"] != "canonical" {
		t.Fatalf("input=%#v", input)
	}
}

func TestCompileMarksTextOnlyToolErrors(t *testing.T) {
	req := protocol.ParsedRequest{
		Source:          protocol.RequestSourceAnthropicMessages,
		UpstreamModelID: "gpt",
		Context: protocol.Context{Messages: []protocol.Message{{
			Role:       protocol.RoleToolResult,
			ToolCallID: "call_1",
			IsError:    true,
			Content: []protocol.ContentPart{{
				Type: protocol.ContentText,
				Text: "boom",
			}},
		}}},
	}
	body, err := prepareCanonicalBody(req)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	input := got["input"].([]any)
	output := input[0].(map[string]any)["output"]
	if output != "[tool error] boom" {
		t.Fatalf("output=%#v", output)
	}
}

func TestCompilePrependsMarkerToMultimodalToolErrors(t *testing.T) {
	req := protocol.ParsedRequest{
		Source:          protocol.RequestSourceAnthropicMessages,
		UpstreamModelID: "gpt",
		Context: protocol.Context{Messages: []protocol.Message{{
			Role:       protocol.RoleToolResult,
			ToolCallID: "call_1",
			IsError:    true,
			Content: []protocol.ContentPart{
				{Type: protocol.ContentText, Text: "boom"},
				{Type: protocol.ContentImage, ImageURL: "https://example.test/image.png"},
			},
		}}},
	}
	body, err := prepareCanonicalBody(req)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	input := got["input"].([]any)
	output := input[0].(map[string]any)["output"].([]any)
	if len(output) != 3 {
		t.Fatalf("output=%#v", output)
	}
	first := output[0].(map[string]any)
	second := output[1].(map[string]any)
	third := output[2].(map[string]any)
	if first["type"] != "input_text" || first["text"] != "[tool error]" {
		t.Fatalf("marker=%#v", first)
	}
	if second["type"] != "input_text" || second["text"] != "boom" {
		t.Fatalf("text=%#v", second)
	}
	if third["type"] != "input_image" || third["image_url"] != "https://example.test/image.png" {
		t.Fatalf("image=%#v", third)
	}
}

func TestCompileLeavesSuccessfulToolResultsUnchanged(t *testing.T) {
	req := protocol.ParsedRequest{
		Source:          protocol.RequestSourceAnthropicMessages,
		UpstreamModelID: "gpt",
		Context: protocol.Context{Messages: []protocol.Message{{
			Role:       protocol.RoleToolResult,
			ToolCallID: "call_1",
			Content:    []protocol.ContentPart{{Type: protocol.ContentText, Text: "ok"}},
		}}},
	}
	body, err := prepareCanonicalBody(req)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	input := got["input"].([]any)
	if output := input[0].(map[string]any)["output"]; output != "ok" {
		t.Fatalf("output=%#v", output)
	}
}

func TestCompileMarksEmptyToolErrorWithoutTrailingSpace(t *testing.T) {
	req := protocol.ParsedRequest{
		Source:          protocol.RequestSourceAnthropicMessages,
		UpstreamModelID: "gpt",
		Context: protocol.Context{Messages: []protocol.Message{{
			Role:       protocol.RoleToolResult,
			ToolCallID: "call_1",
			IsError:    true,
			Content:    []protocol.ContentPart{{Type: protocol.ContentText, Text: ""}},
		}}},
	}
	body, err := prepareCanonicalBody(req)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	input := got["input"].([]any)
	if output := input[0].(map[string]any)["output"]; output != "[tool error]" {
		t.Fatalf("output=%#v", output)
	}
}
