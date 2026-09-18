package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/Wibias/Benes/internal/codexfeatures"
	"github.com/Wibias/Benes/internal/config"
)

func (h *handler) serveV2API(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/api/v2" {
		return false
	}
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	if r.Method == http.MethodPut {
		return h.serveV2PUT(w, r)
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		return true
	}
	h.writeV2Status(w)
	return true
}

func (h *handler) serveV2PUT(w http.ResponseWriter, r *http.Request) bool {
	var body struct {
		Enabled               *bool   `json:"enabled"`
		MultiAgentMode        *string `json:"multiAgentMode"`
		KeepNativeChatGptOnV1 *bool   `json:"keepNativeChatGptOnV1"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "invalid JSON body")
		return true
	}
	if body.Enabled != nil {
		home := h.resolveCodexHome()
		enabled, _ := codexfeatures.Enabled(home)
		if enabled != *body.Enabled {
			if err := codexfeatures.Toggle(*body.Enabled); err != nil {
				writeError(w, http.StatusInternalServerError, "v2_toggle_failed", err.Error())
				return true
			}
		}
	}
	if body.MultiAgentMode != nil || body.KeepNativeChatGptOnV1 != nil {
		if strings.TrimSpace(h.configPath) == "" {
			writeError(w, http.StatusServiceUnavailable, "config_unreadable", "config path is required")
			return true
		}
		tx, err := config.NewTransactionStore(h.configPath, 0).Begin()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "config_unreadable", "config could not be opened")
			return true
		}
		if body.MultiAgentMode != nil {
			mode := strings.ToLower(strings.TrimSpace(*body.MultiAgentMode))
			if mode != "v1" && mode != "default" && mode != "v2" {
				writeError(w, http.StatusBadRequest, "invalid_body", "multiAgentMode must be v1, default, or v2")
				return true
			}
			if mode == "default" {
				if err := tx.Delete(config.JSONPath("multiAgentMode")); err != nil {
					writeError(w, http.StatusInternalServerError, "config_unreadable", "mode could not be stored")
					return true
				}
			} else {
				raw, err := json.Marshal(mode)
				if err != nil {
					writeError(w, http.StatusInternalServerError, "config_unreadable", "mode could not be stored")
					return true
				}
				if err := tx.Set(config.JSONPath("multiAgentMode"), raw); err != nil {
					writeError(w, http.StatusInternalServerError, "config_unreadable", "mode could not be stored")
					return true
				}
			}
		}
		if body.KeepNativeChatGptOnV1 != nil {
			if *body.KeepNativeChatGptOnV1 {
				raw, err := json.Marshal(true)
				if err != nil {
					writeError(w, http.StatusInternalServerError, "config_unreadable", "keepNativeChatGptOnV1 could not be stored")
					return true
				}
				if err := tx.Set(config.JSONPath("keepNativeChatGptOnV1"), raw); err != nil {
					writeError(w, http.StatusInternalServerError, "config_unreadable", "keepNativeChatGptOnV1 could not be stored")
					return true
				}
			} else if err := tx.Delete(config.JSONPath("keepNativeChatGptOnV1")); err != nil {
				writeError(w, http.StatusInternalServerError, "config_unreadable", "keepNativeChatGptOnV1 could not be stored")
				return true
			}
		}
		if _, err := tx.Commit(); err != nil {
			writeError(w, http.StatusInternalServerError, "config_unreadable", "config could not be stored")
			return true
		}
	}
	h.writeV2Status(w)
	return true
}

func (h *handler) writeV2Status(w http.ResponseWriter) {
	home := h.resolveCodexHome()
	enabled, _ := codexfeatures.Enabled(home)
	root := h.loadConfigRoot()
	if root == nil {
		root = map[string]any{}
	}
	mode := stringField(root, "multiAgentMode")
	if mode != "v1" && mode != "v2" {
		mode = "default"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":                             enabled,
		"multiAgentMode":                      mode,
		"keepNativeChatGptOnV1":               boolField(root, "keepNativeChatGptOnV1"),
		"agentsMaxThreadsConflict":            false,
		"maxConcurrentThreadsPerSession":      nil,
		"agentsEnabled":                       nil,
		"agentsMaxDepth":                      nil,
		"subagentDeveloperInstructions":       nil,
		"multiAgentModeHintText":              nil,
		"agentsMaxDepthAppliesWhenV2Disabled": !enabled,
	})
}

func (h *handler) resolveCodexHome() string {
	home := strings.TrimSpace(h.codexHome)
	if home != "" {
		return home
	}
	if resolved, err := config.ResolveCodexHome(config.CodexHomeOptions{}); err == nil {
		return resolved
	}
	return ""
}
