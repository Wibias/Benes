package openairesponses

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/responses/history"
	responsesrequest "github.com/Wibias/Benes/internal/responses/request"
)

func TestStructuredNamespaceCallRoundTripsThroughHistoryAndProviderReplay(t *testing.T) {
	raw := []byte(`{
		"model":"openai-apikey/gpt-5.6",
		"store":false,
		"tools":[{"type":"namespace","name":"exec","tools":[{"type":"function","name":"exec","parameters":{"type":"object"}}]}],
		"input":[
			{"type":"function_call","call_id":"call_1","namespace":"exec","name":"exec","arguments":"{}"},
			{"type":"function_call_output","call_id":"call_1","output":"ok"}
		]
	}`)
	decoded, err := responsesrequest.Decode(bytes.NewReader(raw), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := history.Build(decoded, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(ctx.Messages) != 2 {
		t.Fatalf("messages=%+v", ctx.Messages)
	}
	call := ctx.Messages[0].Content[0]
	if call.Type != protocol.ContentToolCall || call.ToolCallID != "call_1" || call.ToolNamespace != "exec" || call.ToolName != "exec" {
		t.Fatalf("canonical call=%+v", call)
	}
	result := ctx.Messages[1]
	if result.Role != protocol.RoleToolResult || result.ToolCallID != "call_1" || result.ToolNamespace != "exec" || result.ToolName != "exec" {
		t.Fatalf("canonical result=%+v", result)
	}

	compiled, err := Compile(protocol.ParsedRequest{
		UpstreamModelID: "gpt-5.6",
		Context:         ctx,
		Options:         protocol.RequestOptions{Store: decoded.Store},
	})
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(compiled, &body); err != nil {
		t.Fatal(err)
	}
	input, ok := body["input"].([]any)
	if !ok || len(input) != 2 {
		t.Fatalf("compiled input=%#v", body["input"])
	}
	providerCall, ok := input[0].(map[string]any)
	if !ok || providerCall["type"] != "function_call" || providerCall["call_id"] != "call_1" || providerCall["name"] != "exec__exec" {
		t.Fatalf("provider call=%#v", input[0])
	}
	providerResult, ok := input[1].(map[string]any)
	if !ok || providerResult["type"] != "function_call_output" || providerResult["call_id"] != "call_1" {
		t.Fatalf("provider result=%#v", input[1])
	}
}
