package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/contextprojection"
)

func (h *handler) serveContextProjectionAPI(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/api/context-projection" {
		return false
	}
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			return true
		}
		writeJSON(w, http.StatusOK, map[string]any{"mode": h.contextProjectionAPIMode()})
		return true
	case http.MethodPut:
		h.serveContextProjectionPUT(w, r)
		return true
	default:
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
}

func (h *handler) contextProjectionAPIMode() string {
	mode := strings.TrimSpace(string(h.contextProjectionMode()))
	normalized, ok := normalizeContextProjectionMode(mode)
	if !ok {
		return string(contextprojection.ModeOff)
	}
	return normalized
}

func (h *handler) serveContextProjectionPUT(w http.ResponseWriter, r *http.Request) {
	if strings.TrimSpace(h.configPath) == "" {
		writeError(w, http.StatusServiceUnavailable, "config_unreadable", "config path is required")
		return
	}
	var body struct {
		Mode string `json:"mode"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "invalid JSON body")
		return
	}
	mode, ok := normalizeContextProjectionMode(body.Mode)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_mode", "mode must be off, shadow, duplicate, or recovery")
		return
	}
	payload, err := json.Marshal(mode)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "context projection could not be stored")
		return
	}
	if err := h.commitConfig(func(tx *config.Transaction) error {
		return tx.Set(config.JSONPath("contextProjection", "mode"), payload)
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "context projection could not be stored")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "mode": mode})
}

func normalizeContextProjectionMode(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", string(contextprojection.ModeOff):
		return string(contextprojection.ModeOff), true
	case string(contextprojection.ModeShadow):
		return string(contextprojection.ModeShadow), true
	case string(contextprojection.ModeDuplicate):
		return string(contextprojection.ModeDuplicate), true
	case string(contextprojection.ModeRecovery), string(contextprojection.ModeOn):
		return string(contextprojection.ModeRecovery), true
	default:
		return "", false
	}
}
