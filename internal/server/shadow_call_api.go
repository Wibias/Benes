package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/Wibias/Benes/internal/config"
)

func (h *handler) serveShadowCallAPI(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/api/shadow-call-settings" {
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
		writeJSON(w, http.StatusOK, h.shadowCallView())
		return true
	case http.MethodPut:
		return h.serveShadowCallPUT(w, r)
	default:
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
}

func (h *handler) shadowCallView() map[string]any {
	root := h.loadConfigRoot()
	if root == nil {
		root = map[string]any{}
	}
	sci := copyMap(root["shadowCallIntercept"])
	enabled, _ := sci["enabled"].(bool)
	model, _ := sci["model"].(string)
	sources := []string{}
	if raw, ok := sci["sourceModels"].([]any); ok {
		for _, item := range raw {
			text, _ := item.(string)
			if strings.TrimSpace(text) != "" {
				sources = append(sources, text)
			}
		}
	}
	return map[string]any{"enabled": enabled, "model": model, "sourceModels": sources}
}

func (h *handler) serveShadowCallPUT(w http.ResponseWriter, r *http.Request) bool {
	var body map[string]json.RawMessage
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil || body == nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "body must be a JSON object")
		return true
	}
	root := h.loadConfigRoot()
	if root == nil {
		root = map[string]any{}
	}
	sci := copyMap(root["shadowCallIntercept"])
	if raw, ok := body["enabled"]; ok {
		var enabled bool
		if json.Unmarshal(raw, &enabled) != nil {
			writeError(w, http.StatusBadRequest, "invalid_body", "enabled must be a boolean")
			return true
		}
		sci["enabled"] = enabled
	}
	if raw, ok := body["model"]; ok {
		var model string
		if json.Unmarshal(raw, &model) != nil {
			writeError(w, http.StatusBadRequest, "invalid_body", "model must be a string")
			return true
		}
		if model == "" {
			delete(sci, "model")
		} else {
			sci["model"] = model
		}
	}
	if strings.TrimSpace(h.configPath) == "" {
		writeError(w, http.StatusServiceUnavailable, "config_unreadable", "config path is required")
		return true
	}
	tx, err := config.NewTransactionStore(h.configPath, 0).Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "config could not be opened")
		return true
	}
	payload, err := json.Marshal(sci)
	if err != nil || tx.Set(config.JSONPath("shadowCallIntercept"), payload) != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "shadow-call settings could not be stored")
		return true
	}
	if _, err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "shadow-call settings could not be stored")
		return true
	}
	view := h.shadowCallView()
	view["ok"] = true
	writeJSON(w, http.StatusOK, view)
	return true
}
