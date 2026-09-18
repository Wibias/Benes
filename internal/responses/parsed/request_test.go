package parsed

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	requestwire "github.com/Wibias/Benes/internal/responses/request"
)

func decode(t *testing.T, body string) *requestwire.Request {
	t.Helper()
	req, err := requestwire.Decode(strings.NewReader(body), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func TestBuildBindsContextAndTransportMetadata(t *testing.T) {
	req := decode(t, `{"model":"openai-apikey/gpt-5.6","input":"hello","tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}],"stream":true,"previous_response_id":"resp_1"}`)
	got, err := Build(req, 99)
	if err != nil {
		t.Fatal(err)
	}
	if got.ModelID != "openai-apikey/gpt-5.6" || got.PreviousResponseID != "resp_1" || !got.Stream {
		t.Fatalf("metadata=%+v", got)
	}
	if !bytes.Equal(got.Raw, req.Raw) {
		t.Fatalf("raw changed: got=%s want=%s", got.Raw, req.Raw)
	}
	if len(got.Context.Messages) != 1 || got.Context.Messages[0].Role != protocol.RoleUser || got.Context.Messages[0].Content[0].Text != "hello" {
		t.Fatalf("messages=%+v", got.Context.Messages)
	}
	if len(got.Context.Tools) != 1 || got.Context.Tools[0].Name != "lookup" {
		t.Fatalf("tools=%+v", got.Context.Tools)
	}
}

func TestBuildNormalizesScalarOptionsStopsAndReasoning(t *testing.T) {
	req := decode(t, `{"model":"p/m","max_output_tokens":321,"temperature":0,"top_p":0.75,"stop":["END","STOP"],"parallel_tool_calls":false,"reasoning":{"effort":"ultra","summary":"none"},"service_tier":"priority","presence_penalty":0,"frequency_penalty":-0.5,"prompt_cache_key":"cache"}`)
	got, err := Build(req, 1)
	if err != nil {
		t.Fatal(err)
	}
	o := got.Options
	if o.MaxOutputTokens == nil || *o.MaxOutputTokens != 321 || o.Temperature == nil || *o.Temperature != 0 || o.TopP == nil || *o.TopP != 0.75 {
		t.Fatalf("numeric options=%+v", o)
	}
	if len(o.StopSequences) != 2 || o.StopSequences[0] != "END" || o.StopSequences[1] != "STOP" {
		t.Fatalf("stop=%v", o.StopSequences)
	}
	if o.ParallelToolCalls == nil || *o.ParallelToolCalls {
		t.Fatalf("parallel=%v", o.ParallelToolCalls)
	}
	if o.Reasoning != "max" || !o.HideThinkingSummary {
		t.Fatalf("reasoning=%q hide=%v", o.Reasoning, o.HideThinkingSummary)
	}
	if o.ServiceTier == nil || *o.ServiceTier != "priority" || o.PresencePenalty == nil || *o.PresencePenalty != 0 || o.FrequencyPenalty == nil || *o.FrequencyPenalty != -0.5 || o.PromptCacheKey == nil || *o.PromptCacheKey != "cache" {
		t.Fatalf("passthrough options=%+v", o)
	}
}

func TestBuildReasoningSummaryAndStopAbsenceSemantics(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		wantStopNil bool
		wantStop string
		wantReasoning string
		wantHide bool
	}{
		{name: "null stop and absent summary", body: `{"model":"p/m","stop":null,"reasoning":{"effort":"garbage"}}`, wantStopNil: true, wantHide: true},
		{name: "string stop and detailed summary", body: `{"model":"p/m","stop":"END","reasoning":{"effort":"xhigh","summary":"detailed"}}`, wantStop: "END", wantReasoning: "xhigh", wantHide: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Build(decode(t, tc.body), 1)
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantStopNil {
				if got.Options.StopSequences != nil {
					t.Fatalf("stop=%v", got.Options.StopSequences)
				}
			} else if len(got.Options.StopSequences) != 1 || got.Options.StopSequences[0] != tc.wantStop {
				t.Fatalf("stop=%v", got.Options.StopSequences)
			}
			if got.Options.Reasoning != tc.wantReasoning || got.Options.HideThinkingSummary != tc.wantHide {
				t.Fatalf("reasoning=%q hide=%v", got.Options.Reasoning, got.Options.HideThinkingSummary)
			}
		})
	}
}

func TestBuildMapsToolChoice(t *testing.T) {
	cases := []struct {
		name string
		choice string
		kind string
		toolName string
		allowedMode string
		allowed []string
	}{
		{name: "auto", choice: `"auto"`, kind: "auto"},
		{name: "none", choice: `"none"`, kind: "none"},
		{name: "required", choice: `"required"`, kind: "required"},
		{name: "function", choice: `{"type":"function","name":"lookup"}`, kind: "named", toolName: "lookup"},
		{name: "custom", choice: `{"type":"custom","name":"apply_patch"}`, kind: "named", toolName: "apply_patch"},
		{name: "image hosted", choice: `{"type":"image_generation"}`, kind: "named", toolName: "image_gen"},
		{name: "web search hosted", choice: `{"type":"web_search"}`, kind: "named", toolName: "web_search"},
		{name: "web search preview hosted", choice: `{"type":"web_search_preview"}`, kind: "named", toolName: "web_search_preview"},
		{name: "other hosted degrades auto", choice: `{"type":"computer_use_preview"}`, kind: "auto"},
		{name: "allowed", choice: `{"type":"allowed_tools","mode":"required","tools":[{"type":"function","name":"lookup"},{"type":"web_search"},{"type":"image_generation"},{"type":"tool_search"},{"type":"function","name":"lookup"},{"type":"computer_use_preview"}]}`, kind: "allowed", allowedMode: "required", allowed: []string{"lookup", "web_search", "image_gen", "tool_search"}},
		{name: "empty allowed becomes none", choice: `{"type":"allowed_tools","mode":"auto","tools":[{"type":"computer_use_preview"}]}`, kind: "none"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Build(decode(t, `{"model":"p/m","tool_choice":`+tc.choice+`}`), 1)
			if err != nil {
				t.Fatal(err)
			}
			choice := got.Options.ToolChoice
			if choice == nil || string(choice.Kind) != tc.kind || choice.Name != tc.toolName || string(choice.AllowedMode) != tc.allowedMode {
				t.Fatalf("choice=%+v", choice)
			}
			if len(choice.AllowedTools) != len(tc.allowed) {
				t.Fatalf("allowed=%v", choice.AllowedTools)
			}
			for i := range tc.allowed {
				if choice.AllowedTools[i] != tc.allowed[i] {
					t.Fatalf("allowed=%v", choice.AllowedTools)
				}
			}
		})
	}
}

func TestBuildNormalizesStructuredOutputFormat(t *testing.T) {
	got, err := Build(decode(t, `{"model":"p/m","text":{"format":{"type":"json_schema","name":"answer","description":"shape","schema":{"type":"object","properties":{"ok":{"type":"boolean"}}},"strict":false,"drop":"x"}}}`), 1)
	if err != nil {
		t.Fatal(err)
	}
	f := got.Options.TextFormat
	if f == nil || f.Type != "json_schema" || f.Name != "answer" || f.Description != "shape" || f.Strict == nil || *f.Strict || f.Schema["type"] != "object" || !got.StructuredOutput {
		t.Fatalf("format=%+v structured=%v", f, got.StructuredOutput)
	}

	jsonObject, err := Build(decode(t, `{"model":"p/m","text":{"format":{"type":"json_object","name":"drop"}}}`), 1)
	if err != nil {
		t.Fatal(err)
	}
	if jsonObject.Options.TextFormat == nil || jsonObject.Options.TextFormat.Type != "json_object" || jsonObject.Options.TextFormat.Name != "" || !jsonObject.StructuredOutput {
		t.Fatalf("format=%+v structured=%v", jsonObject.Options.TextFormat, jsonObject.StructuredOutput)
	}

	ignored, err := Build(decode(t, `{"model":"p/m","text":{"format":{"type":"xml","schema":{"type":"object"}}}}`), 1)
	if err != nil {
		t.Fatal(err)
	}
	if ignored.Options.TextFormat != nil || ignored.StructuredOutput {
		t.Fatalf("format=%+v structured=%v", ignored.Options.TextFormat, ignored.StructuredOutput)
	}
}

func TestBuildRejectsNilRequest(t *testing.T) {
	if _, err := Build(nil, 1); err == nil {
		t.Fatal("expected error")
	}
}
