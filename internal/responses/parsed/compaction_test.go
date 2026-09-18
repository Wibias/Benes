package parsed

import (
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
)

func TestBuildFlagsCompactionTriggerWithoutInstallingItAsAMessage(t *testing.T) {
	req := decode(t, `{"model":"openai-apikey/gpt-5.5","input":[{"role":"user","content":"keep"},{"type":"compaction_trigger"}]}`)
	got, err := Build(req, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !got.CompactionRequest {
		t.Fatal("expected compaction request")
	}
	if len(got.Context.Messages) != 1 || got.Context.Messages[0].Content[0].Text != "keep" {
		t.Fatalf("messages=%#v", got.Context.Messages)
	}
}

func TestBuildDoesNotFlagCompactionSummaryAsARequest(t *testing.T) {
	req := decode(t, `{"model":"openai-apikey/gpt-5.5","input":[{"type":"compaction_summary","encrypted_content":"benes1:eA=="}]}`)
	got, err := Build(req, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.CompactionRequest {
		t.Fatal("summary item is not a compaction request")
	}
	if got.Context.Messages[0].Role != protocol.RoleUser {
		t.Fatalf("messages=%#v", got.Context.Messages)
	}
}
