package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Wibias/Benes/internal/config"
)

func (h *handler) serveRequestPacingAPI(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/api/provider-request-pacing" {
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
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if strings.TrimSpace(h.configPath) == "" {
		writeJSON(w, http.StatusOK, map[string]any{"pacing": map[string]int{}})
		return true
	}
	disk, err := config.LoadDiskConfig(h.configPath, 0)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"pacing": map[string]int{}})
		return true
	}
	pacing := map[string]int{}
	for id, raw := range disk.Providers {
		if name != "" && id != name {
			continue
		}
		var rec struct {
			RequestPacingMs int `json:"requestPacingMs"`
		}
		if json.Unmarshal(raw, &rec) != nil {
			continue
		}
		if rec.RequestPacingMs > 0 {
			pacing[id] = rec.RequestPacingMs
		}
	}
	if name != "" && len(pacing) == 0 && !h.providerConfigured(name) {
		writeError(w, http.StatusNotFound, "unknown_provider", "unknown provider")
		return true
	}
	writeJSON(w, http.StatusOK, map[string]any{"pacing": pacing})
	return true
}
