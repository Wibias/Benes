package kiro

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	responseshistory "github.com/Wibias/Benes/internal/responses/history"
	"github.com/Wibias/Benes/internal/responses/reasoning"
	responsesrequest "github.com/Wibias/Benes/internal/responses/request"
)

func TestCompileHistoryRequiresAlternationAndToolPairing(t *testing.T) {
	ok, err := CompileHistory(protocol.ParsedRequest{Context: protocol.Context{Messages: []protocol.Message{
		{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}}},
		{Role: protocol.RoleAssistant, Content: []protocol.ContentPart{{Type: protocol.ContentToolCall, ToolCallID: "c1", ToolName: "lookup"}}},
		{Role: protocol.RoleToolResult, ToolCallID: "c1", Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "out"}}},
	}}})
	if err != nil || len(ok) != 3 {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if _, err := CompileHistory(protocol.ParsedRequest{Context: protocol.Context{Messages: []protocol.Message{
		{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "a"}}},
		{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "b"}}},
	}}}); err == nil || !strings.Contains(err.Error(), "alternate") {
		t.Fatalf("alternate=%v", err)
	}
	if _, err := CompileHistory(protocol.ParsedRequest{Context: protocol.Context{Messages: []protocol.Message{
		{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}}},
		{Role: protocol.RoleToolResult, ToolCallID: "missing", Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "x"}}},
	}}}); err == nil || !strings.Contains(err.Error(), "matching tool use") {
		t.Fatalf("orphan=%v", err)
	}
	if _, err := CompileHistory(protocol.ParsedRequest{Context: protocol.Context{Messages: []protocol.Message{
		{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}}},
		{Role: protocol.RoleAssistant, Content: []protocol.ContentPart{{Type: protocol.ContentToolCall, ToolCallID: "c1"}}},
	}}}); err == nil || !strings.Contains(err.Error(), "unanswered") {
		t.Fatalf("unanswered=%v", err)
	}
}

func TestCompileHistoryKeepsToolResultsAndRedactedOnTheWireShape(t *testing.T) {
	got, err := CompileHistory(protocol.ParsedRequest{Context: protocol.Context{Messages: []protocol.Message{
		{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}}},
		{Role: protocol.RoleAssistant, KiroReasoning: protocol.KiroReasoningState{Member: protocol.KiroReasoningRedactedContent, Value: "blob"}, Content: []protocol.ContentPart{
			{Type: protocol.ContentText, Text: "ok"},
			{Type: protocol.ContentToolCall, ToolCallID: "c1", ToolName: "lookup"},
		}},
		{Role: protocol.RoleToolResult, ToolCallID: "c1", IsError: true, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "boom"}}},
	}}})
	if err != nil || len(got) != 3 {
		t.Fatalf("got=%#v err=%v", got, err)
	}
	if got[1].Reasoning.Member != protocol.KiroReasoningRedactedContent || got[1].Reasoning.Value != "blob" || len(got[1].ToolUses) != 1 || got[1].ToolUses[0].Name != "lookup" {
		t.Fatalf("assistant=%#v", got[1])
	}
	if got[2].Text != toolResultCarrier || len(got[2].ToolResults) != 1 || !got[2].ToolResults[0].Error || got[2].ToolResults[0].Text != "boom" {
		t.Fatalf("result=%#v", got[2])
	}
	if _, err := CompileHistory(protocol.ParsedRequest{Context: protocol.Context{Messages: []protocol.Message{
		{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}}},
		{Role: protocol.RoleAssistant, Content: []protocol.ContentPart{{Type: protocol.ContentToolCall, ToolCallID: "c1", ToolName: "lookup"}}},
		{Role: protocol.RoleToolResult, ToolCallID: "c1", ContainsEncryptedContent: true, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "x"}}},
	}}}); err == nil || !strings.Contains(err.Error(), "encrypted") {
		t.Fatalf("encrypted=%v", err)
	}
}

func kiroReplayRequest(t *testing.T, envelopeJSON string) protocol.ParsedRequest {
	t.Helper()
	encrypted := reasoning.Prefix + base64.StdEncoding.EncodeToString([]byte(envelopeJSON))
	rawReasoning, _ := json.Marshal(map[string]any{
		"type": "reasoning",
		"encrypted_content": encrypted,
	})
	req := &responsesrequest.Request{
		Model: "gpt-5.6-luna",
		Input: responsesrequest.Input{Items: []responsesrequest.Item{
			{Type: "message", Role: "user", Raw: json.RawMessage(`{"role":"user","content":"hi"}`)},
			{Type: "message", Role: "assistant", Raw: json.RawMessage(`{"role":"assistant","content":"answer"}`)},
			{Type: "reasoning", Raw: rawReasoning},
			{Type: "message", Role: "user", Raw: json.RawMessage(`{"role":"user","content":"next"}`)},
		}},
	}
	ctx, err := responseshistory.Build(req, 1)
	if err != nil {
		t.Fatal(err)
	}
	return protocol.ParsedRequest{Context: ctx, UpstreamModelID: "gpt-5.6-luna"}
}

func reasoningContentFromReplay(t *testing.T, parsed protocol.ParsedRequest) map[string]any {
	t.Helper()
	entries, err := CompileHistory(parsed)
	if err != nil {
		t.Fatal(err)
	}
	payload := buildGeneratePayload(entries, "", parsed.UpstreamModelID)
	state, ok := payload["conversationState"].(map[string]any)
	if !ok {
		t.Fatalf("state=%#v", payload["conversationState"])
	}
	prior, ok := state["history"].([]map[string]any)
	if !ok || len(prior) < 2 {
		t.Fatalf("history=%#v", state["history"])
	}
	assistant, ok := prior[1]["assistantResponseMessage"].(map[string]any)
	if !ok {
		t.Fatalf("assistant=%#v", prior[1])
	}
	reasoningContent, ok := assistant["reasoningContent"].(map[string]any)
	if !ok {
		t.Fatalf("reasoningContent=%#v", assistant["reasoningContent"])
	}
	return reasoningContent
}

func TestKiroSignatureEnvelopeReplaysSignatureMember(t *testing.T) {
	reasoningContent := reasoningContentFromReplay(t, kiroReplayRequest(t, `{"krc":"sig-1","krk":"signature"}`))
	if reasoningContent["signature"] != "sig-1" {
		t.Fatalf("reasoningContent=%#v", reasoningContent)
	}
	if _, exists := reasoningContent["redactedContent"]; exists {
		t.Fatalf("signature replayed on legacy member: %#v", reasoningContent)
	}
}

func TestLegacyKiroEnvelopeStillReplaysRedactedContent(t *testing.T) {
	reasoningContent := reasoningContentFromReplay(t, kiroReplayRequest(t, `{"krc":"legacy"}`))
	if reasoningContent["redactedContent"] != "legacy" {
		t.Fatalf("reasoningContent=%#v", reasoningContent)
	}
	if _, exists := reasoningContent["signature"]; exists {
		t.Fatalf("legacy envelope changed member: %#v", reasoningContent)
	}
}

