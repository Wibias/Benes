package server

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"github.com/Wibias/Benes/internal/catalog"
	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/modeldiscovery"
)

func (h *handler) serveModelsAPI(w http.ResponseWriter, r *http.Request) bool {
	switch r.URL.Path {
	case "/api/models":
		return h.serveManagementModels(w, r)
	case "/api/models/probe":
		return h.serveModelProbeAPI(w, r)
	case "/api/disabled-models":
		return h.serveDisabledModels(w, r)
	default:
		return false
	}
}

func (h *handler) serveManagementModels(w http.ResponseWriter, r *http.Request) bool {
	if !isLoopbackRequestHost(r.Host) {
		return false
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
	disabled := h.loadDisabledModels()
	hidden := map[string]struct{}{}
	for _, id := range disabled {
		hidden[id] = struct{}{}
	}
	seen := map[string]struct{}{}
	rows := make([]map[string]any, 0, len(h.catalogModels))
	for _, model := range h.catalogModels {
		if model.ID == "" {
			continue
		}
		provider, id := splitNamespaced(model.ID)
		_, isDisabled := hidden[model.ID]
		if !isDisabled {
			_, isDisabled = hidden[id]
		}
		row := map[string]any{
			"provider":   provider,
			"id":         id,
			"namespaced": model.ID,
			"disabled":   isDisabled,
		}
		if model.AdvertisedTokens > 0 {
			row["contextWindow"] = model.AdvertisedTokens
		} else if model.Context.Tokens > 0 && model.Context.Source != catalog.ContextConservativeDefault && model.Context.Source != catalog.ContextOperator {
			row["contextWindow"] = model.Context.Tokens
		}
		if model.StandardTokens > 0 {
			row["standardContextWindow"] = model.StandardTokens
		}
		rows = append(rows, row)
		seen[model.ID] = struct{}{}
		seen[id] = struct{}{}
	}
	rows = append(rows, h.shownArrivalModelRows(hidden, seen)...)
	writeJSON(w, http.StatusOK, rows)
	return true
}

func (h *handler) shownArrivalModelRows(hidden, seen map[string]struct{}) []map[string]any {
	if h == nil || strings.TrimSpace(h.configPath) == "" {
		return nil
	}
	disk, err := config.LoadDiskConfig(h.configPath, 0)
	if err != nil {
		return nil
	}
	snap := modeldiscovery.DecodeConfig(disk.Raw)
	out := make([]map[string]any, 0)
	for _, catalog := range modeldiscovery.CatalogsFromConfig(disk.Providers, disk.Raw, snap) {
		for _, id := range snap.Shown(catalog.ID) {
			namespaced := modeldiscovery.CatalogID(catalog.ID, id)
			if _, ok := seen[namespaced]; ok {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			_, isDisabled := hidden[namespaced]
			if !isDisabled {
				_, isDisabled = hidden[id]
			}
			out = append(out, map[string]any{
				"provider":   catalog.ID,
				"id":         id,
				"namespaced": namespaced,
				"disabled":   isDisabled,
			})
			seen[namespaced] = struct{}{}
			seen[id] = struct{}{}
		}
	}
	return out
}

func (h *handler) serveDisabledModels(w http.ResponseWriter, r *http.Request) bool {
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	if r.Method != http.MethodPut {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
	var body struct {
		Models []string `json:"models"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "invalid JSON body")
		return true
	}
	seen := map[string]struct{}{}
	disabled := make([]string, 0, len(body.Models))
	for _, id := range body.Models {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		disabled = append(disabled, id)
	}
	sort.Strings(disabled)
	if len(disabled) > 5000 {
		writeError(w, http.StatusBadRequest, "invalid_body", "disabled list is too large")
		return true
	}
	if strings.TrimSpace(h.configPath) == "" {
		writeError(w, http.StatusServiceUnavailable, "config_unreadable", "config path is required")
		return true
	}
	payload, err := json.Marshal(disabled)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "disabled models could not be stored")
		return true
	}
	tx, err := config.NewTransactionStore(h.configPath, 0).Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "config could not be opened")
		return true
	}
	if err := tx.Set(config.JSONPath("disabledModels"), payload); err != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "disabled models could not be stored")
		return true
	}
	if _, err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "disabled models could not be stored")
		return true
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "models": disabled})
	return true
}

func (h *handler) loadDisabledModels() []string {
	if h == nil || strings.TrimSpace(h.configPath) == "" {
		return nil
	}
	disk, err := config.LoadDiskConfig(h.configPath, 0)
	if err != nil {
		return nil
	}
	var root map[string]json.RawMessage
	if json.Unmarshal(disk.Raw, &root) != nil {
		return nil
	}
	raw, ok := root["disabledModels"]
	if !ok {
		return nil
	}
	var disabled []string
	if json.Unmarshal(raw, &disabled) != nil {
		return nil
	}
	return disabled
}

func splitNamespaced(id string) (provider, model string) {
	i := strings.Index(id, "/")
	if i <= 0 || i == len(id)-1 {
		return id, id
	}
	return id[:i], id[i+1:]
}
