package request

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
)

func TestDecodeMapsMessagesToolsOptionsAndMetadata(t *testing.T) {
	body := `{
  "model":"mock/test-model","stream":true,
  "messages":[
   {"role":"system","content":"be brief"},
   {"role":"developer","content":[{"type":"text","text":"use evidence"}]},
   {"role":"user","content":[{"type":"text","text":"hi"},{"type":"image_url","image_url":{"url":"https://img.example/a.png","detail":"high"}}]},
   {"role":"assistant","content":"checking","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"q\":\"x\"}"}}]},
   {"role":"tool","tool_call_id":"call_1","content":[{"type":"text","text":"result"}]}
  ],
  "tools":[{"type":"function","function":{"name":"lookup","description":"look up","parameters":{"type":"object","properties":{"q":{"type":"string"}}},"strict":true}}],
  "tool_choice":{"type":"function","function":{"name":"lookup"}},
  "max_completion_tokens":64,"temperature":0.2,"top_p":0.9,"stop":["END"],
  "parallel_tool_calls":false,"prompt_cache_key":"cache-1","service_tier":"flex",
  "presence_penalty":0.1,"frequency_penalty":0.2,
  "reasoning":{"effort":"high","summary":"concise"},
  "response_format":{"type":"json_schema","json_schema":{"name":"answer","description":"schema","schema":{"type":"object"},"strict":true}},
  "user":"u-1","metadata":{"trace":"abc"}
 }`
	got, err := Decode(strings.NewReader(body), 1<<20, DecodeOptions{NowMillis: 1234, IDGenerator: func() string { return "generated" }})
	if err != nil {
		t.Fatal(err)
	}
	if got.ModelID != "mock/test-model" || !got.Stream {
		t.Fatalf("model/stream=%q/%v", got.ModelID, got.Stream)
	}
	if !reflect.DeepEqual(got.Context.SystemPrompt, []string{"be brief", "use evidence"}) {
		t.Fatalf("system=%#v", got.Context.SystemPrompt)
	}
	if len(got.Context.Messages) != 3 {
		t.Fatalf("messages=%#v", got.Context.Messages)
	}
	user := got.Context.Messages[0]
	if user.Role != protocol.RoleUser || len(user.Content) != 2 || user.Content[1].Type != protocol.ContentImage || user.Content[1].ImageURL != "https://img.example/a.png" {
		t.Fatalf("user=%#v", user)
	}
	assistant := got.Context.Messages[1]
	if assistant.Role != protocol.RoleAssistant || len(assistant.Content) != 2 || assistant.Content[1].ToolCallID != "call_1" || assistant.Content[1].ToolName != "lookup" || assistant.Content[1].Arguments["q"] != "x" {
		t.Fatalf("assistant=%#v", assistant)
	}
	toolResult := got.Context.Messages[2]
	if toolResult.Role != protocol.RoleToolResult || toolResult.ToolCallID != "call_1" || toolResult.Content[0].Text != "result" {
		t.Fatalf("tool=%#v", toolResult)
	}
	if len(got.Context.Tools) != 1 || got.Context.Tools[0].Name != "lookup" || got.Context.Tools[0].Strict == nil || !*got.Context.Tools[0].Strict {
		t.Fatalf("tools=%#v", got.Context.Tools)
	}
	if got.Options.ToolChoice == nil || got.Options.ToolChoice.Kind != protocol.ToolChoiceNamed || got.Options.ToolChoice.Name != "lookup" {
		t.Fatalf("choice=%#v", got.Options.ToolChoice)
	}
	if got.Options.MaxOutputTokens == nil || *got.Options.MaxOutputTokens != 64 || got.Options.Reasoning != "high" || got.Options.HideThinkingSummary {
		t.Fatalf("options=%#v", got.Options)
	}
	if got.Options.TextFormat == nil || got.Options.TextFormat.Type != "json_schema" || got.Options.TextFormat.Name != "answer" || !got.StructuredOutput {
		t.Fatalf("format=%#v structured=%v", got.Options.TextFormat, got.StructuredOutput)
	}
	if got.Options.User == nil || *got.Options.User != "u-1" || got.Options.Metadata["trace"] != "abc" {
		t.Fatalf("user/metadata=%#v/%#v", got.Options.User, got.Options.Metadata)
	}
}

func TestDecodeReasoningVisibilityCompatibility(t *testing.T) {
	tests := []struct {
		name, extra string
		wantEffort  string
		wantHide    bool
	}{
		{"effort defaults visible summary", `,"reasoning_effort":"max"`, "max", false},
		{"include false hides", `,"reasoning_effort":"high","include_reasoning":false`, "high", true},
		{"include true visible", `,"include_reasoning":true`, "", false},
		{"explicit summary wins false", `,"include_reasoning":false,"reasoning":{"effort":"high","summary":"auto"}`, "high", false},
		{"no knobs hides", ``, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := `{"model":"p/m","messages":[{"role":"user","content":"hi"}]` + tt.extra + `}`
			got, err := Decode(strings.NewReader(body), 1<<20, DecodeOptions{NowMillis: 1})
			if err != nil {
				t.Fatal(err)
			}
			if got.Options.Reasoning != tt.wantEffort || got.Options.HideThinkingSummary != tt.wantHide {
				t.Fatalf("options=%#v", got.Options)
			}
		})
	}
}

func TestDecodeSupportsFlatAndNestedFunctionTools(t *testing.T) {
	body := `{"model":"p/m","messages":[{"role":"user","content":"hi"}],"tools":[{"type":"function","name":"flat","parameters":{}},{"type":"function","function":{"name":"nested","parameters":{}}}]}`
	got, err := Decode(strings.NewReader(body), 1<<20, DecodeOptions{NowMillis: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Context.Tools) != 2 || got.Context.Tools[0].Name != "flat" || got.Context.Tools[1].Name != "nested" {
		t.Fatalf("tools=%#v", got.Context.Tools)
	}
}

func TestDecodeMintsMissingToolCallIDAndRejectsMissingName(t *testing.T) {
	body := `{"model":"p/m","messages":[{"role":"assistant","tool_calls":[{"type":"function","function":{"name":"lookup","arguments":"{}"}}]}]}`
	got, err := Decode(strings.NewReader(body), 1<<20, DecodeOptions{NowMillis: 1, IDGenerator: func() string { return "abc123" }})
	if err != nil {
		t.Fatal(err)
	}
	if got.Context.Messages[0].Content[0].ToolCallID != "call_abc123" {
		t.Fatalf("call=%#v", got.Context.Messages[0])
	}
	bad := `{"model":"p/m","messages":[{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"arguments":"{}"}}]}]}`
	if _, err := Decode(strings.NewReader(bad), 1<<20, DecodeOptions{NowMillis: 1}); err == nil {
		t.Fatal("missing tool name accepted")
	}
}

func TestDecodeInvalidToolArgumentsMatchResponsesHistoryFallback(t *testing.T) {
	body := `{"model":"p/m","messages":[{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"x","arguments":"not-json"}}]}]}`
	got, err := Decode(strings.NewReader(body), 1<<20, DecodeOptions{NowMillis: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Context.Messages[0].Content[0].Arguments) != 0 {
		t.Fatalf("args=%#v", got.Context.Messages[0].Content[0].Arguments)
	}
}

func TestDecodeRecordsHostedWebSearchTools(t *testing.T) {
	body := `{"model":"p/m","messages":[{"role":"user","content":"hi"}],"tools":[{"type":"web_search_preview"}]}`
	got, err := Decode(strings.NewReader(body), 1<<20, DecodeOptions{NowMillis: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Context.Tools) != 1 || !got.Context.Tools[0].HostedWebSearch || got.Context.Tools[0].Name != "web_search_preview" {
		t.Fatalf("tools=%#v", got.Context.Tools)
	}
}

func TestDecodeRejectsMalformedRoutingBodyAndToolMessages(t *testing.T) {
	for _, body := range []string{
		`[]`,
		`{"messages":[{"role":"user","content":"x"}]}`,
		`{"model":"p/m","messages":[]}`,
		`{"model":"p/m","messages":[{"role":"tool","content":"x"}]}`,
	} {
		if _, err := Decode(strings.NewReader(body), 1<<20, DecodeOptions{NowMillis: 1}); err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
}

func TestDecodeIsBoundedAndRejectsTrailingJSON(t *testing.T) {
	_, err := Decode(strings.NewReader(`{"model":"p/m","messages":[{"role":"user","content":"`+strings.Repeat("x", 1024)+`"}]}`), 128, DecodeOptions{})
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err=%v", err)
	}
	if _, err := Decode(strings.NewReader(`{"model":"p/m","messages":[{"role":"user","content":"x"}]} {}`), 1<<20, DecodeOptions{}); err == nil {
		t.Fatal("trailing JSON accepted")
	}
}
