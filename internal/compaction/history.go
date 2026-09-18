package compaction

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

func ExtractUserMessages(input json.RawMessage) []string {
	trimmed := strings.TrimSpace(string(input))
	if trimmed == "" || trimmed == "null" || trimmed[0] != '[' {
		return nil
	}
	var items []json.RawMessage
	if json.Unmarshal(input, &items) != nil {
		return nil
	}
	out := make([]string, 0)
	for _, raw := range items {
		var rec struct {
			Type    string          `json:"type"`
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		}
		if json.Unmarshal(raw, &rec) != nil {
			continue
		}
		if rec.Type != "" && rec.Type != "message" {
			continue
		}
		if rec.Role != "user" {
			continue
		}
		text := userContentText(rec.Content)
		if strings.TrimSpace(text) != "" {
			out = append(out, text)
		}
	}
	return out
}

func BuildV1Output(userMessages []string, summary string) []map[string]any {
	selected := retainNewest(userMessages, CompactV1RetainedCharBudget)
	summaryText := strings.TrimSpace(summary)
	if summaryText == "" {
		summaryText = "(no summary available)"
	} else {
		summaryText = SummaryPrefix + "\n" + summary
	}
	out := make([]map[string]any, 0, len(selected)+1)
	for _, msg := range selected {
		out = append(out, userMessageItem(msg))
	}
	return append(out, userMessageItem(summaryText))
}

func userMessageItem(text string) map[string]any {
	return map[string]any{
		"type": "message",
		"role": "user",
		"content": []map[string]any{
			{"type": "input_text", "text": text},
		},
	}
}

func userContentText(raw json.RawMessage) string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return ""
	}
	if trimmed[0] == '"' {
		var text string
		if json.Unmarshal(raw, &text) != nil {
			return ""
		}
		return text
	}
	if trimmed[0] != '[' {
		return ""
	}
	var blocks []map[string]any
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	var b strings.Builder
	for _, block := range blocks {
		typ, _ := block["type"].(string)
		if typ != "input_text" && typ != "text" {
			continue
		}
		text, _ := block["text"].(string)
		b.WriteString(text)
	}
	return b.String()
}

func retainNewest(messages []string, budget int) []string {
	if budget <= 0 || len(messages) == 0 {
		return nil
	}
	selected := make([]string, 0, len(messages))
	remaining := budget
	for i := len(messages) - 1; i >= 0 && remaining > 0; i-- {
		msg := messages[i]
		n := utf8.RuneCountInString(msg)
		if n <= remaining {
			selected = append(selected, msg)
			remaining -= n
			continue
		}
		selected = append(selected, tailRunes(msg, remaining))
		break
	}
	for left, right := 0, len(selected)-1; left < right; left, right = left+1, right-1 {
		selected[left], selected[right] = selected[right], selected[left]
	}
	return selected
}

func tailRunes(msg string, n int) string {
	if n <= 0 {
		return ""
	}
	runes := []rune(msg)
	if len(runes) <= n {
		return msg
	}
	return string(runes[len(runes)-n:])
}
