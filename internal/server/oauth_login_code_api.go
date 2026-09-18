package server

import (
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/oauth/cursor"
	"github.com/Wibias/Benes/internal/providers/antigravity"
)

func (h *handler) serveOAuthLoginCodeAPI(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/api/oauth/login/code" {
		return false
	}
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
	var body struct {
		Provider string `json:"provider"`
		Input    string `json:"input"`
		Code     string `json:"code"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "invalid JSON body")
		return true
	}
	provider := strings.ToLower(strings.TrimSpace(body.Provider))
	if !isPublicOAuthProvider(provider) {
		writeError(w, http.StatusBadRequest, "unknown_oauth_provider", "unknown oauth provider")
		return true
	}
	if !loginImplemented(provider) {
		writeError(w, http.StatusNotImplemented, "not_implemented", "oauth login for this provider is not on the Go data plane yet")
		return true
	}
	callback := strings.TrimSpace(body.Input)
	if callback == "" {
		callback = strings.TrimSpace(body.Code)
	}
	if provider != cursor.ProviderID && !isDeviceOAuthProvider(provider) && callback == "" {
		writeError(w, http.StatusBadRequest, "invalid_body", "input is required")
		return true
	}
	home := h.benesHome()
	if home == "" {
		if resolved, err := config.ResolvePaths(config.PathOptions{}); err == nil {
			home = resolved.Home
		}
	}
	if home == "" {
		writeError(w, http.StatusServiceUnavailable, "config_unreadable", "config path is required")
		return true
	}
	if completeDeviceOAuthLogin(w, r, home, provider) {
		return true
	}
	if completeCallbackOAuthLogin(w, r, home, provider, callback) {
		return true
	}
	if provider == cursor.ProviderID {
		token, ok := ownerToken(w, provider)
		if !ok {
			return true
		}
		pending := cursor.PendingFile{Path: filepath.Join(home, cursor.PendingFileName)}
		completed, err := pending.Complete(r.Context())
		if err != nil {
			writeOAuthPublicError(w, http.StatusBadRequest, err)
			return true
		}
		if !persistOwnedAccount(w, provider, filepath.Join(home, "auth.json"), completed.Account, token) {
			return true
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":       true,
			"provider": provider,
			"id":       completed.Account.ID,
		})
		return true
	}
	token, ok := ownerToken(w, provider)
	if !ok {
		return true
	}
	pending := antigravity.PendingFile{Path: filepath.Join(home, "oauth-pending-google-antigravity.json")}
	completed, err := pending.Complete(r.Context(), http.DefaultClient, callback)
	if err != nil {
		writeOAuthPublicError(w, http.StatusBadRequest, err)
		return true
	}
	stored := antigravity.StoredAccount{ID: completed.Account.ID, Token: completed.Account.Token, Refresh: completed.Refresh, ProjectID: completed.Account.ProjectID}
	if !persistOwnedAccount(w, provider, filepath.Join(home, "auth.json"), stored, token) {
		return true
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"provider": provider,
		"project":  completed.Account.ProjectID,
	})
	return true
}
