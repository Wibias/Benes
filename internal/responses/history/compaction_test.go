package history

import (
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/compaction"
	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/responses/request"
)

func TestCompactionTriggerIsDroppedFromMessages(t *testing.T) {
	ctx := parse(t, request.Input{Items: []request.Item{
		item(t, `{"role":"user","content":"keep"}`),
		item(t, `{"type":"compaction_trigger"}`),
	}})
	if len(ctx.Messages) != 1 || ctx.Messages[0].Content[0].Text != "keep" {
		t.Fatalf("%+v", ctx.Messages)
	}
}

func TestBenes1CompactionItemDecodesIntoSummaryUserMessage(t *testing.T) {
	ctx := parse(t, request.Input{Items: []request.Item{
		item(t, `{"type":"compaction","encrypted_content":`+j(t, compaction.EncodeSummary("progress"))+`}`),
	}})
	if len(ctx.Messages) != 1 || ctx.Messages[0].Role != protocol.RoleUser {
		t.Fatalf("%+v", ctx.Messages)
	}
	if got := ctx.Messages[0].Content[0].Text; got != compaction.SummaryPrefix+"\n\nprogress" {
		t.Fatalf("text=%q", got)
	}
}

func TestOpaqueCompactionItemDegradesToNote(t *testing.T) {
	ctx := parse(t, request.Input{Items: []request.Item{
		item(t, `{"type":"compaction","encrypted_content":"native-blob"}`),
	}})
	if ctx.Messages[0].Content[0].Text != compaction.OpaqueNote {
		t.Fatalf("%+v", ctx.Messages)
	}
}

func TestCompactionSummaryAliasDecodesLikeCompaction(t *testing.T) {
	ctx := parse(t, request.Input{Items: []request.Item{
		item(t, `{"type":"compaction_summary","encrypted_content":`+j(t, compaction.EncodeSummary("x"))+`}`),
	}})
	if !strings.Contains(ctx.Messages[0].Content[0].Text, "x") {
		t.Fatalf("%+v", ctx.Messages)
	}
}

func TestContextCompactionWithoutEncryptedContentIsDropped(t *testing.T) {
	ctx := parse(t, request.Input{Items: []request.Item{
		item(t, `{"role":"user","content":"keep"}`),
		item(t, `{"type":"context_compaction"}`),
	}})
	if len(ctx.Messages) != 1 || ctx.Messages[0].Content[0].Text != "keep" {
		t.Fatalf("%+v", ctx.Messages)
	}
}
