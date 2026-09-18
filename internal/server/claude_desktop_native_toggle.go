package server

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/Wibias/Benes/internal/claudedesktop"
)

// claudeDesktopNativeClient reports the Claude Desktop row on the shared
// native-integration surface.
//
// The shared vocabulary is limited to absent, current, and unsafe. A manageable
// host with nothing Benes can install reports absent, never current: enabling
// stores desired state and cannot by itself mean the native configuration was
// written. An unsupported host or an unmanageable configuration reports unsafe.
// The precise disposition lives in the canonical /api/claude-desktop/status.
func (h *handler) claudeDesktopNativeClient() map[string]any {
	status := h.claudeDesktopStatus()
	state := "absent"
	switch {
	case status.Applied:
		state = "current"
	case status.State == claudedesktop.StateUnsupportedHost, status.State == claudedesktop.StateConfigUnavailable:
		state = "unsafe"
	}
	return map[string]any{
		"clientId":       status.ClientID,
		"state":          state,
		"installed":      status.Installed,
		"configPath":     status.ConfigPath,
		"desiredEnabled": status.DesiredEnabled,
		"disableBlocked": nil,
	}
}

// serveClaudeDesktopNativeToggle stores desired enablement and then attempts the
// matching native transition, reporting only what actually happened.
func (h *handler) serveClaudeDesktopNativeToggle(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPut {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil || body.Enabled == nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "enabled must be a boolean")
		return true
	}
	previous := h.claudeDesktopDesiredEnabled()
	if err := h.storeClaudeDesktopEnabled(*body.Enabled); err != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "flag could not be stored")
		return true
	}
	input := h.claudeDesktopInput()
	var (
		result  claudedesktop.MutationResult
		refusal *claudedesktop.Refusal
	)
	if *body.Enabled {
		result, refusal = claudedesktop.Apply(input)
	} else {
		result, refusal = claudedesktop.Disable(input)
	}
	state := "absent"
	message := "Claude Desktop disabled; no Benes-owned native entry was present"
	switch {
	case result.Applied:
		state = "current"
		message = "Claude Desktop enabled and applied"
	case *body.Enabled && refusal != nil:
		message = "Claude Desktop enabled. No native configuration was written: " + refusal.Message
	case *body.Enabled:
		message = "Claude Desktop enabled"
	case result.Changed:
		message = "Claude Desktop disabled and its Benes-owned native entry was removed"
	}
	payload := map[string]any{
		"ok":             true,
		"clientId":       claudedesktop.ClientID,
		"changed":        previous != *body.Enabled || result.Changed,
		"state":          state,
		"desiredEnabled": *body.Enabled,
		"message":        message,
	}
	if refusal != nil {
		payload["reason"] = string(refusal.Code)
	}
	writeJSON(w, http.StatusOK, payload)
	return true
}
