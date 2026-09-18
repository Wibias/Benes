package usage

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestFormatMarkdownTableIsIndependentOfJSON(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, loc).UnixMilli()
	entries := []entry{
		{Timestamp: time.Date(2026, 8, 21, 12, 0, 0, 0, loc).UnixMilli(), Provider: "openai", Model: "gpt-5", UsageStatus: "reported", TotalTokens: int64ptr(10)},
	}
	got := SummarizeQuery(entries, Query{Start: "2026-08-20", End: "2026-08-22", Now: now, Location: loc}, NewTable())
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	md := FormatMarkdown(got)
	if !strings.Contains(md, "| Date | Requests | Tokens |") {
		t.Fatalf("missing table header: %s", md)
	}
	if !strings.Contains(md, "2026-08-21") || !strings.Contains(md, "openai/gpt-5") {
		t.Fatalf("missing rows: %s", md)
	}
	if strings.Contains(md, string(raw)) {
		t.Fatalf("markdown echoed json payload")
	}
	if !strings.Contains(string(raw), `"requests":1`) {
		t.Fatalf("json missing requests: %s", raw)
	}
}
