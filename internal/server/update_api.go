package server

import "net/http"

func (h *handler) serveUpdateAPI(w http.ResponseWriter, r *http.Request) bool {
	switch r.URL.Path {
	case "/api/update/check", "/api/update/run", "/api/update/status", "/api/update/badge":
	default:
		return false
	}
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	if r.URL.Path == "/api/update/run" && r.Method != http.MethodPost && r.Method != http.MethodHead {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
	if r.URL.Path != "/api/update/run" && r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/update/badge" {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusGone)
		}
		return true
	}
	if r.URL.Path == "/api/update/badge" {
		writeJSON(w, http.StatusOK, map[string]any{
			"updateAvailable": false,
			"currentVersion":  "",
			"latestVersion":   nil,
			"channel":         "latest",
			"canUpdate":       false,
			"unknown":         false,
		})
		return true
	}
	writeJSON(w, http.StatusGone, map[string]any{
		"error":  "self-update retired",
		"policy": "Benes does not self-update in-process. Use your package manager or rebuild from source.",
	})
	return true
}
