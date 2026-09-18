package server

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/oauth/anthropic"
	"github.com/Wibias/Benes/internal/oauth/commandcode"
	"github.com/Wibias/Benes/internal/oauth/cursor"
	"github.com/Wibias/Benes/internal/oauth/githubcopilot"
	"github.com/Wibias/Benes/internal/oauth/kimi"
	"github.com/Wibias/Benes/internal/oauth/nous"
	"github.com/Wibias/Benes/internal/oauth/xai"
)

func (h *handler) serveOAuthLoginCancelAPI(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/api/oauth/login/cancel" {
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
	abortOAuthLogin(provider)
	home := h.benesHome()
	if home == "" {
		if resolved, err := config.ResolvePaths(config.PathOptions{}); err == nil {
			home = resolved.Home
		}
	}
	removeOAuthPendingFile(home, provider)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "provider": provider})
	return true
}

func removeOAuthPendingFile(home, provider string) {
	if home == "" {
		return
	}
	switch provider {
	case cursor.ProviderID:
		_ = os.Remove(filepath.Join(home, cursor.PendingFileName))
	case kimi.ProviderID:
		_ = os.Remove(filepath.Join(home, kimi.PendingFileName))
	case nous.ProviderID:
		_ = os.Remove(filepath.Join(home, nous.PendingFileName))
	case githubcopilot.ProviderID:
		_ = os.Remove(filepath.Join(home, githubcopilot.PendingFileName))
	case xai.ProviderID:
		_ = os.Remove(filepath.Join(home, xai.PendingFileName))
	case anthropic.ProviderID:
		_ = os.Remove(filepath.Join(home, anthropic.PendingFileName))
	case commandcode.ProviderID:
		_ = os.Remove(filepath.Join(home, commandcode.PendingFileName))
	default:
		_ = os.Remove(filepath.Join(home, "oauth-pending-google-antigravity.json"))
	}
}
