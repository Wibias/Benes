package openairesponses

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/Wibias/Benes/internal/providers"
)

const (
	providerOpenDetailMaxRunes   = 240
	providerInputTooLargeMessage = "The provider rejected this turn because its input exceeds the provider size or context limit. Reduce the current input or compact the conversation before retrying."
)

func upstreamHTTPError(kind string, status int, body []byte) error {
	if status == http.StatusRequestEntityTooLarge {
		retryable := false
		return &providers.OpenError{
			StatusCode: status,
			ErrorType:  "invalid_request_error",
			Code:       "context_length_exceeded",
			Message:    providerInputTooLargeMessage,
			Retryable:  &retryable,
		}
	}
	if status == http.StatusBadRequest {
		if detail := sanitizeProviderOpenDetail(extractUpstreamErrorDetail(body)); detail != "" {
			return fmt.Errorf("%s returned HTTP %d: %s", kind, status, detail)
		}
	}
	return fmt.Errorf("%s returned HTTP %d", kind, status)
}

func extractUpstreamErrorDetail(body []byte) string {
	if len(body) == 0 || !utf8.Valid(body) {
		return ""
	}
	var root map[string]json.RawMessage
	if json.Unmarshal(body, &root) != nil || root == nil {
		return ""
	}
	if nested, ok := root["error"]; ok {
		var errObj map[string]json.RawMessage
		if json.Unmarshal(nested, &errObj) == nil && errObj != nil {
			if message, isString := ownJSONString(errObj, "message"); isString {
				return message
			}
		}
	}
	if message, isString := ownJSONString(root, "message"); isString {
		return message
	}
	if detail, isString := ownJSONString(root, "detail"); isString {
		return detail
	}
	return ""
}

func sanitizeProviderOpenDetail(raw string) string {
	trimmed := strings.TrimSpace(strings.Join(strings.Fields(raw), " "))
	if trimmed == "" || !utf8.ValidString(trimmed) {
		return ""
	}
	lower := strings.ToLower(trimmed)
	if strings.Contains(lower, "sk-") ||
		strings.Contains(lower, "rk-") ||
		strings.Contains(lower, "bearer ") ||
		strings.Contains(lower, "benes_") {
		return ""
	}
	if utf8.RuneCountInString(trimmed) <= providerOpenDetailMaxRunes {
		return trimmed
	}
	return string([]rune(trimmed)[:providerOpenDetailMaxRunes])
}
