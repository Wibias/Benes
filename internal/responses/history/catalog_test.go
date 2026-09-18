package history

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/responses/request"
)

func rawTool(s string) json.RawMessage { return json.RawMessage(s) }

func catalogItem(raw string) request.Item {
	var meta struct {
		Type string `json:"type"`
		Role string `json:"role"`
	}
	_ = json.Unmarshal([]byte(raw), &meta)
	return request.Item{Type: meta.Type, Role: meta.Role, Raw: json.RawMessage(raw)}
}

func TestToolCatalogBuildsFunctionsNamespacesCustomAndSearch(t *testing.T) {
	req := &request.Request{Model: "m", Tools: []json.RawMessage{
		rawTool(`{"type":"function","name":"plain","description":"p","parameters":{"properties":{"x":{"type":"string"}}},"strict":false}`),
		rawTool(`{"type":"namespace","name":"mcp__ctx","tools":[{"type":"function","name":"lookup","description":"l","parameters":{"type":"object"}}]}`),
		rawTool(`{"type":"custom","name":"apply_patch","description":"patch"}`),
		rawTool(`{"type":"tool_search"}`),
		rawTool(`{"type":"web_search","name":"hosted_search"}`),
		rawTool(`{"type":"image_generation","name":"hosted_image"}`),
		rawTool(`{"type":"computer_use_preview","name":"computer","parameters":null}`),
	}}
	ctx, err := Build(req, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(ctx.Tools) != 6 {
		t.Fatalf("tools=%+v", ctx.Tools)
	}
	plain := ctx.Tools[0]
	if plain.Name != "plain" || plain.Parameters["type"] != "object" || plain.Strict == nil || *plain.Strict {
		t.Fatalf("plain=%+v", plain)
	}
	ns := ctx.Tools[1]
	if ns.Name != "lookup" || ns.Namespace != "mcp__ctx" {
		t.Fatalf("ns=%+v", ns)
	}
	custom := ctx.Tools[2]
	if !custom.Freeform || custom.Parameters["type"] != "object" {
		t.Fatalf("custom=%+v", custom)
	}
	search := ctx.Tools[3]
	if !search.ToolSearch || search.Name != "tool_search" {
		t.Fatalf("search=%+v", search)
	}
	hosted := ctx.Tools[4]
	if !hosted.HostedWebSearch || hosted.Name != "hosted_search" {
		t.Fatalf("hosted=%+v", hosted)
	}
	generic := ctx.Tools[5]
	if generic.Name != "computer" || generic.Parameters["type"] != "object" {
		t.Fatalf("generic=%+v", generic)
	}
}

func TestToolCatalogKeepsNamespacedCustomTools(t *testing.T) {
	req := &request.Request{Model: "m", Tools: []json.RawMessage{
		rawTool(`{"type":"namespace","name":"mcp","tools":[{"type":"custom","name":"exec","description":"run"}]}`),
	}}
	ctx, err := Build(req, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(ctx.Tools) != 1 || ctx.Tools[0].Name != "exec" || ctx.Tools[0].Namespace != "mcp" || !ctx.Tools[0].Freeform {
		t.Fatalf("tools=%+v", ctx.Tools)
	}
}

func TestAdditionalToolsAreLoadedAndDoNotCreateMessage(t *testing.T) {
	req := &request.Request{Model: "m", Input: request.Input{Items: []request.Item{
		catalogItem(`{"type":"additional_tools","tools":[{"type":"function","name":"late","parameters":{}}]}`),
	}}}
	ctx, err := Build(req, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(ctx.Messages) != 0 || len(ctx.Tools) != 1 || ctx.Tools[0].Name != "late" || !ctx.Tools[0].LoadedFromToolSearch {
		t.Fatalf("ctx=%+v", ctx)
	}
}

func TestDeclaredDuplicateWinsDefinitionButIsMarkedLoaded(t *testing.T) {
	req := &request.Request{Model: "m", Tools: []json.RawMessage{rawTool(`{"type":"function","name":"dup","description":"declared","parameters":{}}`)}, Input: request.Input{Items: []request.Item{
		catalogItem(`{"type":"additional_tools","tools":[{"type":"function","name":"dup","description":"loaded","parameters":{}}]}`),
	}}}
	ctx, err := Build(req, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(ctx.Tools) != 1 || ctx.Tools[0].Description != "declared" || !ctx.Tools[0].LoadedFromToolSearch {
		t.Fatalf("tools=%+v", ctx.Tools)
	}
}

func TestToolSearchCallAndOutputReplayExactWireNames(t *testing.T) {
	req := &request.Request{Model: "m", Input: request.Input{Items: []request.Item{
		catalogItem(`{"type":"reasoning","summary":[{"type":"summary_text","text":"think"}]}`),
		catalogItem(`{"type":"tool_search_call","call_id":"ts1","arguments":{"query":"db"}}`),
		catalogItem(`{"type":"tool_search_output","call_id":"ts1","status":"completed","tools":[{"type":"namespace","name":"mcp","tools":[{"type":"function","name":"read","parameters":{}}]},{"type":"function","name":"plain","parameters":{}}]}`),
	}}}
	ctx, err := Build(req, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(ctx.Messages) != 2 {
		t.Fatalf("messages=%+v", ctx.Messages)
	}
	a := ctx.Messages[0]
	if len(a.Content) != 2 || a.Content[0].Type != protocol.ContentThinking || a.Content[1].ToolName != "tool_search" || a.Content[1].ToolCallID != "ts1" || a.Content[1].Arguments["query"] != "db" {
		t.Fatalf("assistant=%+v", a)
	}
	r := ctx.Messages[1]
	want := "Tool search loaded these tools — they are now in your available tools. Call one by its EXACT name: mcp__read, plain."
	if r.Role != protocol.RoleToolResult || r.ToolName != "tool_search" || r.ToolCallID != "ts1" || r.IsError || len(r.Content) != 1 || r.Content[0].Text != want {
		t.Fatalf("result=%+v", r)
	}
	if len(ctx.Tools) != 2 || ctx.Tools[0].Namespace != "mcp" || ctx.Tools[0].Name != "read" || !ctx.Tools[0].LoadedFromToolSearch || !ctx.Tools[1].LoadedFromToolSearch {
		t.Fatalf("tools=%+v", ctx.Tools)
	}
}

func TestToolSearchFailureWithoutToolsIsError(t *testing.T) {
	req := &request.Request{Model: "m", Input: request.Input{Items: []request.Item{
		catalogItem(`{"type":"tool_search_output","call_id":"ts","status":"failed"}`),
	}}}
	ctx, err := Build(req, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(ctx.Messages) != 1 || !ctx.Messages[0].IsError || ctx.Messages[0].Content[0].Text != "Tool search failed (status: failed)." {
		t.Fatalf("ctx=%+v", ctx)
	}
}

func TestStringInputStillBuildsDeclaredTools(t *testing.T) {
	text := "hello"
	req := &request.Request{Model: "m", Tools: []json.RawMessage{rawTool(`{"type":"function","name":"t","parameters":{}}`)}, Input: request.Input{Text: &text}}
	ctx, err := Build(req, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(ctx.Messages) != 1 || len(ctx.Tools) != 1 || ctx.Tools[0].Name != "t" {
		t.Fatalf("ctx=%+v", ctx)
	}
}

func TestNoToolsKeepsCatalogNil(t *testing.T) {
	ctx, err := Build(&request.Request{Model: "m"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Tools != nil {
		t.Fatalf("tools=%#v", ctx.Tools)
	}
}

func TestAdditionalToolsDoNotBreakPendingReasoningOwnership(t *testing.T) {
	req := &request.Request{Model: "m", Input: request.Input{Items: []request.Item{
		catalogItem(`{"type":"reasoning","summary":[{"type":"summary_text","text":"think"}]}`),
		catalogItem(`{"type":"additional_tools","tools":[{"type":"function","name":"late"}]}`),
		catalogItem(`{"type":"function_call","call_id":"c","name":"late","arguments":"{}"}`),
	}}}
	ctx, err := Build(req, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(ctx.Messages) != 1 || len(ctx.Messages[0].Content) != 2 || ctx.Messages[0].Content[0].Type != protocol.ContentThinking || ctx.Messages[0].Content[1].ToolCallID != "c" {
		t.Fatalf("ctx=%+v", ctx)
	}
}

func TestToolSearchOutputClearsUnownedPendingReasoning(t *testing.T) {
	req := &request.Request{Model: "m", Input: request.Input{Items: []request.Item{
		catalogItem(`{"type":"reasoning","summary":[{"type":"summary_text","text":"drop"}]}`),
		catalogItem(`{"type":"tool_search_output","call_id":"ts","status":"completed","tools":[]}`),
		catalogItem(`{"role":"assistant","content":"answer"}`),
	}}}
	ctx, err := Build(req, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(ctx.Messages) != 2 || len(ctx.Messages[1].Content) != 1 || ctx.Messages[1].Content[0].Type != protocol.ContentText {
		t.Fatalf("ctx=%+v", ctx)
	}
}

func TestToolSearchFailedWithReturnedToolsStillReportsLoadedTools(t *testing.T) {
	req := &request.Request{Model: "m", Input: request.Input{Items: []request.Item{
		catalogItem(`{"type":"tool_search_output","call_id":"ts","status":"failed","tools":[{"type":"function","name":"usable"}]}`),
	}}}
	ctx, err := Build(req, 1)
	if err != nil {
		t.Fatal(err)
	}
	m := ctx.Messages[0]
	if m.IsError || !strings.Contains(m.Content[0].Text, "usable") || len(ctx.Tools) != 1 {
		t.Fatalf("ctx=%+v", ctx)
	}
}

func TestMalformedToolSpecsAreSkippedWithoutInventingTools(t *testing.T) {
	req := &request.Request{Model: "m", Tools: []json.RawMessage{
		rawTool(`42`), rawTool(`{"type":"function"}`), rawTool(`{"type":"namespace","name":"x","tools":"bad"}`), rawTool(`{"type":"custom","name":""}`),
	}}
	ctx, err := Build(req, 1)
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Tools != nil {
		t.Fatalf("tools=%+v", ctx.Tools)
	}
}
