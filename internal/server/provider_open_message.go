package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Wibias/Benes/internal/authpublic"
	"github.com/Wibias/Benes/internal/capability"
)

const (
	providerOpenDetailMaxRunes          = 240
	unsupportedStructuredOutputCode     = "unsupported_structured_output"
	unsupportedStructuredOutputMessage  = "Structured output is not supported by the selected provider/model."
)

func structuredOutputCapabilityRefusal(err error) bool {
	return errors.Is(err, capability.ErrStructuredOutputUnsupported)
}

func publicProviderOpenMessage(err error) string {
	if err == nil {
		return "provider request could not be opened"
	}
	if authpublic.IsAuthentication(err) {
		return authpublic.Project(err)
	}
	status, detail := providerHTTPOpenParts(err.Error())
	if status == http.StatusBadRequest {
		if sanitized := sanitizeProviderOpenDetail(detail); sanitized != "" {
			return "provider returned HTTP 400: " + sanitized
		}
		return "provider returned HTTP 400"
	}
	if status > 0 {
		return "provider returned HTTP " + strconv.Itoa(status)
	}
	return "provider request could not be opened"
}

func providerHTTPOpenParts(message string) (int, string) {
	idx := strings.LastIndex(message, "HTTP ")
	if idx < 0 {
		return 0, ""
	}
	rest := message[idx+5:]
	status := 0
	digits := 0
	for _, r := range rest {
		if r < '0' || r > '9' {
			break
		}
		status = status*10 + int(r-'0')
		digits++
		if digits == 3 {
			break
		}
	}
	if digits != 3 || status < 100 || status > 599 {
		return 0, ""
	}
	suffix := strings.TrimSpace(rest[digits:])
	detail := strings.TrimPrefix(suffix, ":")
	return status, strings.TrimSpace(detail)
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
