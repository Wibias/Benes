package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func (h *handler) serveDashboard(w http.ResponseWriter, r *http.Request) bool {
	if h == nil || h.dashboard == nil {
		return false
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	path := r.URL.Path
	if strings.HasPrefix(path, "/v1/") || strings.HasPrefix(path, "/api/") {
		return false
	}
	h.dashboard.ServeHTTP(w, r)
	return true
}

func dashboardFileServer(dir string) http.Handler {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return nil
	}
	return http.FileServer(http.Dir(filepath.Clean(dir)))
}
