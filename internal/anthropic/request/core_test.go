package request

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
)

func f64(v *float64) float64 {
	if v == nil {
		return -999
	}
	return *v
}

func TestDecodeFullAnthropicMessagesRequestIntoCanonicalContext(t *testing.T) {
	body := `{
		"model":"gemini/gemini-3-pro",
		"max_tokens":8192,
		"stream":true,
		"system":[{"type":"text","text":"You are Claude Code."},{"type":"text","text":"Prefer terse answers."}],
		"messages":[
			{"role":"user","content":"read the README"},
			{"role":"assistant","content":[
				{"type":"thinking","thinking":"secret replay text","signature":"sig123"},
				{"type":"redacted_thinking","data":"opaque"},
				{"type":"text","text":"Reading it now."},
				{"type":"tool_use","id":"toolu_01","name":"Read","input":{"file_path":"/README.md"}}
			]},
			{"role":"user","content":[
				{"type":"tool_result","tool_use_id":"toolu_01","content":[{"type":"text","text":"# hello"}]},
				{"type":"text","text":"now summarize"},
				{"type":"image","source":{"type":"base64","media_type":"image/png","data":"aWc="}}
			]}
		],
		"tools":[{"name":"Read","description":"Read a file","input_schema":{"type":"object","properties":{"file_path":{"type":"string"}},"required":["file_path"]}}],
		"tool_choice":{"type":"auto","disable_parallel_tool_use":true},
		"thinking":{"type":"enabled","budget_tokens":10000},
		"temperature":0.7,
		"top_p":0.9,
		"top_k":40,
		"stop_sequences":["STOP"],
		"metadata":{"user_id":"user-abc"},
		"future_field":{"must":"remain raw only"}
	}`

	got, err := Decode(strings.NewReader(body), 1<<20, DecodeOptions{NowMillis: 1234})
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != protocol.RequestSourceAnthropicMessages {
		t.Fatalf("source=%q", got.Source)
	}
	if got.ModelID != "gemini/gemini-3-pro" || got.UpstreamModelID != "" || !got.Stream {
		t.Fatalf("request=%#v", got)
	}
	if !strings.Contains(string(got.Raw), `"future_field"`) {
		t.Fatalf("raw=%s", got.Raw)
	}
	if len(got.Context.SystemPrompt) != 2 || got.Context.SystemPrompt[0] != "You are Claude Code." || got.Context.SystemPrompt[1] != "Prefer terse answers." {
		t.Fatalf("system=%#v", got.Context.SystemPrompt)
	}
	if len(got.Context.Messages) != 4 {
		t.Fatalf("messages=%#v", got.Context.Messages)
	}

	user := got.Context.Messages[0]
	if user.Role != protocol.RoleUser || user.Timestamp != 1234 || len(user.Content) != 1 || user.Content[0].Text != "read the README" {
		t.Fatalf("user=%#v", user)
	}
	assistant := got.Context.Messages[1]
	if assistant.Role != protocol.RoleAssistant || len(assistant.Content) != 2 {
		t.Fatalf("assistant=%#v", assistant)
	}
	if assistant.Content[0].Type != protocol.ContentText || assistant.Content[0].Text != "Reading it now." {
		t.Fatalf("assistant text=%#v", assistant.Content[0])
	}
	call := assistant.Content[1]
	if call.Type != protocol.ContentToolCall || call.ToolCallID != "toolu_01" || call.ToolName != "Read" || call.Arguments["file_path"] != "/README.md" {
		t.Fatalf("call=%#v", call)
	}
	for _, part := range assistant.Content {
		if strings.Contains(part.Text, "secret replay") || part.Type == protocol.ContentThinking {
			t.Fatalf("replay thinking leaked: %#v", assistant.Content)
		}
	}
	result := got.Context.Messages[2]
	if result.Role != protocol.RoleToolResult || result.ToolCallID != "toolu_01" || result.ToolName != "Read" || len(result.Content) != 1 || result.Content[0].Text != "# hello" {
		t.Fatalf("result=%#v", result)
	}
	tail := got.Context.Messages[3]
	if tail.Role != protocol.RoleUser || len(tail.Content) != 2 || tail.Content[0].Text != "now summarize" || tail.Content[1].ImageURL != "data:image/png;base64,aWc=" {
		t.Fatalf("tail=%#v", tail)
	}

	if len(got.Context.Tools) != 1 || got.Context.Tools[0].Name != "Read" || got.Context.Tools[0].Description != "Read a file" || got.Context.Tools[0].Parameters["type"] != "object" {
		t.Fatalf("tools=%#v", got.Context.Tools)
	}
	if f64(got.Options.MaxOutputTokens) != 8192 || f64(got.Options.Temperature) != 0.7 || f64(got.Options.TopP) != 0.9 {
		t.Fatalf("options=%#v", got.Options)
	}
	if len(got.Options.StopSequences) != 1 || got.Options.StopSequences[0] != "STOP" {
		t.Fatalf("stop=%#v", got.Options.StopSequences)
	}
	if got.Options.ToolChoice == nil || got.Options.ToolChoice.Kind != protocol.ToolChoiceAuto {
		t.Fatalf("tool choice=%#v", got.Options.ToolChoice)
	}
	if got.Options.ParallelToolCalls == nil || *got.Options.ParallelToolCalls {
		t.Fatalf("parallel=%#v", got.Options.ParallelToolCalls)
	}
	if got.Options.Reasoning != "medium" || got.Options.HideThinkingSummary {
		t.Fatalf("reasoning=%#v", got.Options)
	}
	if got.Options.User == nil || *got.Options.User != "user-abc" {
		t.Fatalf("user=%#v", got.Options.User)
	}
	h := sha256.Sum256([]byte("user-abc"))
	wantCache := hex.EncodeToString(h[:])[:32]
	if got.Options.PromptCacheKey == nil || *got.Options.PromptCacheKey != wantCache {
		t.Fatalf("cache=%#v want=%s", got.Options.PromptCacheKey, wantCache)
	}
}

func TestDecodeSystemMessagesFoldIntoSystemPrompt(t *testing.T) {
	got, err := Decode(strings.NewReader(`{"model":"m","max_tokens":10,"system":"top-level","messages":[{"role":"system","content":"be terse"},{"role":"system","content":[{"type":"text","text":"block form"}]},{"role":"user","content":"hi"}]}`), 1<<20, DecodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Context.SystemPrompt) != 3 || strings.Join(got.Context.SystemPrompt, "|") != "top-level|be terse|block form" {
		t.Fatalf("system=%#v", got.Context.SystemPrompt)
	}
	if len(got.Context.Messages) != 1 || got.Context.Messages[0].Role != protocol.RoleUser {
		t.Fatalf("messages=%#v", got.Context.Messages)
	}
}

func TestDecodeToolResultErrorDocumentsAndImages(t *testing.T) {
	got, err := Decode(strings.NewReader(`{
		"model":"m","max_tokens":10,
		"messages":[
			{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Read","input":{}}]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","is_error":true,"content":[{"type":"text","text":"boom"},{"type":"document","title":"report.pdf"},{"type":"image","source":{"type":"url","url":"https://example.test/image.png"}}]}]}
		]
	}`), 1<<20, DecodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Context.Messages) != 2 {
		t.Fatalf("messages=%#v", got.Context.Messages)
	}
	result := got.Context.Messages[1]
	if !result.IsError || result.ToolCallID != "t1" || result.ToolName != "Read" || len(result.Content) != 3 {
		t.Fatalf("result=%#v", result)
	}
	if result.Content[0].Text != "boom" || result.Content[1].Text != "[document: report.pdf]" || result.Content[2].ImageURL != "https://example.test/image.png" {
		t.Fatalf("content=%#v", result.Content)
	}
}

func TestDecodeDoesNotLeakReplayThinkingWhenAssistantContainsOnlyThinking(t *testing.T) {
	got, err := Decode(strings.NewReader(`{"model":"m","max_tokens":10,"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"secret","signature":"sig"}]},{"role":"user","content":"continue"}]}`), 1<<20, DecodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Context.Messages) != 1 || got.Context.Messages[0].Role != protocol.RoleUser {
		t.Fatalf("messages=%#v", got.Context.Messages)
	}
}
