package server

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Wibias/Benes/internal/codexrouting"
	"github.com/Wibias/Benes/internal/codexshim"
	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/startuphealth"
)

func (h *handler) serveStartupHealthAPI(w http.ResponseWriter, r *http.Request) bool {
	switch r.URL.Path {
	case "/api/startup-health", "/api/startup-action":
	default:
		return false
	}
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	if r.URL.Path == "/api/startup-health" {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeError(w, http.StatusNotFound, "not_found", "route not found")
			return true
		}
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			return true
		}
		writeJSON(w, http.StatusOK, h.startupHealthView())
		return true
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
	var body struct {
		Action string `json:"action"`
		Repair bool   `json:"repair"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "invalid JSON body")
		return true
	}
	if body.Action != "install-service" && body.Action != "install-shim" {
		writeError(w, http.StatusBadRequest, "invalid_body", "action must be install-service or install-shim")
		return true
	}
	if body.Action == "install-service" {
		writeError(w, http.StatusNotImplemented, "not_implemented", "install-service is not available on this Go data plane yet")
		return true
	}
	home := h.benesHome()
	if home == "" {
		writeError(w, http.StatusServiceUnavailable, "config_unreadable", "config path is required")
		return true
	}
	changed, message, err := codexshim.Install(home)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return true
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"action":  "install-shim",
		"repair":  body.Repair,
		"changed": changed,
		"message": message,
	})
	return true
}

func (h *handler) startupHealthView() startuphealth.Report {
	home := h.benesHome()
	shimInstalled := false
	shimHealthy := false
	if home != "" {
		status := codexshim.Status(home)
		if strings.Contains(status, "installed") && !strings.Contains(status, "not installed") {
			shimInstalled = true
			shimHealthy = true
		}
	}
	autostart := true
	if root := h.loadConfigRoot(); root != nil {
		if _, ok := root["codexAutoStart"]; ok {
			autostart = boolField(root, "codexAutoStart")
		}
	}
	platform := runtime.GOOS
	supported := platform == "windows" || platform == "linux"
	return startuphealth.Derive(startuphealth.Inputs{
		RoutingKind:      readCodexRoutingKind(),
		AutostartEnabled: autostart,
		ServiceInstalled: true,
		ServiceViable:    true,
		ServiceEnabled:   true,
		ServiceRunning:   true,
		ServiceSupported: supported,
		ShimInstalled:    shimInstalled,
		ShimHealthy:      shimHealthy,
		Platform:         platform,
	})
}

func readCodexRoutingKind() codexrouting.Kind {
	home, err := config.ResolveCodexHome(config.CodexHomeOptions{})
	if err != nil {
		return codexrouting.KindUnknown
	}
	raw, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil {
		if os.IsNotExist(err) {
			return codexrouting.KindNative
		}
		return codexrouting.KindUnknown
	}
	return codexrouting.Classify(string(raw))
}

func (h *handler) benesHome() string {
	if h != nil && strings.TrimSpace(h.configPath) != "" {
		return filepath.Dir(h.configPath)
	}
	if resolved, err := config.ResolvePaths(config.PathOptions{}); err == nil {
		return resolved.Home
	}
	return ""
}
