package google

import (
	"fmt"
	"strings"
)

func TruncatedTurn(reason string, toolCallsStarted int) bool {
	switch reason {
	case "MALFORMED_FUNCTION_CALL":
		return true
	case "MAX_TOKENS":
		return toolCallsStarted > 0
	default:
		return false
	}
}

func StopReason(reason string) string {
	if reason == "MAX_TOKENS" {
		return "max_tokens"
	}
	switch reason {
	case "SAFETY", "RECITATION", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII":
		return "content_filter"
	default:
		return ""
	}
}

func TruncationMessage(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return "Google response truncated upstream before the turn completed"
	}
	return fmt.Sprintf("Google response truncated upstream before the turn completed (%s)", reason)
}
