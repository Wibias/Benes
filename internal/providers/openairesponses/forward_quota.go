package openairesponses

import (
	"encoding/json"
	"net/http"
	"strings"
	"unicode/utf8"
)

func observeForwardQuota(observer ForwardOutcomeObserver, header http.Header) {
	quotaObserver, ok := observer.(ForwardQuotaObserver)
	if !ok {
		return
	}
	quotaObserver.ObserveQuota(ForwardQuotaHeaders{
		PrimaryUsedPercent:     header.Get("X-Codex-Primary-Used-Percent"),
		SecondaryUsedPercent:   header.Get("X-Codex-Secondary-Used-Percent"),
		TertiaryUsedPercent:    header.Get("X-Codex-Tertiary-Used-Percent"),
		PrimaryResetAt:         header.Get("X-Codex-Primary-Reset-At"),
		SecondaryResetAt:       header.Get("X-Codex-Secondary-Reset-At"),
		TertiaryResetAt:        header.Get("X-Codex-Tertiary-Reset-At"),
		PrimaryWindowMinutes:   header.Get("X-Codex-Primary-Window-Minutes"),
		SecondaryWindowMinutes: header.Get("X-Codex-Secondary-Window-Minutes"),
	})
}

func isForwardQuotaRetry(statusCode int, body []byte) bool {
	if statusCode == http.StatusPaymentRequired || statusCode == http.StatusTooManyRequests {
		return true
	}
	if statusCode < 500 || statusCode >= 600 {
		return false
	}
	message := forwardQuotaFailureMessage(body)
	return isForwardQuotaFailureMessage(message)
}

func normalizedForwardQuotaOutcomeStatus(statusCode int, quotaFailure bool) int {
	if quotaFailure && statusCode >= 500 && statusCode < 600 {
		return http.StatusTooManyRequests
	}
	return statusCode
}

func forwardQuotaFailureMessage(body []byte) string {
	if len(body) == 0 || !utf8.Valid(body) {
		return ""
	}
	text := strings.TrimSpace(string(body))
	if text == "" {
		return ""
	}

	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		if looksLikeJSONPayload(text) {
			return ""
		}
		return text
	}

	switch root := payload.(type) {
	case string:
		return strings.TrimSpace(root)
	case map[string]any:
		for _, path := range [][]string{
			{"error", "message"},
			{"last_error", "message"},
			{"response", "error", "message"},
			{"response", "incomplete_details", "message"},
		} {
			if value, ok := nestedString(root, path...); ok {
				return strings.TrimSpace(value)
			}
		}
		if value, ok := root["message"].(string); ok {
			return strings.TrimSpace(value)
		}
		if value, ok := root["error"].(string); ok {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func looksLikeJSONPayload(text string) bool {
	if text == "" {
		return false
	}
	switch text[0] {
	case '{', '[', '"':
		return true
	default:
		return false
	}
}

func nestedString(root map[string]any, path ...string) (string, bool) {
	var current any = root
	for _, key := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return "", false
		}
		current, ok = object[key]
		if !ok {
			return "", false
		}
	}
	value, ok := current.(string)
	return value, ok
}

func isForwardQuotaFailureMessage(message string) bool {
	normalized := strings.ToLower(strings.Join(strings.Fields(message), " "))
	if normalized == "" {
		return false
	}
	if normalized == "402" || normalized == "429" {
		return true
	}
	for _, marker := range []string{
		"usage limit",
		"insufficient_quota",
		"exceeded your current quota",
		"quota exhausted",
		"account quota exceeded",
		"monthly quota exceeded",
		"daily quota exceeded",
		"rate limit exceeded",
		"rate limit has been reached",
		"rate limited",
		"too many requests",
		"resource_exhausted",
		"resource exhausted",
		"throttlingexception",
		"throttling",
	} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}
