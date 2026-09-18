package server

import (
	"net/http"

	"github.com/Wibias/Benes/internal/providers"
)

func (h *handler) snapshotForwardHeaders(header http.Header) providers.ForwardHeaders {
	names := providers.ForwardHeaderNames()
	values := make(map[string]string, len(names))
	for _, name := range names {
		if value := header.Get(name); value != "" {
			values[name] = value
		}
	}
	blockedAuthorization := tokenHeaderMatches(
		header.Get("Authorization"),
		"Bearer ",
		h.admission.tokenHashes,
	)
	return providers.NewForwardHeadersWithBlockedAuthorization(values, blockedAuthorization)
}
