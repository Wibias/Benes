package server

import (
	"net/http"

	"github.com/Wibias/Benes/internal/catalog"
)

func (h *handler) serveContextCapsAPI(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/api/provider-context-caps" {
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
	root := h.loadConfigRoot()
	if root == nil {
		root = map[string]any{}
	}
	caps := map[string]any{}
	if raw, ok := root["providerContextCaps"].(map[string]any); ok {
		caps = raw
	}
	value := 0
	if v, ok := root["providerContextCap"].(float64); ok {
		value = int(v)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"cap":   catalog.ConservativeContextWindow,
		"value": value,
		"caps":  caps,
	})
	return true
}
