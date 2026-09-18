package server

import (
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/oauth/cursor"
	"github.com/Wibias/Benes/internal/oauth/loginown"
	"github.com/Wibias/Benes/internal/providers/antigravity"
)

func (h *handler) serveOAuthAccountsAPI(w http.ResponseWriter, r *http.Request) bool {
	switch r.URL.Path {
	case "/api/oauth/accounts":
		return h.serveOAuthAccounts(w, r)
	case "/api/oauth/accounts/active":
		return h.serveOAuthAccountsActive(w, r)
	case "/api/oauth/accounts/import":
		return h.serveOAuthAccountsImport(w, r)
	case "/api/oauth/status":
		return h.serveOAuthStatus(w, r)
	default:
		return false
	}
}

func (h *handler) serveOAuthAccounts(w http.ResponseWriter, r *http.Request) bool {
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	if r.Method == http.MethodDelete {
		return h.serveOAuthAccountsDelete(w, r)
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
	provider := strings.TrimSpace(r.URL.Query().Get("provider"))
	if provider == "" || !isPublicOAuthProvider(provider) {
		writeError(w, http.StatusBadRequest, "unknown_oauth_provider", "unknown oauth provider")
		return true
	}
	accounts, activeID := h.oauthPublicAccounts(provider)
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		return true
	}
	rows := make([]map[string]any, 0, len(accounts))
	for _, account := range accounts {
		label := account.ProjectID
		if label == "" {
			label = account.Email
		}
		rows = append(rows, map[string]any{
			"id":          account.ID,
			"active":      account.ID == activeID,
			"needsReauth": account.NeedsReauth,
			"label":       label,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"accounts":        rows,
		"activeAccountId": activeID,
	})
	return true
}

func (h *handler) serveOAuthAccountsActive(w http.ResponseWriter, r *http.Request) bool {
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	if r.Method != http.MethodPut {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
	var body struct {
		Provider string `json:"provider"`
		ID       string `json:"id"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "invalid JSON body")
		return true
	}
	provider := strings.ToLower(strings.TrimSpace(body.Provider))
	id := strings.TrimSpace(body.ID)
	if provider == "" || !isPublicOAuthProvider(provider) {
		writeError(w, http.StatusBadRequest, "unknown_oauth_provider", "unknown oauth provider")
		return true
	}
	if id == "" {
		writeError(w, http.StatusBadRequest, "invalid_body", "id is required")
		return true
	}
	if !persistedOAuthProvider(provider) {
		writeError(w, http.StatusNotImplemented, "not_implemented", "oauth account switch for this provider is not on the Go data plane yet")
		return true
	}
	path := h.oauthAuthStorePath()
	if path == "" {
		writeError(w, http.StatusServiceUnavailable, "config_unreadable", "auth store path is required")
		return true
	}
	if err := antigravity.SetActiveAccountFor(path, provider, id); err != nil {
		if strings.Contains(err.Error(), "unknown account") {
			writeError(w, http.StatusNotFound, "unknown_account", "unknown account")
			return true
		}
		writeOAuthPublicError(w, http.StatusInternalServerError, err)
		return true
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "provider": provider, "activeAccountId": id})
	return true
}

func (h *handler) serveOAuthAccountsDelete(w http.ResponseWriter, r *http.Request) bool {
	provider := strings.TrimSpace(r.URL.Query().Get("provider"))
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if provider == "" || !isPublicOAuthProvider(provider) {
		writeError(w, http.StatusBadRequest, "unknown_oauth_provider", "unknown oauth provider")
		return true
	}
	if id == "" {
		writeError(w, http.StatusBadRequest, "invalid_body", "id is required")
		return true
	}
	if !persistedOAuthProvider(provider) {
		writeError(w, http.StatusNotImplemented, "not_implemented", "oauth account remove for this provider is not on the Go data plane yet")
		return true
	}
	path := h.oauthAuthStorePath()
	if path == "" {
		writeError(w, http.StatusServiceUnavailable, "config_unreadable", "auth store path is required")
		return true
	}
	if err := antigravity.RemoveAccountFor(path, provider, id); err != nil {
		if strings.Contains(err.Error(), "unknown account") {
			writeError(w, http.StatusNotFound, "unknown_account", "unknown account")
			return true
		}
		writeOAuthPublicError(w, http.StatusInternalServerError, err)
		return true
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "provider": provider, "id": id})
	return true
}

func (h *handler) serveOAuthStatus(w http.ResponseWriter, r *http.Request) bool {
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
	provider := strings.TrimSpace(r.URL.Query().Get("provider"))
	if provider == "" || !isPublicOAuthProvider(provider) {
		writeError(w, http.StatusBadRequest, "unknown_oauth_provider", "unknown oauth provider")
		return true
	}
	accounts, activeID := h.oauthPublicAccounts(provider)
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		return true
	}
	_, pending := loginown.Current(provider)
	loggedIn := false
	if !pending {
		for _, account := range accounts {
			if !account.NeedsReauth {
				loggedIn = true
				break
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"loggedIn":        loggedIn,
		"pending":         pending,
		"done":            !pending,
		"activeAccountId": activeID,
	})
	return true
}

func isPublicOAuthProvider(name string) bool {
	for _, id := range publicOAuthProviders {
		if id == name {
			return true
		}
	}
	return false
}

func (h *handler) oauthAuthStorePath() string {
	path := strings.TrimSpace(h.authStorePath)
	if path == "" && strings.TrimSpace(h.configPath) != "" {
		path = filepath.Join(filepath.Dir(h.configPath), "auth.json")
	}
	if path == "" {
		if resolved, err := config.ResolvePaths(config.PathOptions{}); err == nil {
			path = filepath.Join(resolved.Home, "auth.json")
		}
	}
	return path
}

func persistedOAuthProvider(name string) bool {
	return name == antigravity.AuthStoreProvider || name == cursor.ProviderID || name == "kimi" || name == "nous" || name == "github-copilot" || name == "xai" || name == "anthropic" || name == "command-code" || name == "kiro"
}

func (h *handler) oauthPublicAccounts(provider string) ([]antigravity.PublicAccount, string) {
	if !persistedOAuthProvider(provider) {
		return nil, ""
	}
	path := h.oauthAuthStorePath()
	if path == "" {
		return nil, ""
	}
	accounts, active, err := antigravity.LoadPublicAccountsFor(path, provider)
	if err != nil {
		return nil, ""
	}
	return accounts, active
}
