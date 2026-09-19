package openairesponses

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
)

func boolPtr(v bool) *bool        { return &v }
func floatPtr(v float64) *float64 { return &v }
func stringPtr(v string) *string  { return &v }

func TestCompileUsesCanonicalRequestOnly(t *testing.T) {
	strict := true
	req := protocol.ParsedRequest{
		ModelID:         "client/ignored",
		UpstreamModelID: "gpt-5.6-sol",
		Stream:          true,
		Raw:             json.RawMessage(`{"model":"RAW-MUST-NOT-WIN","input":"raw input","temperature":99,"metadata":{"bad":true},"user":"raw-user"}`),
		Context: protocol.Context{
			SystemPrompt: []string{"be brief", "cite evidence"},
			Messages: []protocol.Message{
				{Role: protocol.RoleUser, Content: []protocol.ContentPart{
					{Type: protocol.ContentText, Text: "look"},
					{Type: protocol.ContentImage, ImageURL: "https://img.example/a.png", Detail: "high"},
				}},
				{Role: protocol.RoleAssistant, Phase: phasePtr(protocol.PhaseCommentary), Content: []protocol.ContentPart{
					{Type: protocol.ContentText, Text: "checking"},
					{Type: protocol.ContentToolCall, ToolCallID: "call_1", ToolName: "lookup", Arguments: map[string]any{"q": "x"}},
				}},
				{Role: protocol.RoleToolResult, ToolCallID: "call_1", ToolName: "lookup", Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "result"}}},
				{Role: protocol.RoleDeveloper, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "developer note"}}},
			},
			Tools: []protocol.Tool{{Name: "lookup", Description: "look up", Parameters: map[string]any{"type": "object"}, Strict: &strict}},
		},
		Options: protocol.RequestOptions{
			MaxOutputTokens: floatPtr(64), Temperature: floatPtr(0.2), TopP: floatPtr(0.9),
			StopSequences: []string{"END"}, ToolChoice: &protocol.ToolChoice{Kind: protocol.ToolChoiceNamed, Name: "lookup"},
			ParallelToolCalls: boolPtr(false), Reasoning: "high", HideThinkingSummary: false,
			ServiceTier: stringPtr("flex"), PresencePenalty: floatPtr(0.1), FrequencyPenalty: floatPtr(0.2),
			PromptCacheKey: stringPtr("cache-1"), User: stringPtr("u-1"), Metadata: map[string]any{"trace": "abc"}, Store: boolPtr(false),
			TextFormat: &protocol.TextFormat{Type: "json_schema", Name: "answer", Description: "schema", Schema: map[string]any{"type": "object"}, Strict: &strict},
		},
		StructuredOutput: true,
	}
	encoded, err := Compile(req)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(encoded, &body); err != nil {
		t.Fatal(err)
	}
	if body["model"] != "gpt-5.6-sol" || body["stream"] != true || body["store"] != false || body["parallel_tool_calls"] != false {
		t.Fatalf("core=%#v", body)
	}
	if body["instructions"] != "be brief\n\ncite evidence" {
		t.Fatalf("instructions=%#v", body["instructions"])
	}
	if body["temperature"] != 0.2 || body["user"] != "u-1" {
		t.Fatalf("options=%#v", body)
	}
	if !reflect.DeepEqual(body["metadata"], map[string]any{"trace": "abc"}) {
		t.Fatalf("metadata=%#v", body["metadata"])
	}
	input, ok := body["input"].([]any)
	if !ok || len(input) != 5 {
		t.Fatalf("input=%#v", body["input"])
	}
	user := input[0].(map[string]any)
	if user["type"] != "message" || user["role"] != "user" {
		t.Fatalf("user=%#v", user)
	}
	assistant := input[1].(map[string]any)
	if assistant["phase"] != "commentary" {
		t.Fatalf("assistant=%#v", assistant)
	}
	call := input[2].(map[string]any)
	if call["type"] != "function_call" || call["call_id"] != "call_1" || call["name"] != "lookup" || call["arguments"] != `{"q":"x"}` {
		t.Fatalf("call=%#v", call)
	}
	result := input[3].(map[string]any)
	if result["type"] != "function_call_output" || result["call_id"] != "call_1" || result["output"] != "result" {
		t.Fatalf("result=%#v", result)
	}
	developer := input[4].(map[string]any)
	if developer["role"] != "developer" {
		t.Fatalf("developer=%#v", developer)
	}
	tools := body["tools"].([]any)
	if tools[0].(map[string]any)["name"] != "lookup" {
		t.Fatalf("tools=%#v", tools)
	}
	choice := body["tool_choice"].(map[string]any)
	if choice["type"] != "function" || choice["name"] != "lookup" {
		t.Fatalf("choice=%#v", choice)
	}
	reasoning := body["reasoning"].(map[string]any)
	if reasoning["effort"] != "high" || reasoning["summary"] != "auto" {
		t.Fatalf("reasoning=%#v", reasoning)
	}
	text := body["text"].(map[string]any)["format"].(map[string]any)
	if text["type"] != "json_schema" || text["name"] != "answer" {
		t.Fatalf("text=%#v", text)
	}
}

func TestCompileReasoningVisibilityAndUltraNormalization(t *testing.T) {
	cases := []struct {
		name string
		req  protocol.RequestOptions
		want map[string]any
	}{
		{"hidden effort", protocol.RequestOptions{Reasoning: "ultra", HideThinkingSummary: true}, map[string]any{"effort": "max", "summary": "none"}},
		{"summary only", protocol.RequestOptions{HideThinkingSummary: false}, map[string]any{"summary": "auto"}},
		{"no reasoning", protocol.RequestOptions{HideThinkingSummary: true}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			encoded, err := Compile(protocol.ParsedRequest{UpstreamModelID: "gpt", Options: tc.req, Context: protocol.Context{Messages: []protocol.Message{{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}}}}}})
			if err != nil {
				t.Fatal(err)
			}
			var body map[string]any
			_ = json.Unmarshal(encoded, &body)
			if tc.want == nil {
				if _, ok := body["reasoning"]; ok {
					t.Fatalf("reasoning=%#v", body["reasoning"])
				}
				return
			}
			if !reflect.DeepEqual(body["reasoning"], tc.want) {
				t.Fatalf("reasoning=%#v", body["reasoning"])
			}
		})
	}
}

func TestCompileToolOutputWithImagesPreservesStructure(t *testing.T) {
	req := protocol.ParsedRequest{UpstreamModelID: "gpt", Context: protocol.Context{Messages: []protocol.Message{
		{Role: protocol.RoleToolResult, ToolCallID: "call_1", Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "caption"}, {Type: protocol.ContentImage, ImageURL: "data:image/png;base64,abc", Detail: "original"}}},
	}}}
	encoded, err := Compile(req)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	_ = json.Unmarshal(encoded, &body)
	item := body["input"].([]any)[0].(map[string]any)
	output := item["output"].([]any)
	if output[0].(map[string]any)["type"] != "input_text" || output[1].(map[string]any)["type"] != "input_image" {
		t.Fatalf("output=%#v", output)
	}
}

func TestCompileAllowedTools(t *testing.T) {
	req := protocol.ParsedRequest{UpstreamModelID: "gpt", Context: protocol.Context{Tools: []protocol.Tool{{Name: "a", Parameters: map[string]any{"type": "object"}}, {Name: "b", Parameters: map[string]any{"type": "object"}}}}, Options: protocol.RequestOptions{ToolChoice: &protocol.ToolChoice{Kind: protocol.ToolChoiceAllowed, AllowedTools: []string{"b"}, AllowedMode: protocol.ToolChoiceModeRequired}}}
	encoded, err := Compile(req)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	_ = json.Unmarshal(encoded, &body)
	choice := body["tool_choice"].(map[string]any)
	if choice["type"] != "allowed_tools" || choice["mode"] != "required" {
		t.Fatalf("choice=%#v", choice)
	}
	tools := choice["tools"].([]any)
	if len(tools) != 1 || tools[0].(map[string]any)["name"] != "b" {
		t.Fatalf("allowed=%#v", tools)
	}
}

func TestCompileRejectsCanonicalShapesOutsideMigratedNativeSubset(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*protocol.ParsedRequest)
	}{
		{"missing model", func(req *protocol.ParsedRequest) { req.UpstreamModelID = "" }},
		{"parallel calls", func(req *protocol.ParsedRequest) { req.Options.ParallelToolCalls = boolPtr(true) }},
		{"thinking replay", func(req *protocol.ParsedRequest) {
			req.Context.Messages = []protocol.Message{{Role: protocol.RoleAssistant, Content: []protocol.ContentPart{{Type: protocol.ContentThinking, Thinking: "secret"}}}}
		}},
		{"kiro replay", func(req *protocol.ParsedRequest) {
			req.Context.Messages = []protocol.Message{{Role: protocol.RoleAssistant, KiroReasoning: protocol.KiroReasoningState{Member: protocol.KiroReasoningRedactedContent, Value: "blob"}}}
		}},
		{"undeclared call", func(req *protocol.ParsedRequest) {
			req.Context.Messages = []protocol.Message{{Role: protocol.RoleAssistant, Content: []protocol.ContentPart{{Type: protocol.ContentToolCall, ToolCallID: "c", ToolName: "x", CustomWireName: "x"}}}}
		}},
		{"tool collision", func(req *protocol.ParsedRequest) {
			req.Context.Tools = []protocol.Tool{{Name: "read", Namespace: "mcp"}, {Name: "mcp__read"}}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := protocol.ParsedRequest{UpstreamModelID: "gpt"}
			tc.mutate(&req)
			_, err := Compile(req)
			if !errors.Is(err, ErrUnsupportedRequestShape) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestCompileLowersNamespacedAndToolSearchIdentities(t *testing.T) {
	encoded, err := Compile(protocol.ParsedRequest{
		UpstreamModelID: "gpt",
		Context: protocol.Context{
			Tools: []protocol.Tool{
				{Name: "lookup", Namespace: "mcp", Parameters: map[string]any{"type": "object"}},
				{Name: "tool_search", ToolSearch: true, Parameters: map[string]any{"type": "object"}},
				{Name: "exec", Freeform: true, Parameters: map[string]any{"type": "object", "properties": map[string]any{"input": map[string]any{"type": "string"}}}},
			},
			Messages: []protocol.Message{{
				Role: protocol.RoleAssistant,
				Content: []protocol.ContentPart{{
					Type: protocol.ContentToolCall, ToolCallID: "c1", ToolName: "lookup", ToolNamespace: "mcp",
					Arguments: map[string]any{"q": "x"},
				}},
			}},
		},
		Options: protocol.RequestOptions{ToolChoice: &protocol.ToolChoice{Kind: protocol.ToolChoiceNamed, Name: "mcp.lookup"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(encoded, &body); err != nil {
		t.Fatal(err)
	}
	tools, ok := body["tools"].([]any)
	if !ok || len(tools) != 3 {
		t.Fatalf("tools=%#v", body["tools"])
	}
	names := []string{
		tools[0].(map[string]any)["name"].(string),
		tools[1].(map[string]any)["name"].(string),
		tools[2].(map[string]any)["name"].(string),
	}
	if names[0] != "mcp__lookup" || names[1] != "tool_search" || names[2] != "exec" {
		t.Fatalf("names=%v", names)
	}
	choice := body["tool_choice"].(map[string]any)
	if choice["name"] != "mcp__lookup" {
		t.Fatalf("choice=%#v", choice)
	}
	call := body["input"].([]any)[0].(map[string]any)
	if call["name"] != "mcp__lookup" {
		t.Fatalf("call=%#v", call)
	}
}

func TestCompilePreservesPreviousResponseID(t *testing.T) {
	encoded, err := Compile(protocol.ParsedRequest{
		UpstreamModelID:    "gpt",
		PreviousResponseID: "resp_1",
		Context:            protocol.Context{Messages: []protocol.Message{{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(encoded, &body); err != nil {
		t.Fatal(err)
	}
	if body["previous_response_id"] != "resp_1" {
		t.Fatalf("previous_response_id=%#v", body["previous_response_id"])
	}
}

func phasePtr(v protocol.MessagePhase) *protocol.MessagePhase { return &v }

func TestCompileRejectsToolResultWithoutCallID(t *testing.T) {
	req := protocol.ParsedRequest{UpstreamModelID: "gpt", Context: protocol.Context{Messages: []protocol.Message{{Role: protocol.RoleToolResult, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "x"}}}}}}
	if _, err := Compile(req); !errors.Is(err, ErrUnsupportedRequestShape) {
		t.Fatalf("err=%v", err)
	}
}

func TestCompileRejectsNamedChoiceForMissingTool(t *testing.T) {
	req := protocol.ParsedRequest{UpstreamModelID: "gpt", Context: protocol.Context{Tools: []protocol.Tool{{Name: "a", Parameters: map[string]any{}}}}, Options: protocol.RequestOptions{ToolChoice: &protocol.ToolChoice{Kind: protocol.ToolChoiceNamed, Name: "missing"}}}
	if _, err := Compile(req); !errors.Is(err, ErrUnsupportedRequestShape) {
		t.Fatalf("err=%v", err)
	}
}

func TestCompileNormalizesFunctionParametersToObjectSchema(t *testing.T) {
	req := protocol.ParsedRequest{UpstreamModelID: "gpt", Context: protocol.Context{Tools: []protocol.Tool{{Name: "a", Parameters: map[string]any{"properties": map[string]any{"q": map[string]any{"type": "string"}}}}}}}
	encoded, err := Compile(req)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	_ = json.Unmarshal(encoded, &body)
	params := body["tools"].([]any)[0].(map[string]any)["parameters"].(map[string]any)
	if params["type"] != "object" {
		t.Fatalf("parameters=%#v", params)
	}
}
