package compaction

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestExtractUserMessagesKeepsRealUserText(t *testing.T) {
	input := json.RawMessage(`[
		{"type":"message","role":"user","content":[{"type":"input_text","text":"keep"}]},
		{"type":"message","role":"assistant","content":[{"type":"output_text","text":"drop"}]},
		{"type":"function_call","name":"tool"},
		{"role":"user","content":"also"}
	]`)
	got := ExtractUserMessages(input)
	if len(got) != 2 || got[0] != "keep" || got[1] != "also" {
		t.Fatalf("got=%q", got)
	}
}

func TestBuildV1OutputAppendsSummaryAfterRetainedUserMessages(t *testing.T) {
	out := BuildV1Output([]string{"one", "two"}, "summary")
	if len(out) != 3 {
		t.Fatalf("len=%d", len(out))
	}
	if textFrom(t, out[0]) != "one" || textFrom(t, out[1]) != "two" {
		t.Fatalf("retained=%#v", out)
	}
	if textFrom(t, out[2]) != SummaryPrefix+"\nsummary" {
		t.Fatalf("summary=%q", textFrom(t, out[2]))
	}
}

func TestBuildV1OutputDegradesEmptySummary(t *testing.T) {
	out := BuildV1Output(nil, "  ")
	if len(out) != 1 || textFrom(t, out[0]) != "(no summary available)" {
		t.Fatalf("out=%#v", out)
	}
}

func TestBuildV1OutputRetainsNewestMessagesWithinBudget(t *testing.T) {
	old := strings.Repeat("a", CompactV1RetainedCharBudget/4)
	newer := "recent"
	out := BuildV1Output([]string{old, newer}, "sum")
	if len(out) != 3 {
		t.Fatalf("len=%d", len(out))
	}
	if textFrom(t, out[0]) != old || textFrom(t, out[1]) != newer {
		t.Fatalf("order=%q %q", textFrom(t, out[0]), textFrom(t, out[1]))
	}
}

func TestBuildV1OutputTruncatesOldestMessageToFitBudget(t *testing.T) {
	old := strings.Repeat("a", CompactV1RetainedCharBudget)
	newer := "recent"
	out := BuildV1Output([]string{old, newer}, "sum")
	if len(out) != 3 {
		t.Fatalf("len=%d", len(out))
	}
	kept := textFrom(t, out[0])
	if kept == old || !strings.HasSuffix(kept, "a") || textFrom(t, out[1]) != newer {
		t.Fatalf("kept len=%d newer=%q", len(kept), textFrom(t, out[1]))
	}
}

func textFrom(t *testing.T, item map[string]any) string {
	t.Helper()
	content, ok := item["content"].([]map[string]any)
	if !ok || len(content) == 0 {
		t.Fatalf("item=%#v", item)
	}
	text, _ := content[0]["text"].(string)
	return text
}
