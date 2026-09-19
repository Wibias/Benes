package providers

import (
	"net/http"
	"strings"
	"unicode"
)

const MaxUserAgentBytes = 512

func NormalizeUserAgent(value string) string {
	if value == "" || value != strings.TrimSpace(value) || len(value) > MaxUserAgentBytes {
		return ""
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return ""
		}
	}
	return value
}

func ApplyUserAgent(header http.Header, configured string, forwarded ForwardHeaders) {
	if header == nil {
		return
	}
	value := NormalizeUserAgent(configured)
	if value == "" {
		value = NormalizeUserAgent(forwarded.Get("user-agent"))
	}
	if value != "" {
		header.Set("User-Agent", value)
	}
}
