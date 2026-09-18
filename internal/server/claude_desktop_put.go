package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/Wibias/Benes/internal/config"
)

// errClaudeDesktopConfigPath reports a listener without a Benes config path to
// store desired state in.
var errClaudeDesktopConfigPath = errors.New("config path is required")

// Legacy `claudeDesktop` profile fields and their disposition under the runtime
// contract. The block is compatibility-only data: it is neither read as desired
// state nor projected into native configuration, and it is never exposed.
//
//   - model, assignments, defaults, version: unsupported historical fields.
//     They describe native model and family routing, which Claude Desktop's
//     supported local configuration cannot express.
//   - appliedFingerprint, appliedAt: forbidden as applied evidence. A Benes-side
//     receipt is not native state, and applied state is derived only from the
//     native document.
//   - apiKey, key, token: secret-bearing. Benes stores no secret for this
//     harness and never surfaces one.
//
// serveClaudeDesktopPUT therefore accepts exactly one desired field.
func (h *handler) serveClaudeDesktopPUT(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/api/claude-desktop" || r.Method != http.MethodPut {
		return false
	}
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	var body map[string]json.RawMessage
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil || body == nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "invalid JSON body")
		return true
	}
	if _, legacy := body["profile"]; legacy {
		writeError(w, http.StatusBadRequest, "unsupported_field",
			"Claude Desktop's supported local configuration exposes no model, family, or endpoint surface, so a stored profile is not supported desired state")
		return true
	}
	raw, ok := body["enabled"]
	if !ok || len(body) != 1 {
		writeError(w, http.StatusBadRequest, "invalid_body", "enabled must be the only field")
		return true
	}
	var enabled bool
	if json.Unmarshal(raw, &enabled) != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "enabled must be a boolean")
		return true
	}
	if err := h.storeClaudeDesktopEnabled(enabled); err != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "desired state could not be stored")
		return true
	}
	writeJSON(w, http.StatusOK, h.claudeDesktopStatus())
	return true
}

// storeClaudeDesktopEnabled persists desired enablement only.
func (h *handler) storeClaudeDesktopEnabled(enabled bool) error {
	if strings.TrimSpace(h.configPath) == "" {
		return errClaudeDesktopConfigPath
	}
	payload, err := json.Marshal(enabled)
	if err != nil {
		return err
	}
	tx, err := config.NewTransactionStore(h.configPath, 0).Begin()
	if err != nil {
		return err
	}
	if err := tx.Set(config.JSONPath("claudeDesktopEnabled"), payload); err != nil {
		return err
	}
	if _, err := tx.Commit(); err != nil {
		return err
	}
	return nil
}
