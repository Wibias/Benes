package openairesponses

import (
	"encoding/json"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/responses/websearchcall"
)

func TestCompileEmitsHealedWebSearchCallWithoutDuplicateFunctionCall(t *testing.T) {
	req := protocol.ParsedRequest{
		UpstreamModelID: "gpt-5.6",
		Stream:          true,
		Context: protocol.Context{
			Messages: []protocol.Message{
				{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "search"}}},
				{Role: protocol.RoleAssistant, Content: []protocol.ContentPart{
					websearchcall.HostedPart("ws_1", "one", []string{"one", "two"}),
					{Type: protocol.ContentToolCall, ToolCallID: "call_1", ToolName: "lookup", Arguments: map[string]any{"q": "x"}},
				}},
				{Role: protocol.RoleToolResult, ToolCallID: "call_1", ToolName: "lookup", Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "ok"}}},
			},
			Tools: []protocol.Tool{{Name: "lookup", Parameters: map[string]any{"type": "object"}}},
		},
	}
	encoded, err := Compile(req)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(encoded, &body); err != nil {
		t.Fatal(err)
	}
	input, _ := body["input"].([]any)
	if len(input) != 4 {
		t.Fatalf("input=%#v", input)
	}
	search := input[1].(map[string]any)
	if search["type"] != "web_search_call" || search["id"] != "ws_1" {
		t.Fatalf("search=%#v", search)
	}
	action := search["action"].(map[string]any)
	if action["query"] != "one" {
		t.Fatalf("action=%#v", action)
	}
	queries, _ := action["queries"].([]any)
	if len(queries) != 2 {
		t.Fatalf("queries=%#v", action["queries"])
	}
	call := input[2].(map[string]any)
	if call["type"] != "function_call" || call["call_id"] != "call_1" {
		t.Fatalf("call=%#v", call)
	}
	for _, raw := range input {
		item := raw.(map[string]any)
		if item["type"] == "function_call" && item["name"] == "web_search" {
			t.Fatalf("duplicate function_call inserted: %#v", input)
		}
		if item["type"] == "function_call_output" && item["call_id"] == "ws_1" {
			t.Fatalf("duplicate tool output inserted: %#v", input)
		}
	}
}

func TestValidateMigratedRequestAllowsWebSearchCallHistory(t *testing.T) {
	request := canonicalRequest(t, `{"model":"openai-apikey/gpt-5.6","store":false,"input":[{"role":"user","content":"hi"},{"type":"web_search_call","id":"ws_1","action":{"queries":["one","two"]}}]}`, "gpt-5.6")
	if err := ValidateMigratedRequest(request.Parsed); err != nil {
		t.Fatalf("ValidateMigratedRequest(): %v", err)
	}
	if len(request.Parsed.Context.Messages) != 2 {
		t.Fatalf("messages=%#v", request.Parsed.Context.Messages)
	}
	part := request.Parsed.Context.Messages[1].Content[0]
	if !websearchcall.IsHosted(part) || part.Arguments["query"] != "one" {
		t.Fatalf("part=%#v", part)
	}
}
