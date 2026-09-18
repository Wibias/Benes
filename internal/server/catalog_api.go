package server

import "net/http"

func (h *handler) serveCatalogAPI(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/api/catalog" {
		return false
	}
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
	models := make([]map[string]any, 0, len(h.catalogModels))
	for _, model := range h.catalogModels {
		if model.ID == "" {
			continue
		}
		models = append(models, map[string]any{
			"id":               model.ID,
			"context":          model.Context.Tokens,
			"vision":           string(model.Vision),
			"reasoningEfforts": model.ReasoningEfforts,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": models})
	return true
}
