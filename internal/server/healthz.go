package server

import "net/http"

func (h *handler) serveHealthz(w http.ResponseWriter, r *http.Request) bool {
	switch r.URL.Path {
	case "/healthz", "/readyz":
	default:
		return false
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		if r.URL.Path == "/readyz" {
			_, _ = w.Write([]byte(`{"ok":true,"status":"ready"}`))
		} else {
			_, _ = w.Write([]byte(`{"ok":true}`))
		}
	}
	return true
}
