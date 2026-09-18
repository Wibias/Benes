package websearch

import (
	"strings"
	"testing"
)

func TestParseAnthropicSidecarSSECollectsTextAndSources(t *testing.T) {
	sse := strings.Join([]string{
		`event: content_block_start`,
		`data: {"type":"content_block_start","content_block":{"type":"web_search_tool_result","content":[{"type":"web_search_result","url":"https://example.com","title":"Example"}]}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"Benes is a proxy."}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","delta":{"type":"citations_delta","citation":{"type":"web_search_result_location","url":"https://second.example","title":"Second"}}}`,
		``,
	}, "\n")
	got, err := parseAnthropicSidecarSSE(strings.NewReader(sse))
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "Benes is a proxy." {
		t.Fatalf("text=%q", got.Text)
	}
	if len(got.Sources) != 2 || got.Sources[0].URL != "https://example.com" || got.Sources[1].URL != "https://second.example" {
		t.Fatalf("sources=%#v", got.Sources)
	}
}

func TestParseAnthropicSidecarSSEDropsUnsafeCitationSources(t *testing.T) {
	sse := strings.Join([]string{
		`event: content_block_start`,
		`data: {"type":"content_block_start","content_block":{"type":"web_search_tool_result","content":[{"type":"web_search_result","url":"javascript:alert(1)","title":"xss"},{"type":"web_search_result","url":"https://example.com","title":"Example"}]}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","delta":{"type":"citations_delta","citation":{"type":"web_search_result_location","url":"https://user:pass@evil.example/","title":"creds"}}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","delta":{"type":"citations_delta","citation":{"type":"web_search_result_location","url":"https://example.com","title":"Duplicate"}}}`,
		``,
	}, "\n")
	got, err := parseAnthropicSidecarSSE(strings.NewReader(sse))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Sources) != 1 || got.Sources[0].URL != "https://example.com" || got.Sources[0].Title != "Example" {
		t.Fatalf("sources=%#v", got.Sources)
	}
}

func TestParseAnthropicSidecarSSEToolResultErrorHasNoSources(t *testing.T) {
	sse := strings.Join([]string{
		`event: content_block_start`,
		`data: {"type":"content_block_start","content_block":{"type":"web_search_tool_result","content":{"type":"web_search_tool_result_error"}}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"fallback"}}`,
		``,
	}, "\n")
	got, err := parseAnthropicSidecarSSE(strings.NewReader(sse))
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "fallback" || len(got.Sources) != 0 {
		t.Fatalf("got=%#v", got)
	}
}

func TestParseAnthropicSidecarSSEErrorEventDoesNotLeakBody(t *testing.T) {
	sse := strings.Join([]string{
		`event: error`,
		`data: {"type":"error","error":{"type":"authentication_error","message":"SECRET sk-leaked"}}`,
		``,
	}, "\n")
	_, err := parseAnthropicSidecarSSE(strings.NewReader(sse))
	if err == nil || strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), "sk-leaked") {
		t.Fatalf("leaked=%v", err)
	}
}
