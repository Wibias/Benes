package openairesponses

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/responses/parsed"
	requestwire "github.com/Wibias/Benes/internal/responses/request"
	"github.com/Wibias/Benes/internal/sidecar/websearch"
)

func decodeHostedSearchParsed(t *testing.T, toolJSON string) protocol.ParsedRequest {
	t.Helper()
	raw := `{"model":"opencode-go/muse-spark-1.3-contributor","input":"search","tools":[` + toolJSON + `]}`
	wire, err := requestwire.Decode(strings.NewReader(raw), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	req, err := parsed.Build(wire, 0)
	if err != nil {
		t.Fatal(err)
	}
	req.UpstreamModelID = "muse-spark-1.3-contributor"
	return req
}

func TestNativeHostedWebSearchAdmission(t *testing.T) {
	for _, toolJSON := range []string{
		`{"type":"web_search"}`,
		`{"type":"web_search_preview"}`,
	} {
		req := decodeHostedSearchParsed(t, toolJSON)
		if err := validateMigratedRequestForCompile(req); err != nil {
			t.Fatalf("hosted tool should be admitted (%s): %v", toolJSON, err)
		}
	}
}

func TestNativeHostedWebSearchCompilationPreservesTypedFields(t *testing.T) {
	cases := []struct {
		name        string
		toolJSON    string
		wantType    string
		wantIndexed bool
	}{
		{
			name:        "web_search",
			toolJSON:    `{"type":"web_search","search_context_size":"medium","search_content_types":["text","image"],"indexed_web_access":true,"external_web_access":true,"user_location":{"type":"approximate","country":"DE","city":"Berlin"},"filters":{"allowed_domains":["example.com"]}}`,
			wantType:    "web_search",
			wantIndexed: true,
		},
		{
			name:     "web_search_preview",
			toolJSON: `{"type":"web_search_preview","search_context_size":"low","search_content_types":["text","image"],"user_location":{"type":"approximate","country":"DE"}}`,
			wantType: "web_search_preview",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := decodeHostedSearchParsed(t, tc.toolJSON)
			encoded, err := Compile(req)
			if err != nil {
				t.Fatal(err)
			}
			var body map[string]any
			if err := json.Unmarshal(encoded, &body); err != nil {
				t.Fatal(err)
			}
			tools, ok := body["tools"].([]any)
			if !ok || len(tools) != 1 {
				t.Fatalf("tools=%#v", body["tools"])
			}
			tool, ok := tools[0].(map[string]any)
			if !ok {
				t.Fatalf("tool=%#v", tools[0])
			}
			if tool["type"] != tc.wantType {
				t.Fatalf("type=%#v tool=%#v", tool["type"], tool)
			}
			if tool["search_context_size"] == nil || tool["search_content_types"] == nil || tool["user_location"] == nil {
				t.Fatalf("hosted search options lost: %#v", tool)
			}
			if tc.wantIndexed {
				if tool["indexed_web_access"] != true || tool["external_web_access"] != true || tool["filters"] == nil {
					t.Fatalf("plain web_search options lost: %#v", tool)
				}
			} else {
				for _, field := range []string{"indexed_web_access", "external_web_access", "filters"} {
					if _, exists := tool[field]; exists {
						t.Fatalf("preview gained unsupported %s: %#v", field, tool)
					}
				}
			}
			if _, exists := tool["name"]; exists {
				t.Fatalf("hosted tool was converted to a function: %#v", tool)
			}
		})
	}
}

func TestNativeHostedWebSearchRejectsMalformedAndUnknownShapes(t *testing.T) {
	cases := []string{
		`{"type":"web_search","search_context_size":12}`,
		`{"type":"web_search","search_content_types":"text"}`,
		`{"type":"web_search","indexed_web_access":"yes"}`,
		`{"type":"web_search","return_token_budget":"unlimited"}`,
		`{"type":"web_search","filters":{"blocked_domains":["example.com"]}}`,
		`{"type":"web_search_preview","search_context_size":"huge"}`,
		`{"type":"web_search_preview","indexed_web_access":true}`,
		`{"type":"web_search_preview","external_web_access":true}`,
		`{"type":"web_search_preview","filters":{"allowed_domains":["example.com"]}}`,
		`{"type":"web_search","unknown_field":true}`,
		`{"type":"web_search_future"}`,
	}
	for _, toolJSON := range cases {
		raw := `{"model":"opencode-go/muse-spark-1.3-contributor","input":"search","tools":[` + toolJSON + `]}`
		wire, err := requestwire.Decode(strings.NewReader(raw), 1<<20)
		if err == nil {
			_, err = parsed.Build(wire, 0)
		}
		if err == nil {
			t.Fatalf("malformed hosted tool accepted: %s", toolJSON)
		}
	}
}

func TestNativeHostedWebSearchKeepsFunctionToolsUnchanged(t *testing.T) {
	raw := `{"model":"openai-apikey/gpt-5.6","input":"search","tools":[{"type":"function","name":"lookup","description":"find","parameters":{"type":"object","properties":{"q":{"type":"string"}}}}]}`
	wire, err := requestwire.Decode(strings.NewReader(raw), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	req, err := parsed.Build(wire, 0)
	if err != nil {
		t.Fatal(err)
	}
	req.UpstreamModelID = "gpt-5.6"
	if err := ValidateMigratedRequest(req); err != nil {
		t.Fatal(err)
	}
	encoded, err := Compile(req)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(encoded, &body); err != nil {
		t.Fatal(err)
	}
	tool := body["tools"].([]any)[0].(map[string]any)
	if tool["type"] != "function" || tool["name"] != "lookup" {
		t.Fatalf("function tool changed: %#v", tool)
	}
}

func TestNativeHostedWebSearchToolChoicePreservesPreviewVariant(t *testing.T) {
	raw := `{"model":"openai-apikey/gpt-5.6","input":"search","tools":[{"type":"web_search_preview"}],"tool_choice":{"type":"web_search_preview"}}`
	wire, err := requestwire.Decode(strings.NewReader(raw), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	req, err := parsed.Build(wire, 0)
	if err != nil {
		t.Fatal(err)
	}
	req.UpstreamModelID = "gpt-5.6"
	encoded, err := Compile(req)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(encoded, &body); err != nil {
		t.Fatal(err)
	}
	choice, ok := body["tool_choice"].(map[string]any)
	if !ok || choice["type"] != "web_search_preview" {
		t.Fatalf("tool_choice=%#v", body["tool_choice"])
	}
}

func TestSidecarHostedWebSearchPreviewValidatesAndCompilesAsSyntheticFunction(t *testing.T) {
	raw := `{"model":"openai-apikey/gpt-5.6","input":"search","tools":[{"type":"web_search_preview"}],"tool_choice":{"type":"web_search_preview"}}`
	wire, err := requestwire.Decode(strings.NewReader(raw), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	req, err := parsed.Build(wire, 0)
	if err != nil {
		t.Fatal(err)
	}
	req.UpstreamModelID = "gpt-5.6"
	req.Context.Tools = websearch.ReplaceHostedTools(req.Context.Tools)
	req.HostedWebSearchTools = nil

	if err := validateMigratedRequestForCompile(req); err != nil {
		t.Fatalf("sidecar transformed request rejected: %v", err)
	}
	encoded, err := Compile(req)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(encoded, &body); err != nil {
		t.Fatal(err)
	}
	tools := body["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("tools=%#v", tools)
	}
	tool := tools[0].(map[string]any)
	if tool["type"] != "function" || tool["name"] != websearch.ToolName {
		t.Fatalf("sidecar tool=%#v", tool)
	}
	choice := body["tool_choice"].(map[string]any)
	if choice["type"] != "function" || choice["name"] != websearch.ToolName {
		t.Fatalf("sidecar tool_choice=%#v", choice)
	}
}

func TestSidecarHostedWebSearchRejectsUndeclaredPreviewChoice(t *testing.T) {
	raw := `{"model":"openai-apikey/gpt-5.6","input":"search","tools":[{"type":"web_search"}],"tool_choice":{"type":"web_search_preview"}}`
	wire, err := requestwire.Decode(strings.NewReader(raw), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	req, err := parsed.Build(wire, 0)
	if err != nil {
		t.Fatal(err)
	}
	req.UpstreamModelID = "gpt-5.6"
	req.Context.Tools = websearch.ReplaceHostedTools(req.Context.Tools)
	req.HostedWebSearchTools = nil

	if err := validateMigratedRequestForCompile(req); err != nil {
		t.Fatalf("sidecar transformed request failed shape validation before choice validation: %v", err)
	}
	if _, err := Compile(req); err == nil {
		t.Fatal("sidecar accepted undeclared web_search_preview tool_choice")
	}
}

func TestSidecarMarkerDoesNotAuthorizeHostedRawWithoutHostedDeclaration(t *testing.T) {
	req := protocol.ParsedRequest{
		UpstreamModelID: "gpt-5.6",
		Raw:             json.RawMessage(`{"model":"openai-apikey/gpt-5.6","input":"search","tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}]}`),
		Context: protocol.Context{Tools: []protocol.Tool{websearch.SyntheticTool()}},
	}
	if err := validateMigratedRequestForCompile(req); err == nil {
		t.Fatal("sidecar marker authorized request without hosted declaration")
	}
}

func TestCompileHostedWebSearchDoesNotReadParsedRaw(t *testing.T) {
	req := protocol.ParsedRequest{
		UpstreamModelID: "gpt-5.6",
		Raw:             json.RawMessage(`{"model":"raw-model","tools":[{"type":"web_search","evil_raw_field":"must-not-survive"}]}`),
		HostedWebSearchTools: []protocol.HostedWebSearchTool{{
			Type:              protocol.HostedWebSearchWebSearch,
			SearchContextSize: "low",
		}},
	}
	encoded, err := Compile(req)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "evil_raw_field") || strings.Contains(string(encoded), "raw-model") {
		t.Fatalf("raw request escaped canonical compiler: %s", encoded)
	}
	if !strings.Contains(string(encoded), `"type":"web_search"`) || !strings.Contains(string(encoded), `"search_context_size":"low"`) {
		t.Fatalf("typed hosted state was not compiled: %s", encoded)
	}
}
