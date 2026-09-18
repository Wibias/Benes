package cursor

import (
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
)

func TestCompileRunNormalizesToolResultsAndBindsPrefix(t *testing.T) {
	got, err := CompileRun(protocol.ParsedRequest{
		UpstreamModelID: "gpt-5.4",
		Context: protocol.Context{
			SystemPrompt: []string{"sys"},
			Messages: []protocol.Message{
				{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}}},
				{Role: protocol.RoleToolResult, IsError: true},
			},
		},
	})
	if err != nil || got.Model != "gpt-5.4" || len(got.Turns) != 2 || !got.Turns[1].IsError || got.Turns[1].Text != "Tool failed." {
		t.Fatalf("run=%#v err=%v", got, err)
	}
	if got.Digest != PrefixDigest([]string{"sys"}, []string{"hi", "Tool failed."}) {
		t.Fatalf("digest=%q", got.Digest)
	}
	if got.ModelDetails["modelName"] != "gpt-5.4" {
		t.Fatalf("modelDetails=%#v", got.ModelDetails)
	}
}
