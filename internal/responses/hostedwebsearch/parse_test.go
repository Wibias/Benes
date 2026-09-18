package hostedwebsearch

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
)

func TestParseWebSearchPreservesSupportedFields(t *testing.T) {
	got, err := Parse(json.RawMessage(`{
		"type":"web_search",
		"search_context_size":"medium",
		"search_content_types":["text","image"],
		"indexed_web_access":true,
		"external_web_access":false,
		"user_location":{
			"type":"approximate",
			"country":"DE",
			"city":"Berlin",
			"region":"Berlin",
			"timezone":"Europe/Berlin"
		},
		"filters":{"allowed_domains":["example.com","openai.com"]}
	}`))
	if err != nil {
		t.Fatal(err)
	}

	indexed := true
	external := false
	want := protocol.HostedWebSearchTool{
		Type:               protocol.HostedWebSearchWebSearch,
		SearchContextSize:  "medium",
		SearchContentTypes: []string{"text", "image"},
		IndexedWebAccess:   &indexed,
		ExternalWebAccess:  &external,
		UserLocation: &protocol.HostedWebSearchLocation{
			Type:     "approximate",
			Country:  "DE",
			City:     "Berlin",
			Region:   "Berlin",
			Timezone: "Europe/Berlin",
		},
		Filters: &protocol.HostedWebSearchFilters{
			AllowedDomains: []string{"example.com", "openai.com"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Parse() = %#v, want %#v", got, want)
	}
}

func TestParseWebSearchPreviewPreservesSupportedSubset(t *testing.T) {
	got, err := Parse(json.RawMessage(`{
		"type":"web_search_preview",
		"search_context_size":"low",
		"search_content_types":["text","image"],
		"user_location":{"type":"approximate","country":"DE"}
	}`))
	if err != nil {
		t.Fatal(err)
	}

	want := protocol.HostedWebSearchTool{
		Type:               protocol.HostedWebSearchPreview,
		SearchContextSize:  "low",
		SearchContentTypes: []string{"text", "image"},
		UserLocation: &protocol.HostedWebSearchLocation{
			Type:    "approximate",
			Country: "DE",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Parse() = %#v, want %#v", got, want)
	}
}

func TestParseRejectsMalformedAndUnsupportedShapes(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantErr string
	}{
		{name: "non object", raw: `[]`, wantErr: "must be an object"},
		{name: "missing type", raw: `{"search_context_size":"low"}`, wantErr: "requires type"},
		{name: "unknown type", raw: `{"type":"web_search_future"}`, wantErr: "unsupported hosted web search type"},
		{name: "unknown top level field", raw: `{"type":"web_search","extra":true}`, wantErr: "unsupported hosted web search field"},
		{name: "invalid context size", raw: `{"type":"web_search","search_context_size":"huge"}`, wantErr: "invalid search_context_size"},
		{name: "context size wrong type", raw: `{"type":"web_search","search_context_size":12}`, wantErr: "search_context_size must be a string"},
		{name: "content types wrong type", raw: `{"type":"web_search","search_content_types":"text"}`, wantErr: "search_content_types must be a string array"},
		{name: "unsupported content type", raw: `{"type":"web_search","search_content_types":["video"]}`, wantErr: "unsupported search_content_types value"},
		{name: "too many content types", raw: `{"type":"web_search","search_content_types":["text","image","text"]}`, wantErr: "search_content_types exceeds 2 entries"},
		{name: "indexed wrong type", raw: `{"type":"web_search","indexed_web_access":"yes"}`, wantErr: "indexed_web_access must be a boolean"},
		{name: "external null", raw: `{"type":"web_search","external_web_access":null}`, wantErr: "external_web_access must be a boolean"},
		{name: "preview indexed", raw: `{"type":"web_search_preview","indexed_web_access":true}`, wantErr: "indexed_web_access is not supported"},
		{name: "preview external", raw: `{"type":"web_search_preview","external_web_access":true}`, wantErr: "external_web_access is not supported"},
		{name: "preview filters", raw: `{"type":"web_search_preview","filters":{"allowed_domains":["example.com"]}}`, wantErr: "filters are not supported"},
		{name: "location wrong type", raw: `{"type":"web_search","user_location":{"type":"precise"}}`, wantErr: `user_location.type must be "approximate"`},
		{name: "location unknown field", raw: `{"type":"web_search","user_location":{"type":"approximate","latitude":52.5}}`, wantErr: "unsupported user_location field"},
		{name: "filter unknown field", raw: `{"type":"web_search","filters":{"blocked_domains":["example.com"]}}`, wantErr: "unsupported filters field"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(json.RawMessage(tc.raw))
			if err == nil {
				t.Fatalf("Parse(%s) unexpectedly succeeded", tc.raw)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Parse(%s) error = %q, want substring %q", tc.raw, err, tc.wantErr)
			}
		})
	}
}

func TestParseEnforcesAllowedDomainsLimit(t *testing.T) {
	domains := make([]string, 100)
	for i := range domains {
		domains[i] = "example.com"
	}

	tool := map[string]any{
		"type": "web_search",
		"filters": map[string]any{
			"allowed_domains": domains,
		},
	}
	raw, err := json.Marshal(tool)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Parse(raw)
	if err != nil {
		t.Fatalf("100 allowed domains should be accepted: %v", err)
	}
	if got.Filters == nil || len(got.Filters.AllowedDomains) != 100 {
		t.Fatalf("allowed domains = %#v", got.Filters)
	}

	domains = append(domains, "overflow.example")
	tool["filters"].(map[string]any)["allowed_domains"] = domains
	raw, err = json.Marshal(tool)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(raw); err == nil || !strings.Contains(err.Error(), "filters.allowed_domains exceeds 100 entries") {
		t.Fatalf("101 allowed domains error = %v", err)
	}
}

func TestParseDeclaredReturnsOnlyHostedSearchTools(t *testing.T) {
	tools := []json.RawMessage{
		json.RawMessage(`{"type":"function","name":"lookup","parameters":{"type":"object"}}`),
		json.RawMessage(`{"type":"web_search","search_context_size":"high"}`),
		json.RawMessage(`{"type":"computer_use_preview"}`),
		json.RawMessage(`{"type":"web_search_preview","search_context_size":"low"}`),
	}

	got, err := ParseDeclared(tools)
	if err != nil {
		t.Fatal(err)
	}
	want := []protocol.HostedWebSearchTool{
		{Type: protocol.HostedWebSearchWebSearch, SearchContextSize: "high"},
		{Type: protocol.HostedWebSearchPreview, SearchContextSize: "low"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseDeclared() = %#v, want %#v", got, want)
	}
}

func TestParseDeclaredRejectsMalformedAndUnknownWebSearchDeclarations(t *testing.T) {
	cases := []struct {
		name    string
		tool    json.RawMessage
		wantErr string
	}{
		{name: "non object", tool: json.RawMessage(`[]`), wantErr: "tools[0] must be an object"},
		{name: "missing type", tool: json.RawMessage(`{"name":"lookup"}`), wantErr: "tools[0]: requires type"},
		{name: "unknown web search variant", tool: json.RawMessage(`{"type":"web_search_preview_2025_03_11"}`), wantErr: "unsupported hosted web search type"},
		{name: "malformed hosted declaration", tool: json.RawMessage(`{"type":"web_search","filters":{"blocked_domains":["example.com"]}}`), wantErr: "unsupported filters field"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseDeclared([]json.RawMessage{tc.tool})
			if err == nil {
				t.Fatalf("ParseDeclared(%s) unexpectedly succeeded", tc.tool)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("ParseDeclared(%s) error = %q, want substring %q", tc.tool, err, tc.wantErr)
			}
		})
	}
}
