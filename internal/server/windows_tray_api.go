package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func (h *handler) serveWindowsTrayAPI(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/api/windows-tray" {
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
	if runtime.GOOS != "windows" {
		writeJSON(w, http.StatusOK, map[string]any{
			"supported": false,
			"installed": false,
			"running":   false,
			"stale":     false,
			"summary":   "unsupported on " + runtime.GOOS,
		})
		return true
	}
	home := h.benesHome()
	raw, err := os.ReadFile(filepath.Join(home, "tray-state.json"))
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"supported": true,
			"installed": false,
			"running":   false,
			"stale":     false,
			"summary":   "not installed",
		})
		return true
	}
	var state struct {
		RunValue string `json:"runValue"`
	}
	if json.Unmarshal(raw, &state) != nil || strings.TrimSpace(state.RunValue) == "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"supported": true,
			"installed": false,
			"running":   false,
			"stale":     false,
			"summary":   "invalid tray-state.json",
		})
		return true
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"supported": true,
		"installed": true,
		"running":   false,
		"stale":     false,
		"summary":   "installed",
		"runValue":  state.RunValue,
	})
	return true
}
