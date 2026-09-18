package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Wibias/Benes/internal/config"
)

func (h *handler) serveFabricSettingsAPI(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/api/fabric-settings" {
		return false
	}
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		enabled, err := h.fabricEnabledState()
		if err != nil {
			writeFabricConfigError(w)
			return true
		}
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			return true
		}
		writeJSON(w, http.StatusOK, map[string]any{"enabled": enabled})
		return true
	case http.MethodPut:
		h.serveFabricSettingsPUT(w, r)
		return true
	default:
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
}

func (h *handler) serveFabricSettingsPUT(w http.ResponseWriter, r *http.Request) {
	if strings.TrimSpace(h.configPath) == "" {
		writeError(w, http.StatusServiceUnavailable, "config_unreadable", "config path is required")
		return
	}
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if err := decodeExactJSON(r.Body, 1<<10, &body); err != nil {
		status, code, message, _ := fabricJSONError(err)
		writeError(w, status, code, message)
		return
	}
	if body.Enabled == nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "enabled must be a boolean")
		return
	}
	payload, err := json.Marshal(*body.Enabled)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "fabric settings could not be stored")
		return
	}
	if err := h.commitConfig(func(tx *config.Transaction) error {
		return tx.Set(config.JSONPath("fabric", "enabled"), payload)
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "fabric settings could not be stored")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "enabled": *body.Enabled})
}
