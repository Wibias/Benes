package server

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/Wibias/Benes/internal/grok"
)

/*
Grok projection API.

One model authority exists in Benes: the catalogued model set the listener routes. The Grok
config carries a projection of it, written by grok.Inject into the single managed region. There
is deliberately no Grok-side selection to store here: nothing in this file reads or writes a
per-Grok model list, so the block cannot drift away from the catalogue it came from.
*/
func (h *handler) serveGrokAPI(w http.ResponseWriter, r *http.Request) bool {
	switch r.URL.Path {
	case "/api/grok":
		return h.serveGrokStatus(w, r)
	case "/api/grok/apply":
		return h.serveGrokApply(w, r)
	default:
		return false
	}
}

// grokModels projects the listener's catalogue into what the Grok writer needs.
func (h *handler) grokModels() []grok.Model {
	models := make([]grok.Model, 0, len(h.catalogModels))
	for _, model := range h.catalogModels {
		if strings.TrimSpace(model.ID) == "" {
			continue
		}
		models = append(models, grok.Model{ID: model.ID, ContextWindow: model.Context.Tokens})
	}
	return models
}

// grokTarget is the address a Grok write should point at: the listener this caller reached.
// A Host Benes cannot parse falls back to the documented default bind.
func grokTarget(r *http.Request) (string, int) {
	host, portStr, err := net.SplitHostPort(r.Host)
	if err != nil {
		return "127.0.0.1", 23100
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 {
		return host, 23100
	}
	return host, port
}

// grokExpectedBaseURL is the address a write would use today, or "" when this request
// cannot say (the caller then judges the model set alone instead of guessing an address).
func grokExpectedBaseURL(r *http.Request) string {
	host, portStr, err := net.SplitHostPort(r.Host)
	if err != nil {
		return ""
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 {
		return ""
	}
	return fmt.Sprintf("http://%s:%d/v1", host, port)
}

func (h *handler) serveGrokStatus(w http.ResponseWriter, r *http.Request) bool {
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
	status := grok.ReadStatus(grok.InjectOptions{})
	writeJSON(w, http.StatusOK, grokStatusPayload{
		Registration:       grok.RegistrationFor(h.grokModels(), status, grokExpectedBaseURL(r)),
		LastAutomaticError: h.lastProjectionError(),
	})
	return true
}

// grokStatusPayload is the derived registration summary plus the last automatic reconcile
// failure, when there was one. That failure is a runtime fact about this process rather than
// something derivable from the config file, so it stays outside grok.Registration.
type grokStatusPayload struct {
	grok.Registration
	LastAutomaticError string `json:"lastAutomaticError,omitempty"`
}

func (h *handler) serveGrokApply(w http.ResponseWriter, r *http.Request) bool {
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
	host, port := grokTarget(r)
	result := h.writeGrokProjection(host, port)
	if result.OK && result.SkippedReason == "" {
		// An explicit apply that landed clears a recorded automatic failure.
		h.recordProjectionError("")
	}
	status := http.StatusOK
	if !result.OK {
		status = http.StatusInternalServerError
	}
	body := map[string]any{"ok": result.OK, "changed": result.Changed, "message": result.Message}
	if result.SkippedReason != "" {
		body["skippedReason"] = result.SkippedReason
	}
	writeJSON(w, status, body)
	return true
}
