package server

import (
	"net/http"

	"github.com/Wibias/Benes/internal/claudedesktop"
)

// claudeDesktopInput assembles the evidence the Claude Desktop runtime contract
// evaluates. Native locations belong to internal/claudedesktop, so no path is
// derived here. h.host is the platform this listener manages; it is the running
// host outside tests, and the contract itself is host specific.
func (h *handler) claudeDesktopInput() claudedesktop.Input {
	return claudedesktop.Input{
		Host:           h.host,
		Home:           h.home,
		DesiredEnabled: h.claudeDesktopDesiredEnabled(),
		// Benes ships no Claude Desktop MCP runtime, so there is no projection
		// to bind. The contract reports that absence rather than a placeholder.
		Managed: claudedesktop.ManagedNativeProjection(),
	}
}

// claudeDesktopDesiredEnabled reads desired product state. It is never evidence
// of applied native state, and a stored legacy profile never implies it.
func (h *handler) claudeDesktopDesiredEnabled() bool {
	return boolField(h.loadConfigRoot(), "claudeDesktopEnabled")
}

func (h *handler) claudeDesktopStatus() claudedesktop.Status {
	return claudedesktop.Evaluate(h.claudeDesktopInput())
}

// serveClaudeDesktopAPI serves the single canonical Claude Desktop runtime
// response, plus the desired-state write and the apply/disable lifecycle.
func (h *handler) serveClaudeDesktopAPI(w http.ResponseWriter, r *http.Request) bool {
	switch r.URL.Path {
	case "/api/claude-desktop", "/api/claude-desktop/status", "/api/claude-desktop/apply", "/api/claude-desktop/disable":
	default:
		return false
	}
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	if r.URL.Path == "/api/claude-desktop/apply" || r.URL.Path == "/api/claude-desktop/disable" {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusNotFound, "not_found", "route not found")
			return true
		}
		var (
			result  claudedesktop.MutationResult
			refusal *claudedesktop.Refusal
		)
		if r.URL.Path == "/api/claude-desktop/apply" {
			result, refusal = claudedesktop.Apply(h.claudeDesktopInput())
		} else {
			result, refusal = claudedesktop.Disable(h.claudeDesktopInput())
		}
		if refusal != nil {
			writeError(w, http.StatusConflict, string(refusal.Code), refusal.Message)
			return true
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":              true,
			"clientId":        result.ClientID,
			"changed":         result.Changed,
			"applied":         result.Applied,
			"configPath":      result.ConfigPath,
			"fingerprint":     result.Fingerprint,
			"restartRequired": result.RestartRequired,
		})
		return true
	}
	if r.URL.Path == "/api/claude-desktop" && r.Method == http.MethodPut {
		return h.serveClaudeDesktopPUT(w, r)
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
	writeJSON(w, http.StatusOK, h.claudeDesktopStatus())
	return true
}
