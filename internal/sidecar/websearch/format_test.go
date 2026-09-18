package websearch

import (
	"strings"
	"testing"
)

func TestFormatSearchResultWrapsAnswerAsUntrusted(t *testing.T) {
	got := formatSearchResult("benes <script>", Result{
		Text: "Benes is a proxy.",
		Sources: []Source{
			{URL: "https://example.com", Title: "Example"},
			{URL: "https://second.example", Title: "Second"},
		},
	}, nil, false)
	if strings.Contains(got, "<script>") {
		t.Fatalf("query was not sanitized: %q", got)
	}
	if !strings.Contains(got, "UNTRUSTED") {
		t.Fatalf("missing untrusted marker: %q", got)
	}
	if !strings.Contains(got, "<web_search_result>") || !strings.Contains(got, "</web_search_result>") {
		t.Fatalf("missing untrusted boundary: %q", got)
	}
	if !strings.Contains(got, "Benes is a proxy.") {
		t.Fatalf("missing answer: %q", got)
	}
	if !strings.Contains(got, "[1] Example — https://example.com") {
		t.Fatalf("missing numbered source: %q", got)
	}
}

func TestFormatSearchResultClampsAnswerAndSources(t *testing.T) {
	sources := make([]Source, 10)
	for i := range sources {
		sources[i] = Source{URL: "https://example.com/" + strings.Repeat("x", i), Title: "T"}
	}
	got := formatSearchResult("q", Result{Text: strings.Repeat("a", 4001), Sources: sources}, nil, false)
	if !strings.Contains(got, "…[truncated]") {
		t.Fatalf("answer was not clamped: len=%d", len(got))
	}
	if strings.Count(got, "https://example.com/") > 8 {
		t.Fatalf("sources were not capped: %q", got)
	}
}

func TestFormatSearchResultStructuredJSON(t *testing.T) {
	got := formatSearchResult("q", Result{
		Text:    "ans",
		Sources: []Source{{URL: "https://example.com", Title: "T"}},
	}, nil, true)
	if strings.Contains(got, "<web_search_result>") || strings.Contains(got, "Sources:") {
		t.Fatalf("structured path used markdown: %q", got)
	}
	if !strings.Contains(got, `"query":"q"`) || !strings.Contains(got, `"answer":"ans"`) || !strings.Contains(got, `"url":"https://example.com"`) {
		t.Fatalf("missing JSON payload: %q", got)
	}
}

func TestFormatSearchResultOmitsUnsafeSources(t *testing.T) {
	got := formatSearchResult("q", Result{
		Text: "ans",
		Sources: []Source{
			{URL: "javascript:alert(1)", Title: "xss"},
			{URL: "https://example.com", Title: "Example"},
		},
	}, nil, false)
	if strings.Contains(got, "javascript:") {
		t.Fatalf("unsafe source leaked: %q", got)
	}
	if !strings.Contains(got, "[1] Example — https://example.com") {
		t.Fatalf("missing safe source: %q", got)
	}
}
