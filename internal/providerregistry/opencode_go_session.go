package providerregistry

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode/utf8"

	"github.com/Wibias/Benes/internal/providers"
)

const (
	openCodeGoSessionDomain      = "benes/opencode-go/session/v1"
	maxOpenCodeGoIdentityRunes   = 256
	openCodeGoSessionValuePrefix = "benes_"
	openCodeGoSessionDigestBytes = 16
)

func bindOpenCodeGoSession(dispatch providers.DispatchRequest, override string) providers.DispatchRequest {
	if override != "" {
		return providers.WithOpenCodeGoSession(dispatch, override)
	}
	lane := openCodeGoLaneIdentity(dispatch.ForwardHeaders)
	if lane == "" {
		return dispatch
	}
	digest := sha256.Sum256([]byte(openCodeGoSessionDomain + "\x00" + lane))
	return providers.WithOpenCodeGoSession(
		dispatch,
		openCodeGoSessionValuePrefix+hex.EncodeToString(digest[:openCodeGoSessionDigestBytes]),
	)
}

func openCodeGoLaneIdentity(headers providers.ForwardHeaders) string {
	parent := sanitizeOpenCodeGoIdentity(headers.Get("x-codex-parent-thread-id"))
	thread := sanitizeOpenCodeGoIdentity(headers.Get("thread-id"))
	session := sanitizeOpenCodeGoIdentity(firstNonEmptySessionHeader(
		headers.Get("session-id"),
		headers.Get("session_id"),
	))

	specific := thread
	if specific == "" {
		specific = session
	}
	if parent != "" && specific != "" {
		return parent + "\x00" + specific
	}
	if specific != "" {
		return specific
	}
	return parent
}

func sanitizeOpenCodeGoIdentity(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || utf8.RuneCountInString(trimmed) > maxOpenCodeGoIdentityRunes {
		return ""
	}
	for _, r := range trimmed {
		if r < 32 || r == 127 {
			return ""
		}
	}
	return trimmed
}

func validOpenCodeGoSessionOverride(value string) bool {
	if value == "" || value != strings.TrimSpace(value) {
		return false
	}
	for _, r := range value {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}

func firstNonEmptySessionHeader(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
