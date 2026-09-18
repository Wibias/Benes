package request

import (
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
)

func TestDecodeThinkingVariantsAndOutputEffortPrecedence(t *testing.T) {
	base := `"model":"m","max_tokens":10,"messages":[{"role":"user","content":"hi"}]`
	cases := []struct {
		name, extra, effort string
		hide                bool
	}{
		{"omitted", "", "", true},
		{"disabled", `,"thinking":{"type":"disabled"}`, "none", true},
		{"enabled-low", `,"thinking":{"type":"enabled","budget_tokens":1024}`, "low", false},
		{"enabled-medium", `,"thinking":{"type":"enabled","budget_tokens":8192}`, "medium", false},
		{"enabled-high", `,"thinking":{"type":"enabled","budget_tokens":30000}`, "high", false},
		{"adaptive", `,"thinking":{"type":"adaptive"}`, "", false},
		{"adaptive-effort", `,"thinking":{"type":"adaptive"},"output_config":{"effort":"xhigh"}`, "xhigh", false},
		{"effort-only", `,"output_config":{"effort":"medium"}`, "medium", false},
		{"effort-wins-budget", `,"thinking":{"type":"enabled","budget_tokens":1024},"output_config":{"effort":"max"}`, "max", false},
		{"disabled-wins", `,"thinking":{"type":"disabled"},"output_config":{"effort":"high"}`, "none", true},
		{"unknown-effort", `,"thinking":{"type":"adaptive"},"output_config":{"effort":"turbo"}`, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Decode(strings.NewReader(`{`+base+tc.extra+`}`), 1<<20, DecodeOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if got.Options.Reasoning != tc.effort || got.Options.HideThinkingSummary != tc.hide {
				t.Fatalf("options=%#v", got.Options)
			}
		})
	}
}

func TestDecodeToolChoices(t *testing.T) {
	cases := []struct {
		raw      string
		kind     protocol.ToolChoiceKind
		name     string
		parallel *bool
	}{
		{`{"type":"auto","disable_parallel_tool_use":true}`, protocol.ToolChoiceAuto, "", boolPtr(false)},
		{`{"type":"auto","disable_parallel_tool_use":false}`, protocol.ToolChoiceAuto, "", boolPtr(true)},
		{`{"type":"any"}`, protocol.ToolChoiceRequired, "", nil},
		{`{"type":"none"}`, protocol.ToolChoiceNone, "", nil},
		{`{"type":"tool","name":"Read"}`, protocol.ToolChoiceNamed, "Read", nil},
	}
	for _, tc := range cases {
		got, err := Decode(strings.NewReader(`{"model":"m","max_tokens":10,"messages":[{"role":"user","content":"hi"}],"tool_choice":`+tc.raw+`}`), 1<<20, DecodeOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if got.Options.ToolChoice == nil || got.Options.ToolChoice.Kind != tc.kind || got.Options.ToolChoice.Name != tc.name {
			t.Fatalf("choice=%#v", got.Options.ToolChoice)
		}
		if tc.parallel == nil {
			if got.Options.ParallelToolCalls != nil {
				t.Fatalf("parallel=%#v", got.Options.ParallelToolCalls)
			}
		} else if got.Options.ParallelToolCalls == nil || *got.Options.ParallelToolCalls != *tc.parallel {
			t.Fatalf("parallel=%#v", got.Options.ParallelToolCalls)
		}
	}
}

func TestDecodeStructuredOutputOnlyForSupportedSchemaShapes(t *testing.T) {
	valid := `{"model":"m","max_tokens":10,"messages":[{"role":"user","content":"json"}],"output_config":{"format":{"type":"json_schema","schema":{"type":"object","properties":{"answer":{"type":"string"}}}}}}`
	got, err := Decode(strings.NewReader(valid), 1<<20, DecodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !got.StructuredOutput || got.Options.TextFormat == nil || got.Options.TextFormat.Type != "json_schema" || got.Options.TextFormat.Name != "response" {
		t.Fatalf("format=%#v", got.Options.TextFormat)
	}

	ref := `{"model":"m","max_tokens":10,"messages":[{"role":"user","content":"json"}],"output_config":{"format":{"type":"json_schema","schema":{"$defs":{"x":{"type":"object"}},"$ref":"#/$defs/x"}}}}`
	got, err = Decode(strings.NewReader(ref), 1<<20, DecodeOptions{})
	if err != nil || !got.StructuredOutput || got.Options.TextFormat == nil {
		t.Fatalf("got=%#v err=%v", got, err)
	}

	invalid := `{"model":"m","max_tokens":10,"messages":[{"role":"user","content":"json"}],"output_config":{"format":{"type":"json_schema","schema":{"description":"answer"}}}}`
	got, err = Decode(strings.NewReader(invalid), 1<<20, DecodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got.StructuredOutput || got.Options.TextFormat != nil {
		t.Fatalf("invalid schema accepted: %#v", got.Options.TextFormat)
	}
}

func boolPtr(v bool) *bool { return &v }
