package antigravity

import (
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
)

func TestCompileEnvelopeUsesSameSnapshotProjectAndClaudeContract(t *testing.T) {
	env, err := CompileDispatchEnvelope(protocol.ParsedRequest{
		UpstreamModelID: "claude-sonnet-4-6",
		Context: protocol.Context{
			SystemPrompt: []string{"be brief"},
			Messages: []protocol.Message{
				{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}}},
			},
			Tools: []protocol.Tool{{Name: "lookup", Description: "look"}},
		},
	}, Account{ID: "acct", Token: "tok", ProjectID: "proj-1"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if env.Project != "proj-1" || env.Model != "claude-sonnet-4-6" || env.UserAgent != EnvelopeUserAgent {
		t.Fatalf("envelope=%#v", env)
	}
	if env.Request["preamble"] != true || env.Request["beta"] != InterleavedThinkingBeta {
		t.Fatalf("claude contract=%#v", env.Request)
	}
	sys, _ := env.Request["systemInstruction"].(map[string]any)
	if sys == nil {
		t.Fatal("system instruction must live in one replacement slot")
	}
	toolCfg, _ := env.Request["toolConfig"].(map[string]any)
	fcc, _ := toolCfg["functionCallingConfig"].(map[string]any)
	if fcc["mode"] != "VALIDATED" {
		t.Fatalf("toolConfig=%#v", toolCfg)
	}
}

func TestCompileEnvelopeAddsContinueForEmptyHistoryOnly(t *testing.T) {
	empty, err := CompileDispatchEnvelope(protocol.ParsedRequest{
		UpstreamModelID: "claude-sonnet-4-6",
	}, Account{ProjectID: "p"}, false)
	if err != nil {
		t.Fatal(err)
	}
	contents, _ := empty.Request["contents"].([]map[string]any)
	if len(contents) != 1 {
		t.Fatalf("empty contents=%#v", contents)
	}
	parts, _ := contents[0]["parts"].([]map[string]any)
	if parts[0]["text"] != "continue" {
		t.Fatalf("continue=%#v", parts)
	}

	normal, err := CompileDispatchEnvelope(protocol.ParsedRequest{
		UpstreamModelID: "gemini-3.7-flash",
		Context: protocol.Context{Messages: []protocol.Message{
			{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "thanks"}}},
		}},
	}, Account{ProjectID: "p"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := normal.Request["preamble"]; ok {
		t.Fatal("gemini must not use Claude replacement preamble")
	}
	ncontents, _ := normal.Request["contents"].([]map[string]any)
	nparts, _ := ncontents[0]["parts"].([]map[string]any)
	if nparts[0]["text"] != "thanks" {
		t.Fatalf("user tail corrupted: %#v", ncontents)
	}
}

func TestCompileEnvelopeImageModalitiesAndMissingProject(t *testing.T) {
	env, err := CompileDispatchEnvelope(protocol.ParsedRequest{UpstreamModelID: "gemini-3.7-flash"}, Account{ProjectID: "p"}, true)
	if err != nil {
		t.Fatal(err)
	}
	gc, _ := env.Request["generationConfig"].(map[string]any)
	raw, _ := gc["responseModalities"].([]string)
	if strings.Join(raw, ",") != "TEXT,IMAGE" {
		t.Fatalf("modalities=%#v", gc)
	}
	if _, err := CompileDispatchEnvelope(protocol.ParsedRequest{}, Account{}, false); err == nil {
		t.Fatal("missing project must fail closed")
	}
}

func TestCompileEnvelopeRejectsUnprovenStructuredOutput(t *testing.T) {
	_, err := CompileDispatchEnvelope(protocol.ParsedRequest{
		UpstreamModelID: "gemini-3.7-flash",
		Context: protocol.Context{Messages: []protocol.Message{{
			Role: protocol.RoleUser,
			Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "answer"}},
		}}},
		Options:          protocol.RequestOptions{TextFormat: &protocol.TextFormat{Type: "json_object"}},
		StructuredOutput: true,
	}, Account{ID: "acct", Token: "tok", ProjectID: "proj-1"}, false)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "structured output") {
		t.Fatalf("err=%v want structured-output refusal", err)
	}
}

