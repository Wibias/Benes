package kiro

import (
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
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
		{Role: protocol.RoleAssistant, KiroRedactedReasoning: "blob", Content: []protocol.ContentPart{
			{Type: protocol.ContentText, Text: "ok"},
			{Type: protocol.ContentToolCall, ToolCallID: "c1", ToolName: "lookup"},
		}},
		{Role: protocol.RoleToolResult, ToolCallID: "c1", IsError: true, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "boom"}}},
	}}})
	if err != nil || len(got) != 3 {
		t.Fatalf("got=%#v err=%v", got, err)
	}
	if got[1].Redacted != "blob" || len(got[1].ToolUses) != 1 || got[1].ToolUses[0].Name != "lookup" {
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
