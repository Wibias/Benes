package openairesponses

import (
	"reflect"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
)

func boolValuePtr(v bool) *bool { return &v }

func hostedSearchFixture(kind protocol.HostedWebSearchKind) protocol.HostedWebSearchTool {
	tool := protocol.HostedWebSearchTool{
		Type:               kind,
		SearchContextSize:  "high",
		SearchContentTypes: []string{"text", "image"},
		UserLocation: &protocol.HostedWebSearchLocation{
			Type: "approximate", Country: "DE", City: "Berlin",
		},
	}
	if kind == protocol.HostedWebSearchWebSearch {
		tool.IndexedWebAccess = boolValuePtr(true)
		tool.ExternalWebAccess = boolValuePtr(true)
		tool.Filters = &protocol.HostedWebSearchFilters{AllowedDomains: []string{"example.com"}}
	}
	return tool
}

func TestSanitizeOpenCodeGoMuseWebSearchStripsOnlyStrictPlainFields(t *testing.T) {
	for _, model := range []string{"muse-spark-1.3-contributor", "muse-spark-1.2-contributor"} {
		t.Run(model, func(t *testing.T) {
			originalTool := hostedSearchFixture(protocol.HostedWebSearchWebSearch)
			req := protocol.ParsedRequest{
				UpstreamModelID:      model,
				HostedWebSearchTools: []protocol.HostedWebSearchTool{originalTool},
			}
			got := SanitizeOpenCodeGoMuseWebSearch(req, "https://opencode.ai/zen/go/v1/responses")
			tool := got.HostedWebSearchTools[0]
			if tool.SearchContentTypes != nil || tool.IndexedWebAccess != nil {
				t.Fatalf("strict fields survived: %#v", tool)
			}
			if tool.Type != protocol.HostedWebSearchWebSearch || tool.SearchContextSize != "high" || tool.ExternalWebAccess == nil || !*tool.ExternalWebAccess {
				t.Fatalf("unrelated hosted fields changed: %#v", tool)
			}
			if tool.UserLocation == nil || tool.UserLocation.City != "Berlin" || tool.Filters == nil || !reflect.DeepEqual(tool.Filters.AllowedDomains, []string{"example.com"}) {
				t.Fatalf("nested hosted fields changed: %#v", tool)
			}
			if req.HostedWebSearchTools[0].SearchContentTypes == nil || req.HostedWebSearchTools[0].IndexedWebAccess == nil {
				t.Fatalf("input request mutated: %#v", req.HostedWebSearchTools[0])
			}
		})
	}
}

func TestSanitizeOpenCodeGoMuseWebSearchPreservesPreview(t *testing.T) {
	req := protocol.ParsedRequest{
		UpstreamModelID:      "muse-spark-1.3-contributor",
		HostedWebSearchTools: []protocol.HostedWebSearchTool{hostedSearchFixture(protocol.HostedWebSearchPreview)},
	}
	got := SanitizeOpenCodeGoMuseWebSearch(req, "https://opencode.ai/zen/go/v1/responses")
	if !reflect.DeepEqual(got.HostedWebSearchTools, req.HostedWebSearchTools) {
		t.Fatalf("preview changed: got=%#v want=%#v", got.HostedWebSearchTools, req.HostedWebSearchTools)
	}
}

func TestSanitizeOpenCodeGoMuseWebSearchLeavesUnrelatedModelAndDestinationAlone(t *testing.T) {
	base := protocol.ParsedRequest{
		UpstreamModelID:      "gpt-5.6-luna",
		HostedWebSearchTools: []protocol.HostedWebSearchTool{hostedSearchFixture(protocol.HostedWebSearchWebSearch)},
	}
	if got := SanitizeOpenCodeGoMuseWebSearch(base, "https://opencode.ai/zen/go/v1/responses"); !reflect.DeepEqual(got.HostedWebSearchTools, base.HostedWebSearchTools) {
		t.Fatalf("unrelated model changed: %#v", got.HostedWebSearchTools)
	}
	muse := base
	muse.UpstreamModelID = "muse-spark-1.3-contributor"
	for _, destination := range []string{
		"https://api.openai.com/v1/responses",
		"https://opencode.ai/zen/go/v1/chat/completions",
		"https://opencode.ai/zen/go/v1/responses?compat=1",
	} {
		if got := SanitizeOpenCodeGoMuseWebSearch(muse, destination); !reflect.DeepEqual(got.HostedWebSearchTools, muse.HostedWebSearchTools) {
			t.Fatalf("destination %q changed request: %#v", destination, got.HostedWebSearchTools)
		}
	}
}
