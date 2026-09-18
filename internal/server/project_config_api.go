package server

import (
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/Wibias/Benes/internal/config"
)

func (h *handler) serveProjectConfigAPI(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/api/diagnostics/project-config" {
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
	home := strings.TrimSpace(h.codexHome)
	if home == "" {
		if resolved, err := config.ResolveCodexHome(config.CodexHomeOptions{}); err == nil {
			home = resolved
		}
	}
	warnings := []string{}
	if home != "" {
		_ = filepath.WalkDir(home, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(strings.ToLower(d.Name()), ".toml") {
				return nil
			}
			raw, readErr := os.ReadFile(path)
			if readErr != nil {
				return nil
			}
			if strings.Contains(string(raw), "model_fallback") {
				rel := path
				if trimmed, err := filepath.Rel(home, path); err == nil {
					rel = trimmed
				}
				warnings = append(warnings, rel)
			}
			return nil
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"warnings": warnings,
		"grouped":  map[string]any{"model_fallback": warnings},
	})
	return true
}
