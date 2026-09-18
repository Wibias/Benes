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

func (h *handler) serveOAuthLoginAPI(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/api/oauth/login" {
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
	if !beginOAuthLogin(w, provider) {
		return true
	}
	home := h.benesHome()
	if home == "" {
		if resolved, err := config.ResolvePaths(config.PathOptions{}); err == nil {
			home = resolved.Home
		}
	}
	if home == "" {
		abortOAuthLogin(provider)
		writeError(w, http.StatusServiceUnavailable, "config_unreadable", "config path is required")
		return true
	}
	if startDeviceOAuthLogin(w, r, home, provider) {
		return true
	}
	if startCallbackOAuthLogin(w, r, home, provider) {
		return true
	}
	if provider == cursor.ProviderID {
		login, err := cursor.NewPendingLogin()
		if err != nil {
			abortOAuthLogin(provider)
			writeOAuthPublicError(w, http.StatusInternalServerError, err)
			return true
		}
		pending := cursor.PendingFile{Path: filepath.Join(home, cursor.PendingFileName)}
		if err := pending.Save(login); err != nil {
			abortOAuthLogin(provider)
			writeOAuthPublicError(w, http.StatusInternalServerError, err)
			return true
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":       true,
			"provider": provider,
			"url":      login.AuthURL(),
		})
		return true
	}
	login, err := antigravity.NewPendingLogin()
	if err != nil {
		abortOAuthLogin(provider)
		writeOAuthPublicError(w, http.StatusInternalServerError, err)
		return true
	}
	authURL, err := login.AuthURL()
	if err != nil {
		abortOAuthLogin(provider)
		writeOAuthPublicError(w, http.StatusInternalServerError, err)
		return true
	}
	pending := antigravity.PendingFile{Path: filepath.Join(home, "oauth-pending-google-antigravity.json")}
	if err := pending.Save(login); err != nil {
		abortOAuthLogin(provider)
		writeOAuthPublicError(w, http.StatusInternalServerError, err)
		return true
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"provider": provider,
		"url":      authURL,
	})
	return true
}
