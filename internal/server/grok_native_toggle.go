package server

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/Wibias/Benes/internal/grok"
)

// Grok is reachable through the same Harness apply/disable lifecycle as every other native
// client: enabling writes the managed projection of the catalogue, disabling removes only
// that managed region. Neither direction consults a Grok-specific model list.
func (h *handler) serveGrokNativeToggle(w http.ResponseWriter, r *http.Request) bool {
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
	if !*body.Enabled {
		result := grok.Strip(grok.InjectOptions{})
		if result.OK && result.SkippedReason == "" {
			h.recordProjectionError("")
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": true, "clientId": "grok", "changed": result.OK && result.SkippedReason == "",
			"state": "absent", "desiredEnabled": false, "message": result.Message,
		})
		return true
	}
	host, port := grokTarget(r)
	result := h.writeGrokProjection(host, port)
	if result.OK && result.SkippedReason == "" {
		// Applying by hand clears a recorded automatic failure.
		h.recordProjectionError("")
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": result.OK, "clientId": "grok", "changed": result.Changed,
		"state": "current", "desiredEnabled": true, "message": result.Message,
	})
	return true
}
