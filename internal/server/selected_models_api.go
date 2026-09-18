package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/Wibias/Benes/internal/config"
)

func (h *handler) serveSelectedModelsAPI(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/api/selected-models" {
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
		return h.serveSelectedModelsGET(w)
	case http.MethodPut:
		return h.serveSelectedModelsPUT(w, r)
	default:
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
}

func (h *handler) serveSelectedModelsGET(w http.ResponseWriter) bool {
	available := map[string][]string{}
	for _, model := range h.catalogModels {
		if model.ID == "" {
			continue
		}
		provider, id := splitNamespaced(model.ID)
		available[provider] = append(available[provider], id)
	}
	selected := map[string][]string{}
	root := h.loadConfigRoot()
	if root == nil {
		root = map[string]any{}
	}
	providers, _ := root["providers"].(map[string]any)

	for name, raw := range providers {
		rec, _ := raw.(map[string]any)
		list, _ := rec["selectedModels"].([]any)
		if len(list) == 0 {
			continue
		}
		ids := []string{}
		for _, item := range list {
			text, _ := item.(string)
			if strings.TrimSpace(text) != "" {
				ids = append(ids, text)
			}
		}
		if len(ids) > 0 {
			selected[name] = ids
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"selected":        selected,
		"available":       available,
		"liveModelCounts": map[string]int{},
	})
	return true
}

func (h *handler) serveSelectedModelsPUT(w http.ResponseWriter, r *http.Request) bool {
	var body struct {
		Provider string   `json:"provider"`
		Models   []string `json:"models"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "invalid JSON body")
		return true
	}
	provider := strings.TrimSpace(body.Provider)
	if provider == "" {
		writeError(w, http.StatusBadRequest, "invalid_body", "unknown provider")
		return true
	}
	if !h.providerConfigured(provider) {
		writeError(w, http.StatusNotFound, "unknown_provider", "unknown provider")
		return true
	}
	seen := map[string]struct{}{}
	models := []string{}
	for _, id := range body.Models {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		models = append(models, id)
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
	path := config.JSONPath("providers", provider, "selectedModels")
	if len(models) == 0 {
		_ = tx.Delete(path)
	} else {
		payload, err := json.Marshal(models)
		if err != nil || tx.Set(path, payload) != nil {
			writeError(w, http.StatusInternalServerError, "config_unreadable", "selected models could not be stored")
			return true
		}
	}
	if _, err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "selected models could not be stored")
		return true
	}
	h.markSelectedModelsCustom(provider)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "provider": provider, "selected": models})
	return true
}
