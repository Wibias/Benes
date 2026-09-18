package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/requestpolicy"
)

func (h *handler) serveSettingsAPI(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/api/settings" {
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
		writeJSON(w, http.StatusOK, h.settingsView())
		return true
	case http.MethodPut:
		return h.serveSettingsPUT(w, r)
	default:
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
}

func (h *handler) settingsView() map[string]any {
	root := h.loadConfigRoot()
	if root == nil {
		root = map[string]any{}
	}
	port := 23100
	if v, ok := root["port"].(float64); ok && int(v) > 0 {
		port = int(v)
	}
	hostname, _ := root["hostname"].(string)
	if hostname == "" {
		hostname = "127.0.0.1"
	}
	streamMode, _ := root["streamMode"].(string)
	if streamMode == "" {
		streamMode = "auto"
	}
	budget := 256
	if v, ok := root["appOwnedMemoryBudgetMb"].(float64); ok {
		budget = int(v)
	}
	return map[string]any{
		"timeZone":                  time.Now().Location().String(),
		"codexAutoStart":            boolField(root, "codexAutoStart"),
		"port":                      port,
		"hostname":                  hostname,
		"streamMode":                streamMode,
		"appOwnedMemoryBudgetMb":    budget,
		"codexAccountPickerEnabled": boolField(root, "codexAccountPickerEnabled"),
		"requestPolicy":             requestpolicy.Current(),
		"codexRuntime": map[string]any{
			"path":           "codex",
			"version":        nil,
			"source":         "fallback",
			"newerAvailable": nil,
			"catalogClamp":   map[string]any{"active": false, "removedEfforts": []string{}, "runtimeVersion": nil},
			"warning":        nil,
		},
	}
}

func (h *handler) serveSettingsPUT(w http.ResponseWriter, r *http.Request) bool {
	var body map[string]json.RawMessage
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil || body == nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "settings body must be an object")
		return true
	}
	if body["codexAutoStart"] == nil && body["streamMode"] == nil && body["appOwnedMemoryBudgetMb"] == nil && body["codexAccountPickerEnabled"] == nil && body["requestPolicy"] == nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "provide codexAutoStart, streamMode, appOwnedMemoryBudgetMb, codexAccountPickerEnabled, or requestPolicy")
		return true
	}
	values := map[string]any{}
	deleteEmpty := []string{}
	var nextRequestPolicy *requestpolicy.Policy
	if raw, ok := body["codexAutoStart"]; ok {
		var enabled bool
		if json.Unmarshal(raw, &enabled) != nil {
			writeError(w, http.StatusBadRequest, "invalid_body", "codexAutoStart boolean is required")
			return true
		}
		values["codexAutoStart"] = enabled
	}
	if raw, ok := body["streamMode"]; ok {
		var mode string
		if json.Unmarshal(raw, &mode) != nil || (mode != "auto" && mode != "legacy-tee" && mode != "eager-relay") {
			writeError(w, http.StatusBadRequest, "invalid_body", "streamMode must be auto, legacy-tee, or eager-relay")
			return true
		}
		if mode == "auto" {
			values["streamMode"] = ""
			deleteEmpty = append(deleteEmpty, "streamMode")
		} else {
			values["streamMode"] = mode
		}
	}
	if raw, ok := body["appOwnedMemoryBudgetMb"]; ok {
		var budget int
		if json.Unmarshal(raw, &budget) != nil || budget < 64 || budget > 4096 {
			writeError(w, http.StatusBadRequest, "invalid_body", "appOwnedMemoryBudgetMb must be an integer from 64 to 4096")
			return true
		}
		values["appOwnedMemoryBudgetMb"] = budget
	}
	if raw, ok := body["codexAccountPickerEnabled"]; ok {
		var enabled bool
		if json.Unmarshal(raw, &enabled) != nil {
			writeError(w, http.StatusBadRequest, "invalid_body", "codexAccountPickerEnabled boolean is required")
			return true
		}
		values["codexAccountPickerEnabled"] = enabled
	}
	if raw, ok := body["requestPolicy"]; ok {
		policy, err := requestpolicy.Decode(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
			return true
		}
		nextRequestPolicy = &policy
		if policy.Empty() {
			deleteEmpty = append(deleteEmpty, "requestPolicy")
		} else {
			values["requestPolicy"] = policy
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
	empty := map[string]struct{}{}
	for _, key := range deleteEmpty {
		empty[key] = struct{}{}
	}
	for key, value := range values {
		if _, drop := empty[key]; drop {
			_ = tx.Delete(config.JSONPath(key))
			continue
		}
		payload, err := json.Marshal(value)
		if err != nil || tx.Set(config.JSONPath(key), payload) != nil {
			writeError(w, http.StatusInternalServerError, "config_unreadable", "settings could not be stored")
			return true
		}
	}
	for key := range empty {
		if _, exists := values[key]; exists {
			continue
		}
		_ = tx.Delete(config.JSONPath(key))
	}
	if _, err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "settings could not be stored")
		return true
	}
	if nextRequestPolicy != nil {
		_ = requestpolicy.Publish(*nextRequestPolicy)
	}
	view := h.settingsView()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                        true,
		"codexAutoStart":            view["codexAutoStart"],
		"streamMode":                view["streamMode"],
		"appOwnedMemoryBudgetMb":    view["appOwnedMemoryBudgetMb"],
		"codexAccountPickerEnabled": view["codexAccountPickerEnabled"],
		"requestPolicy":             view["requestPolicy"],
	})
	return true
}
