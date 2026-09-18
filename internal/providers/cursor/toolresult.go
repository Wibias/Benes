package cursor

import "strings"

type ToolResult struct {
	Text    string
	IsError bool
	HasBlob bool
}

func NormalizeToolResult(text string, parts []string, isError, hasBlob bool) ToolResult {
	if hasBlob {
		return ToolResult{Text: text, IsError: isError, HasBlob: true}
	}
	joined := strings.TrimSpace(text)
	if joined == "" && len(parts) > 0 {
		var b strings.Builder
		for _, part := range parts {
			b.WriteString(part)
		}
		joined = strings.TrimSpace(b.String())
	}
	if isError {
		if joined == "" {
			joined = "Tool failed."
		}
		return ToolResult{Text: joined, IsError: true}
	}
	if joined == "" {
		return ToolResult{Text: "Tool produced no output.", IsError: false}
	}
	return ToolResult{Text: joined, IsError: false}
}
