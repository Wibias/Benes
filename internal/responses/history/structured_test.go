package history

import (
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/responses/request"
)

func TestCustomToolCallAndOutputReplay(t *testing.T) {
	ctx := parse(t, request.Input{Items: []request.Item{
		item(t, `{"type":"reasoning","summary":[{"type":"summary_text","text":"think"}]}`),
		item(t, `{"type":"custom_tool_call","call_id":"c","name":"apply_patch","input":"*** Begin Patch"}`),
		item(t, `{"type":"custom_tool_call_output","call_id":"c","output":"done"}`),
	}})
	if len(ctx.Messages) != 2 {
		t.Fatalf("messages=%+v", ctx.Messages)
	}
	a := ctx.Messages[0]
	if len(a.Content) != 2 || a.Content[0].Type != protocol.ContentThinking || a.Content[1].Type != protocol.ContentToolCall {
		t.Fatalf("assistant=%+v", a)
	}
	call := a.Content[1]
	if call.ToolCallID != "c" || call.ToolName != "apply_patch" || call.CustomWireName != "apply_patch" || call.Arguments["input"] != "*** Begin Patch" {
		t.Fatalf("call=%+v", call)
	}
	r := ctx.Messages[1]
	if r.Role != protocol.RoleToolResult || r.ToolCallID != "c" || r.ToolName != "apply_patch" || len(r.Content) != 1 || r.Content[0].Text != "done" {
		t.Fatalf("result=%+v", r)
	}
}
func TestInputImagesAndFilesPreserveSafeReferences(t *testing.T) {
	ctx := parse(t, request.Input{Items: []request.Item{item(t, `{"role":"user","content":[
		{"type":"input_text","text":"look"},
		{"type":"input_image","image_url":"data:image/png;base64,AAAA","detail":"original"},
		{"type":"input_image","file_id":"file-img"},
		{"type":"input_file","file_id":"file-doc","filename":"secret.pdf"},
		{"type":"input_file","filename":"inline.txt","file_data":"VERY_SECRET_BASE64"},
		{"type":"input_file","filename":"bare-name-only.txt"}
	]}`)}})
	parts := ctx.Messages[0].Content
	if len(parts) != 6 {
		t.Fatalf("parts=%+v", parts)
	}
	if parts[0].Type != protocol.ContentText || parts[0].Text != "look" {
		t.Fatalf("text=%+v", parts[0])
	}
	if parts[1].Type != protocol.ContentImage || parts[1].ImageURL != "data:image/png;base64,AAAA" || parts[1].Detail != "high" {
		t.Fatalf("image=%+v", parts[1])
	}
	if parts[2].Type != protocol.ContentImage || parts[2].FileID != "file-img" {
		t.Fatalf("image file ref=%+v", parts[2])
	}
	if parts[3].Type != protocol.ContentFile || parts[3].FileID != "file-doc" || parts[3].Filename != "secret.pdf" {
		t.Fatalf("file ref=%+v", parts[3])
	}
	if parts[4].Type != protocol.ContentFile || parts[4].FileData != "VERY_SECRET_BASE64" || parts[4].Filename != "inline.txt" {
		t.Fatalf("inline file=%+v", parts[4])
	}
	if parts[5].Type != protocol.ContentFile || parts[5].Filename != "bare-name-only.txt" {
		t.Fatalf("filename-only file=%+v", parts[5])
	}
	for _, part := range parts {
		if strings.Contains(part.Text, "VERY_SECRET_BASE64") {
			t.Fatalf("inline file bytes leaked into text: %+v", part)
		}
	}
}
func TestFunctionCallOutputArrayNormalizesTextImageAndEncryptedContent(t *testing.T) {
	ctx := parse(t, request.Input{Items: []request.Item{
		item(t, `{"type":"function_call","call_id":"c","name":"view_image","arguments":"{}"}`),
		item(t, `{"type":"function_call_output","call_id":"c","output":[
			{"type":"output_text","text":"before"},
			{"type":"input_image","image_url":"https://example.test/x.png","detail":"original"},
			{"type":"encrypted_content","encrypted_content":"opaque-secret"},
			{"type":"refusal","refusal":"nope"}
		]}`),
	}})
	r := ctx.Messages[1]
	if !r.ContainsEncryptedContent {
		t.Fatalf("encrypted flag missing: %+v", r)
	}
	if len(r.Content) != 4 {
		t.Fatalf("content=%+v", r.Content)
	}
	if r.Content[0].Text != "before" || r.Content[1].Type != protocol.ContentImage || r.Content[1].ImageURL != "https://example.test/x.png" || r.Content[1].Detail != "high" || r.Content[2].Text != "[encrypted content omitted]" || r.Content[3].Text != "[refusal: nope]" {
		t.Fatalf("content=%+v", r.Content)
	}
}
func TestTextOnlyToolOutputArrayCollapsesToSingleTextPart(t *testing.T) {
	ctx := parse(t, request.Input{Items: []request.Item{
		item(t, `{"type":"function_call","call_id":"c","name":"tool","arguments":"{}"}`),
		item(t, `{"type":"function_call_output","call_id":"c","output":[{"type":"output_text","text":"a"},{"type":"input_text","text":"b"}]}`),
	}})
	r := ctx.Messages[1]
	if len(r.Content) != 1 || r.Content[0].Type != protocol.ContentText || r.Content[0].Text != "ab" {
		t.Fatalf("content=%+v", r.Content)
	}
}
func TestAgentMessageBecomesUserBoundaryAndUsesPlaceholder(t *testing.T) {
	ctx := parse(t, request.Input{Items: []request.Item{
		item(t, `{"type":"reasoning","summary":[{"type":"summary_text","text":"must-drop"}]}`),
		item(t, `{"type":"agent_message","author":"worker","recipient":"parent","content":[]}`),
		item(t, `{"role":"assistant","content":"answer"}`),
	}})
	if len(ctx.Messages) != 2 {
		t.Fatalf("messages=%+v", ctx.Messages)
	}
	if ctx.Messages[0].Role != protocol.RoleUser || len(ctx.Messages[0].Content) != 1 || ctx.Messages[0].Content[0].Text != "(sub-agent message received)" {
		t.Fatalf("agent=%+v", ctx.Messages[0])
	}
	if len(ctx.Messages[1].Content) != 1 || ctx.Messages[1].Content[0].Type != protocol.ContentText {
		t.Fatalf("pending reasoning crossed boundary: %+v", ctx.Messages[1])
	}
}
func TestAgentMessagePreservesImageContent(t *testing.T) {
	ctx := parse(t, request.Input{Items: []request.Item{item(t, `{"type":"agent_message","content":[{"type":"input_text","text":"see"},{"type":"input_image","image_url":"https://example.test/a.png","detail":"low"}]}`)}})
	parts := ctx.Messages[0].Content
	if len(parts) != 2 || parts[1].Type != protocol.ContentImage || parts[1].ImageURL != "https://example.test/a.png" || parts[1].Detail != "low" {
		t.Fatalf("parts=%+v", parts)
	}
}
func TestLocalShellCallPairsWithFunctionOutput(t *testing.T) {
	ctx := parse(t, request.Input{Items: []request.Item{
		item(t, `{"type":"local_shell_call","id":"shell-item","action":{"type":"exec","command":["git","status"]}}`),
		item(t, `{"type":"function_call_output","call_id":"shell-item","output":"clean"}`),
	}})
	if len(ctx.Messages) != 2 {
		t.Fatalf("messages=%+v", ctx.Messages)
	}
	call := ctx.Messages[0].Content[0]
	cmd, ok := call.Arguments["command"].([]any)
	if !ok || len(cmd) != 2 || cmd[0] != "git" || call.ToolName != "shell" || call.ToolCallID != "shell-item" {
		t.Fatalf("call=%+v", call)
	}
	if ctx.Messages[1].ToolName != "shell" || ctx.Messages[1].Content[0].Text != "clean" {
		t.Fatalf("result=%+v", ctx.Messages[1])
	}
}
func TestLocalShellCallWithoutIDIsDropped(t *testing.T) {
	ctx := parse(t, request.Input{Items: []request.Item{item(t, `{"type":"local_shell_call","action":{"command":["pwd"]}}`)}})
	if len(ctx.Messages) != 0 {
		t.Fatalf("messages=%+v", ctx.Messages)
	}
}
func TestTypedMalformedInputContentIsIgnoredLikeLooseZodFallback(t *testing.T) {
	ctx := parse(t, request.Input{Items: []request.Item{item(t, `{"type":"message","role":"user","content":[42,{"type":"input_text","text":7},{"type":"input_image"}]}`)}})
	if len(ctx.Messages) != 1 || len(ctx.Messages[0].Content) != 0 {
		t.Fatalf("messages=%+v", ctx.Messages)
	}
}
func TestToolOutputIgnoresMalformedBlocksAndNeverLeaksEncryptedBytes(t *testing.T) {
	ctx := parse(t, request.Input{Items: []request.Item{
		item(t, `{"type":"function_call","call_id":"c","name":"tool","arguments":"{}"}`),
		item(t, `{"type":"function_call_output","call_id":"c","output":[42,{"type":"output_text","text":7},{"type":"encrypted_content","encrypted_content":"DO_NOT_LEAK"}]}`),
	}})
	r := ctx.Messages[1]
	if !r.ContainsEncryptedContent || len(r.Content) != 1 || r.Content[0].Text != "[encrypted content omitted]" || strings.Contains(r.Content[0].Text, "DO_NOT_LEAK") {
		t.Fatalf("result=%+v", r)
	}
}
func TestLocalShellLooseHistoryDoesNotPoisonReplay(t *testing.T) {
	ctx := parse(t, request.Input{Items: []request.Item{
		item(t, `{"type":"local_shell_call","id":"mixed","action":{"command":["echo",7,true]}}`),
		item(t, `{"type":"local_shell_call","id":"bad-command","action":{"command":"not-an-array"}}`),
	}})
	if len(ctx.Messages) != 1 {
		t.Fatalf("messages=%+v", ctx.Messages)
	}
	if len(ctx.Messages[0].Content) != 2 {
		t.Fatalf("content=%+v", ctx.Messages[0].Content)
	}
	mixed, ok := ctx.Messages[0].Content[0].Arguments["command"].([]any)
	if !ok || len(mixed) != 3 || mixed[0] != "echo" || mixed[1] != float64(7) || mixed[2] != true {
		t.Fatalf("mixed=%#v", ctx.Messages[0].Content[0].Arguments["command"])
	}
	if len(ctx.Messages[0].Content[1].Arguments) != 0 {
		t.Fatalf("bad command should default empty: %+v", ctx.Messages[0].Content[1])
	}
}
func TestBuildStructuredHistoryFromDecodedResponsesRequest(t *testing.T) {
	body := `{"model":"model-x","input":[{"type":"message","role":"user","content":[{"type":"input_image","image_url":"https://example.test/a.png","detail":"original"}]},{"type":"custom_tool_call","call_id":"c","name":"apply_patch","input":"patch"},{"type":"custom_tool_call_output","call_id":"c","output":[{"type":"output_text","text":"ok"},{"type":"encrypted_content","encrypted_content":"secret"}]}]}`
	req, err := request.Decode(strings.NewReader(body), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := Build(req, 42)
	if err != nil {
		t.Fatal(err)
	}
	if len(ctx.Messages) != 3 {
		t.Fatalf("messages=%+v", ctx.Messages)
	}
	if ctx.Messages[0].Content[0].Type != protocol.ContentImage || ctx.Messages[0].Content[0].Detail != "high" {
		t.Fatalf("user=%+v", ctx.Messages[0])
	}
	if ctx.Messages[1].Content[0].CustomWireName != "apply_patch" {
		t.Fatalf("assistant=%+v", ctx.Messages[1])
	}
	r := ctx.Messages[2]
	if !r.ContainsEncryptedContent || len(r.Content) != 1 || r.Content[0].Text != "ok[encrypted content omitted]" {
		t.Fatalf("result=%+v", r)
	}
}
