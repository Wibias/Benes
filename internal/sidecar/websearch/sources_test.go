package websearch

import (
	"strings"
	"testing"
)

func TestSanitizeSourcesKeepsOrdinaryHTTPAndHTTPS(t *testing.T) {
	got := SanitizeSources([]Source{
		{URL: "http://example.com/a", Title: "HTTP"},
		{URL: "https://example.com/b", Title: "HTTPS"},
		{URL: "file:///etc/passwd", Title: "file"},
		{URL: "data:text/html,hi", Title: "data"},
	})
	if len(got) != 2 || got[0].URL != "http://example.com/a" || got[1].URL != "https://example.com/b" {
		t.Fatalf("sources=%#v", got)
	}
}

func TestSanitizeSourcesRejectsControlCharactersAndEmptyHost(t *testing.T) {
	got := SanitizeSources([]Source{
		{URL: "https://example.com/\x1b", Title: "esc"},
		{URL: "https://", Title: "empty-host"},
		{URL: "https://ok.example", Title: strings.Repeat("t", 257)},
	})
	if len(got) != 1 || got[0].URL != "https://ok.example" || got[0].Title != "" {
		t.Fatalf("sources=%#v", got)
	}
}
