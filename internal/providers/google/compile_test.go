package google

import (
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
)

func TestCompileRequestBuildsContentsAndSystemInstruction(t *testing.T) {
	body, err := CompileRequest(protocol.ParsedRequest{Context: protocol.Context{
		SystemPrompt: []string{"sys"},
		Messages: []protocol.Message{
			{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}}},
			{Role: protocol.RoleAssistant, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "yo"}}},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	contents := body["contents"].([]map[string]any)
	if contents[0]["role"] != "user" || contents[1]["role"] != "model" {
		t.Fatalf("roles=%#v", contents)
	}
	if _, ok := body["systemInstruction"]; !ok {
		t.Fatal("system")
	}
}

func TestCompileRequestProjectsToolChoiceAndNamespaces(t *testing.T) {
	none, err := CompileRequest(protocol.ParsedRequest{
		Context: protocol.Context{
			Tools:    []protocol.Tool{{Name: "lookup", Parameters: map[string]any{"type": "object"}}},
			Messages: []protocol.Message{{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}}}},
		},
		Options: protocol.RequestOptions{ToolChoice: &protocol.ToolChoice{Kind: protocol.ToolChoiceNone}},
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg := none["toolConfig"].(map[string]any)["functionCallingConfig"].(map[string]any)
	if cfg["mode"] != "NONE" {
		t.Fatalf("none=%#v", cfg)
	}

	named, err := CompileRequest(protocol.ParsedRequest{
		Context: protocol.Context{
			Tools:    []protocol.Tool{{Name: "lookup", Namespace: "mcp", Parameters: map[string]any{"type": "object"}}},
			Messages: []protocol.Message{{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}}}},
		},
		Options: protocol.RequestOptions{ToolChoice: &protocol.ToolChoice{Kind: protocol.ToolChoiceNamed, Name: "mcp.lookup"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	decl := named["tools"].([]map[string]any)[0]["functionDeclarations"].([]map[string]any)[0]
	if decl["name"] != "mcp__lookup" {
		t.Fatalf("decl=%#v", decl)
	}
	names := named["toolConfig"].(map[string]any)["functionCallingConfig"].(map[string]any)["allowedFunctionNames"].([]string)
	if len(names) != 1 || names[0] != "mcp__lookup" {
		t.Fatalf("names=%v", names)
	}

	auto, err := CompileRequest(protocol.ParsedRequest{
		Context: protocol.Context{
			Tools:    []protocol.Tool{{Name: "lookup", Parameters: map[string]any{"type": "object"}}},
			Messages: []protocol.Message{{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}}}},
		},
		Options: protocol.RequestOptions{ToolChoice: &protocol.ToolChoice{Kind: protocol.ToolChoiceAuto}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := auto["toolConfig"]; ok {
		t.Fatalf("auto leaked toolConfig %#v", auto["toolConfig"])
	}

	filtered, err := CompileRequest(protocol.ParsedRequest{
		Context: protocol.Context{
			Tools: []protocol.Tool{
				{Name: "keep", Parameters: map[string]any{"type": "object"}},
				{Name: "drop", Parameters: map[string]any{"type": "object"}},
			},
			Messages: []protocol.Message{{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}}}},
		},
		Options: protocol.RequestOptions{ToolChoice: &protocol.ToolChoice{
			Kind: protocol.ToolChoiceAllowed, AllowedTools: []string{"keep"}, AllowedMode: protocol.ToolChoiceModeRequired,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	decls := filtered["tools"].([]map[string]any)[0]["functionDeclarations"].([]map[string]any)
	if len(decls) != 1 || decls[0]["name"] != "keep" {
		t.Fatalf("filtered=%#v", decls)
	}
	if filtered["toolConfig"].(map[string]any)["functionCallingConfig"].(map[string]any)["mode"] != "ANY" {
		t.Fatalf("required allowed=%#v", filtered["toolConfig"])
	}
}

func TestCompileRequestPairsToolCallsWithThoughtSignatures(t *testing.T) {
	body, err := CompileRequest(protocol.ParsedRequest{Context: protocol.Context{
		Tools: []protocol.Tool{{Name: "lookup", Description: "d", Parameters: map[string]any{"type": "object"}}},
		Messages: []protocol.Message{
			{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}}},
			{Role: protocol.RoleAssistant, Content: []protocol.ContentPart{{
				Type: protocol.ContentToolCall, ToolCallID: "c1", ToolName: "lookup",
				Arguments: map[string]any{"q": "x"}, ThoughtSignature: "sig-real",
			}}},
			{Role: protocol.RoleToolResult, ToolCallID: "c1", ToolName: "lookup", Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "out"}}},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	contents := body["contents"].([]map[string]any)
	assistant := contents[1]["parts"].([]map[string]any)[0]
	if assistant["thoughtSignature"] != "sig-real" {
		t.Fatalf("sig=%#v", assistant)
	}
	if call := assistant["functionCall"].(map[string]any); call["id"] != "c1" || call["name"] != "lookup" {
		t.Fatalf("call=%#v", call)
	}
	result := contents[2]["parts"].([]map[string]any)[0]["functionResponse"].(map[string]any)
	if result["id"] != "c1" || result["name"] != "lookup" {
		t.Fatalf("result=%#v", result)
	}
	synthetic, err := CompileRequest(protocol.ParsedRequest{Context: protocol.Context{Messages: []protocol.Message{
		{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}}},
		{Role: protocol.RoleAssistant, Content: []protocol.ContentPart{{
			Type: protocol.ContentToolCall, ToolCallID: "c1", ToolName: "lookup", ThoughtSignature: "fc_synthetic",
		}}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	part := synthetic["contents"].([]map[string]any)[1]["parts"].([]map[string]any)[0]
	if _, ok := part["thoughtSignature"]; ok {
		t.Fatalf("synthetic leaked=%#v", part)
	}
}

func TestCompileRequestInlinesDataImagesAndMarkersRemoteURLs(t *testing.T) {
	png := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="
	body, err := CompileRequest(protocol.ParsedRequest{Context: protocol.Context{Messages: []protocol.Message{{
		Role: protocol.RoleUser,
		Content: []protocol.ContentPart{
			{Type: protocol.ContentText, Text: "see"},
			{Type: protocol.ContentImage, ImageURL: "data:image/png;base64," + png},
			{Type: protocol.ContentImage, ImageURL: "https://example.com/x.png"},
		},
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	parts := body["contents"].([]map[string]any)[0]["parts"].([]map[string]any)
	if _, ok := parts[1]["inline_data"]; !ok {
		t.Fatalf("inline=%#v", parts)
	}
	if parts[2]["text"] != "[image: https://example.com/x.png]" {
		t.Fatalf("marker=%#v", parts[2])
	}
	if _, err := CompileRequest(protocol.ParsedRequest{Context: protocol.Context{Messages: []protocol.Message{{
		Role:    protocol.RoleUser,
		Content: []protocol.ContentPart{{Type: protocol.ContentImage, ImageURL: "data:image/png;base64,%%%"}},
	}}}}); err == nil {
		t.Fatal("bad base64")
	}
}

func TestCompileRequestRepairsMissingToolResult(t *testing.T) {
	body, err := CompileRequest(protocol.ParsedRequest{Context: protocol.Context{Messages: []protocol.Message{
		{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}}},
		{Role: protocol.RoleAssistant, Content: []protocol.ContentPart{{
			Type: protocol.ContentToolCall, ToolCallID: "c1", ToolName: "lookup", Arguments: map[string]any{"q": "x"},
		}}},
		{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "next"}}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	contents := body["contents"].([]map[string]any)
	if len(contents) != 4 {
		t.Fatalf("contents=%#v", contents)
	}
	resp := contents[2]["parts"].([]map[string]any)[0]["functionResponse"].(map[string]any)
	if resp["id"] != "c1" || resp["name"] != "lookup" {
		t.Fatalf("resp=%#v", resp)
	}
	result := resp["response"].(map[string]any)["result"].(string)
	if !strings.Contains(result, "execution status unknown") || strings.Contains(strings.ToLower(result), "success") && !strings.Contains(result, "do not treat this as success") {
		t.Fatalf("missing marker=%q", result)
	}
	if contents[3]["role"] != "user" {
		t.Fatalf("barrier=%#v", contents[3])
	}
}

func TestCompileRequestDegradesOrphanToolResultToText(t *testing.T) {
	body, err := CompileRequest(protocol.ParsedRequest{Context: protocol.Context{Messages: []protocol.Message{
		{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}}},
		{Role: protocol.RoleToolResult, ToolCallID: "orphan", ToolName: "lookup", Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "leaked"}}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	contents := body["contents"].([]map[string]any)
	if len(contents) != 2 {
		t.Fatalf("contents=%#v", contents)
	}
	parts := contents[1]["parts"].([]map[string]any)
	if _, ok := parts[0]["functionResponse"]; ok {
		t.Fatalf("orphan stayed a functionResponse: %#v", parts)
	}
	text, _ := parts[0]["text"].(string)
	if !strings.Contains(text, "orphaned tool result") || !strings.Contains(text, "leaked") {
		t.Fatalf("orphan text=%q", text)
	}
}

func TestCompileRequestRepairsParallelToolResultsInCallOrder(t *testing.T) {
	body, err := CompileRequest(protocol.ParsedRequest{Context: protocol.Context{Messages: []protocol.Message{
		{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}}},
		{Role: protocol.RoleAssistant, Content: []protocol.ContentPart{
			{Type: protocol.ContentToolCall, ToolCallID: "c1", ToolName: "first", Arguments: map[string]any{}},
			{Type: protocol.ContentToolCall, ToolCallID: "c2", ToolName: "second", Arguments: map[string]any{}},
		}},
		{Role: protocol.RoleToolResult, ToolCallID: "c2", ToolName: "second", Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "two"}}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	contents := body["contents"].([]map[string]any)
	if len(contents) != 3 {
		t.Fatalf("contents=%#v", contents)
	}
	parts := contents[2]["parts"].([]map[string]any)
	if len(parts) != 2 {
		t.Fatalf("parts=%#v", parts)
	}
	first := parts[0]["functionResponse"].(map[string]any)
	second := parts[1]["functionResponse"].(map[string]any)
	if first["id"] != "c1" || first["name"] != "first" {
		t.Fatalf("first=%#v", first)
	}
	if second["id"] != "c2" || second["response"].(map[string]any)["result"] != "two" {
		t.Fatalf("second=%#v", second)
	}
	missing := first["response"].(map[string]any)["result"].(string)
	if !strings.Contains(missing, "execution status unknown") {
		t.Fatalf("missing=%q", missing)
	}
}

func TestCompileRequestProjectsJSONSchemaStructuredOutput(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"answer": map[string]any{"type": "string"},
		},
		"required": []string{"answer"},
	}
	body, err := CompileRequest(protocol.ParsedRequest{
		Context: protocol.Context{Messages: []protocol.Message{{
			Role: protocol.RoleUser,
			Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "answer"}},
		}}},
		Options: protocol.RequestOptions{TextFormat: &protocol.TextFormat{
			Type: "json_schema", Name: "answer", Schema: schema,
		}},
		StructuredOutput: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	gc, ok := body["generationConfig"].(map[string]any)
	if !ok {
		t.Fatalf("generationConfig=%#v", body["generationConfig"])
	}
	if gc["responseMimeType"] != "application/json" {
		t.Fatalf("generationConfig=%#v", gc)
	}
	got, ok := gc["responseJsonSchema"].(map[string]any)
	if !ok || got["type"] != "object" {
		t.Fatalf("responseJsonSchema=%#v", gc["responseJsonSchema"])
	}
}

func TestCompileRequestProjectsJSONObjectWithoutInventingSchema(t *testing.T) {
	body, err := CompileRequest(protocol.ParsedRequest{
		Context: protocol.Context{Messages: []protocol.Message{{
			Role: protocol.RoleUser,
			Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "answer"}},
		}}},
		Options:          protocol.RequestOptions{TextFormat: &protocol.TextFormat{Type: "json_object"}},
		StructuredOutput: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	gc, ok := body["generationConfig"].(map[string]any)
	if !ok || gc["responseMimeType"] != "application/json" {
		t.Fatalf("generationConfig=%#v", body["generationConfig"])
	}
	if _, ok := gc["responseJsonSchema"]; ok {
		t.Fatalf("json_object invented schema: %#v", gc)
	}
}

