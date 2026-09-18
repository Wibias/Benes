package server

import (
	"strings"

	"github.com/Wibias/Benes/internal/credentials"
	"github.com/Wibias/Benes/internal/oauth/loginown"
	"github.com/Wibias/Benes/internal/providers/antigravity"
)

func (h *handler) purgeRemovedProviderSecrets(name string) {
	if h == nil || strings.TrimSpace(name) == "" {
		return
	}
	loginown.Cancel(name)
	removeOAuthPendingFile(h.benesHome(), name)
	if path := h.oauthAuthStorePath(); path != "" {
		_ = antigravity.ClearAccountsFor(path, name)
	}
	h.deleteStoredProviderAPIKeys(name)
}

func (h *handler) deleteStoredProviderAPIKeys(name string) {
	if h.credentials == nil {
		return
	}
	ids := map[string]struct{}{name: {}}
	if id := h.activeCredentialID(name); id != "" {
		ids[id] = struct{}{}
	}
	for _, entry := range h.loadAPIKeyPool(name) {
		if id := strings.TrimSpace(entry.ID); id != "" {
			ids[id] = struct{}{}
		}
	}
	for id := range ids {
		_ = h.credentials.Delete(credentials.Ref{ID: id, Source: credentials.SourceSecureStore})
	}
}
