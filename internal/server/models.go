package server

import (
	"net/http"
)

const modelsPath = "/v1/models"

func (h *handler) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return
	}
	payload, _ := h.catalogPayload()
	if h.catalogMaxBytes > 0 && len(payload) > h.catalogMaxBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "catalog_too_large", "catalog projection exceeds the configured byte bound")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(payload)
}
