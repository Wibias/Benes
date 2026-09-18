package server

import "net/http"

func (h *handler) serveGitHubStarAPI(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/api/github/star" {
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
		writeJSON(w, http.StatusOK, map[string]any{
			"starred": false,
			"unknown": true,
		})
		return true
	case http.MethodPost:
		writeError(w, http.StatusForbidden, "consent_required", "starring uses the user's GitHub identity and requires an explicit dashboard session")
		return true
	default:
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
}
