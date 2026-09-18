package antigravity

import (
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
)

func TestRepairClaudeTurnsDropsOrphansAndKeepsNormalTails(t *testing.T) {
	turns := RepairClaudeTurns([]protocol.Message{
		{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}}},
		{Role: protocol.RoleAssistant, Content: []protocol.ContentPart{{Type: protocol.ContentToolCall, ToolCallID: "c1", ToolName: "lookup"}}},
		{Role: protocol.RoleToolResult, ToolCallID: "missing", Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "nope"}}},
		{Role: protocol.RoleAssistant, Content: []protocol.ContentPart{
			{Type: protocol.ContentToolCall, ToolCallID: "c2", ToolName: "lookup"},
		}},
		{Role: protocol.RoleToolResult, ToolCallID: "c2", Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "ok"}}},
		{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "thanks"}}},
	})
	if len(turns) != 3 || turns[0].Text != "hi" || !turns[1].HasCall || !turns[1].HasOut || turns[2].Text != "thanks" {
		t.Fatalf("turns=%#v", turns)
	}
	if ContinuationShape(turns) != "" {
		t.Fatal("normal user tail must not become continue")
	}
	if ContinuationShape(nil) != "continue" {
		t.Fatal("empty history")
	}
	if ContinuationShape([]ClaudeTurn{{Role: "assistant"}}) != "continue" {
		t.Fatal("model tail")
	}
}

func TestReplacementPreambleIsClaudeOnly(t *testing.T) {
	if !UseReplacementPreamble(true) || UseReplacementPreamble(false) {
		t.Fatal("preamble mode")
	}
}
