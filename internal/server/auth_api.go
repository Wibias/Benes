package server

import "net/http"

func (h *handler) serveAuthAPI(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/api/auth" {
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
	writeJSON(w, http.StatusOK, map[string]any{"loopback": true})
	return true
}
