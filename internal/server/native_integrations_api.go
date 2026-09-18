package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/grok"
)

func (h *handler) serveNativeIntegrationsAPI(w http.ResponseWriter, r *http.Request) bool {
	switch r.URL.Path {
	case "/api/native-integrations", "/api/native-integrations/claude", "/api/native-integrations/codex", "/api/native-integrations/grok", "/api/native-integrations/claude-desktop":
	default:
		return false
	}
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	if r.URL.Path == "/api/native-integrations" {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeError(w, http.StatusNotFound, "not_found", "route not found")
			return true
		}
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			return true
		}
		writeJSON(w, http.StatusOK, map[string]any{"clients": h.nativeIntegrationClients()})
		return true
	}
	if r.URL.Path == "/api/native-integrations/codex" {
		return h.serveCodexNativeToggle(w, r)
	}
	if r.URL.Path == "/api/native-integrations/grok" {
		return h.serveGrokNativeToggle(w, r)
	}
	if r.URL.Path == "/api/native-integrations/claude-desktop" {
		return h.serveClaudeDesktopNativeToggle(w, r)
	}

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
	root := h.loadConfigRoot()
	if root == nil {
		root = map[string]any{}
	}
	block := copyMap(root["claudeCode"])
	current := true
	if v, ok := block["enabled"].(bool); ok {
		current = v
	}
	if current == *body.Enabled {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": true, "clientId": "claude", "changed": false,
			"state":          map[bool]string{true: "current", false: "absent"}[*body.Enabled],
			"desiredEnabled": *body.Enabled,
			"message":        map[bool]string{true: "Claude inbound is already on", false: "Claude inbound is already off"}[*body.Enabled],
		})
		return true
	}
	block["enabled"] = *body.Enabled
	if strings.TrimSpace(stringField(block, "authModeMigratedAt")) == "" {
		block["authModeMigratedAt"] = time.Now().UTC().Format(time.RFC3339)
	}
	if strings.TrimSpace(h.configPath) == "" {
		writeError(w, http.StatusServiceUnavailable, "config_unreadable", "config path is required")
		return true
	}
	payload, err := json.Marshal(block)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "config could not be stored")
		return true
	}
	tx, err := config.NewTransactionStore(h.configPath, 0).Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "config could not be opened")
		return true
	}
	if err := tx.Set(config.JSONPath("claudeCode"), payload); err != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "config could not be stored")
		return true
	}
	if _, err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "config could not be stored")
		return true
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "clientId": "claude", "changed": true,
		"state":          map[bool]string{true: "current", false: "absent"}[*body.Enabled],
		"desiredEnabled": *body.Enabled,
		"message":        map[bool]string{true: "Claude inbound enabled", false: "Claude inbound disabled"}[*body.Enabled],
	})
	return true
}

func (h *handler) nativeIntegrationClients() []map[string]any {
	root := h.loadConfigRoot()
	if root == nil {
		root = map[string]any{}
	}
	claude := copyMap(root["claudeCode"])
	claudeOn := true
	if v, ok := claude["enabled"].(bool); ok {
		claudeOn = v
	}
	return []map[string]any{
		nativeClient("claude", claudeOn, h.configPath),
		h.grokNativeClient(),
		nativeClient("codex", true, h.codexHome),
		h.claudeDesktopNativeClient(),
	}
}

// grokNativeClient reports Grok Build's real state instead of a fixed stub.
//
// "installed" is whether Grok Build is set up on this machine (its home directory
// exists); "state" is whether Benes has written its managed block into that client's
// config. Keeping the two apart is what lets the Harness offer Apply when Grok is present
// but unwritten, and Disable once the block exists — the same lifecycle every other
// Harness uses. The block itself is always a projection of the listener's catalogue.
func (h *handler) grokNativeClient() map[string]any {
	opts := grok.InjectOptions{}
	status := grok.ReadStatus(opts)
	installed := grok.HomeExists(opts)
	configPath := ""
	if installed {
		configPath = status.ConfigPath
	}
	state := "absent"
	if status.Present {
		state = "current"
	}
	return map[string]any{
		"clientId":       "grok",
		"state":          state,
		"installed":      installed,
		"configPath":     configPath,
		"desiredEnabled": status.Present,
		"disableBlocked": nil,
	}
}

func nativeClient(id string, enabled bool, path string) map[string]any {
	state := "absent"
	if enabled {
		state = "current"
	}
	return map[string]any{
		"clientId":       id,
		"state":          state,
		"installed":      enabled,
		"configPath":     path,
		"desiredEnabled": enabled,
		"disableBlocked": nil,
	}
}
