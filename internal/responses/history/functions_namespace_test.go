package history

import (
	"encoding/json"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/responses/request"
)

func TestCodexFunctionsNamespaceFlattensBuiltinsAndNestedCustom(t *testing.T) {
	req := &request.Request{Model: "m", Tools: []json.RawMessage{
		rawTool(`{"type":"namespace","name":"functions","tools":[{"type":"custom","name":"exec","description":"Run JavaScript with nested helpers."},{"type":"function","name":"wait","parameters":{"type":"object","properties":{}}}]}`),
		rawTool(`{"type":"namespace","name":"collaboration","tools":[{"type":"function","name":"spawn_agent","parameters":{"type":"object","properties":{}}}]}`),
	}}
	ctx, err := Build(req, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(ctx.Tools) != 3 {
		t.Fatalf("tools=%+v", ctx.Tools)
	}
	exec := findCatalogTool(ctx.Tools, "exec")
	if exec == nil || !exec.Freeform || exec.Namespace != "" {
		t.Fatalf("exec=%+v", exec)
	}
	wait := findCatalogTool(ctx.Tools, "wait")
	if wait == nil || wait.Namespace != "" {
		t.Fatalf("wait=%+v", wait)
	}
	spawn := findCatalogTool(ctx.Tools, "spawn_agent")
	if spawn == nil || spawn.Namespace != "collaboration" {
		t.Fatalf("spawn=%+v", spawn)
	}
}

func TestCodexFunctionsNamespaceToolSearchMessageUsesFlattenedWireNames(t *testing.T) {
	req := &request.Request{Model: "m", Input: request.Input{Items: []request.Item{
		catalogItem(`{"type":"tool_search_output","call_id":"ts","status":"completed","tools":[{"type":"namespace","name":"functions","tools":[{"type":"custom","name":"exec"},{"type":"function","name":"wait","parameters":{}}]},{"type":"namespace","name":"collaboration","tools":[{"type":"function","name":"spawn_agent","parameters":{}}]}]}`),
	}}}
	ctx, err := Build(req, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(ctx.Messages) != 1 {
		t.Fatalf("messages=%+v", ctx.Messages)
	}
	want := "Tool search loaded these tools — they are now in your available tools. Call one by its EXACT name: exec, wait, collaboration__spawn_agent."
	if got := ctx.Messages[0].Content[0].Text; got != want {
		t.Fatalf("message=%q want %q", got, want)
	}
}

func findCatalogTool(tools []protocol.Tool, name string) *protocol.Tool {
	for i := range tools {
		if tools[i].Name == name {
			return &tools[i]
		}
	}
	return nil
}
