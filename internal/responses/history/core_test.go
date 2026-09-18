package history

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/responses/request"
)

func TestStringInputBecomesUserMessage(t *testing.T) {
	ctx := parse(t, request.Input{Text: strptr("hello")})
	if len(ctx.Messages) != 1 || ctx.Messages[0].Role != protocol.RoleUser || ctx.Messages[0].Content[0].Text != "hello" {
		t.Fatalf("%+v", ctx)
	}
}
func TestSystemUserDeveloperAndAssistant(t *testing.T) {
	in := request.Input{Items: []request.Item{
		item(t, `{"role":"system","content":"sys"}`),
		item(t, `{"role":"user","content":[{"type":"input_text","text":"u"}]}`),
		item(t, `{"role":"developer","content":"d"}`),
		item(t, `{"role":"assistant","phase":"commentary","content":[{"type":"output_text","text":"a"}]}`),
	}}
	ctx := parse(t, in)
	if len(ctx.SystemPrompt) != 1 || ctx.SystemPrompt[0] != "sys" {
		t.Fatalf("sys=%v", ctx.SystemPrompt)
	}
	if len(ctx.Messages) != 3 {
		t.Fatalf("msgs=%+v", ctx.Messages)
	}
	if ctx.Messages[2].Role != protocol.RoleAssistant || ctx.Messages[2].Phase == nil || *ctx.Messages[2].Phase != protocol.PhaseCommentary || ctx.Messages[2].Model != "model-x" {
		t.Fatalf("assistant=%+v", ctx.Messages[2])
	}
}
func TestReasoningBeforeFunctionCallStaysInSameAssistantTurn(t *testing.T) {
	in := request.Input{Items: []request.Item{
		item(t, `{"type":"reasoning","summary":[{"type":"summary_text","text":"think"}]}`),
		item(t, `{"type":"function_call","id":"fc_wire","call_id":"call1","name":"tool","arguments":"{\"x\":1}"}`),
	}}
	ctx := parse(t, in)
	m := ctx.Messages[0]
	if len(m.Content) != 2 || m.Content[0].Type != protocol.ContentThinking || m.Content[1].Type != protocol.ContentToolCall || m.Content[1].ToolCallID != "call1" {
		t.Fatalf("%+v", m)
	}
	if _, ok := m.Content[1].Arguments["x"]; !ok {
		t.Fatalf("args=%v", m.Content[1].Arguments)
	}
	if m.Content[1].ThoughtSignature != "" {
		t.Fatalf("wire id leaked to thoughtSignature")
	}
}
func TestInvalidOrNonObjectToolArgumentsDefaultEmpty(t *testing.T) {
	for _, args := range []string{"", `not json`, `[]`, `42`} {
		raw := `{"type":"function_call","call_id":"c","name":"t","arguments":` + j(t, args) + `}`
		ctx := parse(t, request.Input{Items: []request.Item{item(t, raw)}})
		a := ctx.Messages[0].Content[0].Arguments
		if len(a) != 0 {
			t.Fatalf("args %q => %v", args, a)
		}
	}
}
func TestReasoningAfterFunctionCallAttachesBackToCallOwner(t *testing.T) {
	in := request.Input{Items: []request.Item{
		item(t, `{"type":"function_call","call_id":"c","name":"tool","arguments":"{}"}`),
		item(t, `{"type":"reasoning","summary":[{"type":"summary_text","text":"late"}]}`),
		item(t, `{"type":"function_call_output","call_id":"c","output":"ok"}`),
	}}
	ctx := parse(t, in)
	if len(ctx.Messages) != 2 {
		t.Fatalf("%+v", ctx.Messages)
	}
	a := ctx.Messages[0]
	if len(a.Content) != 2 || a.Content[0].Type != protocol.ContentThinking || a.Content[1].Type != protocol.ContentToolCall {
		t.Fatalf("%+v", a.Content)
	}
	r := ctx.Messages[1]
	if r.Role != protocol.RoleToolResult || r.ToolCallID != "c" || r.ToolName != "tool" || (len(r.Content) != 1 || r.Content[0].Text != "ok") {
		t.Fatalf("%+v", r)
	}
}
func TestInstructionsPrecedeSystemMessages(t *testing.T) {
	req := &request.Request{Model: "m", Instructions: json.RawMessage(`"top"`), Input: request.Input{Items: []request.Item{
		item(t, `{"role":"system","content":"item-system"}`),
	}}}
	ctx, err := Build(req, 9)
	if err != nil {
		t.Fatal(err)
	}
	if len(ctx.SystemPrompt) != 2 || ctx.SystemPrompt[0] != "top" || ctx.SystemPrompt[1] != "item-system" {
		t.Fatalf("system=%v", ctx.SystemPrompt)
	}
}
func TestUserBoundaryClearsPendingReasoning(t *testing.T) {
	ctx := parse(t, request.Input{Items: []request.Item{
		item(t, `{"type":"reasoning","summary":[{"type":"summary_text","text":"old"}]}`),
		item(t, `{"role":"user","content":"new question"}`),
		item(t, `{"role":"assistant","content":"answer"}`),
	}})
	if len(ctx.Messages) != 2 || len(ctx.Messages[1].Content) != 1 || ctx.Messages[1].Content[0].Type != protocol.ContentText {
		t.Fatalf("messages=%+v", ctx.Messages)
	}
}
func TestFunctionCallPreservesBoundedProviderMetadata(t *testing.T) {
	ctx := parse(t, request.Input{Items: []request.Item{item(t, `{"type":"function_call","call_id":"c","name":"tool","arguments":"{}","extra_content":{"google":{"thought_signature":"signed"},"drop":"x"}}`)}})
	p := ctx.Messages[0].Content[0]
	if p.ProviderMetadata == nil || p.ProviderMetadata.Google == nil || p.ProviderMetadata.Google.ThoughtSignature != "signed" {
		t.Fatalf("metadata=%+v", p.ProviderMetadata)
	}
}
func TestUntypedRoleItemUsesMessagePath(t *testing.T) {
	ctx := parse(t, request.Input{Items: []request.Item{item(t, `{"role":"user","content":"hello"}`)}})
	if len(ctx.Messages) != 1 || ctx.Messages[0].Role != protocol.RoleUser || ctx.Messages[0].Content[0].Text != "hello" {
		t.Fatalf("%+v", ctx)
	}
}
func TestBuildFromDecodedResponsesRequest(t *testing.T) {
	body := `{"model":"model-x","instructions":"sys","input":[{"type":"reasoning","summary":[{"type":"summary_text","text":"think"}]},{"type":"function_call","call_id":"c","name":"tool","arguments":"{\"x\":1}"},{"type":"function_call_output","call_id":"c","output":"ok"}]}`
	req, err := request.Decode(strings.NewReader(body), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := Build(req, 42)
	if err != nil {
		t.Fatal(err)
	}
	if len(ctx.SystemPrompt) != 1 || ctx.SystemPrompt[0] != "sys" {
		t.Fatalf("system=%v", ctx.SystemPrompt)
	}
	if len(ctx.Messages) != 2 {
		t.Fatalf("messages=%+v", ctx.Messages)
	}
	a := ctx.Messages[0]
	if a.Role != protocol.RoleAssistant || len(a.Content) != 2 || a.Content[0].Type != protocol.ContentThinking || a.Content[0].Thinking != "think" || a.Content[1].Type != protocol.ContentToolCall || a.Content[1].ToolCallID != "c" {
		t.Fatalf("assistant=%+v", a)
	}
	r := ctx.Messages[1]
	if r.Role != protocol.RoleToolResult || r.ToolCallID != "c" || r.ToolName != "tool" || len(r.Content) != 1 || r.Content[0].Text != "ok" {
		t.Fatalf("toolResult=%+v", r)
	}
}
