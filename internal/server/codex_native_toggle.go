package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/Wibias/Benes/internal/codexrestore"
	"github.com/Wibias/Benes/internal/config"
)

func (h *handler) serveCodexNativeToggle(w http.ResponseWriter, r *http.Request) bool {
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
	home := strings.TrimSpace(h.codexHome)
	if home == "" {
		resolved, err := config.ResolveCodexHome(config.CodexHomeOptions{})
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "codex_home_unreadable", err.Error())
			return true
		}
		home = resolved
	}
	if *body.Enabled {
		host, portStr, err := net.SplitHostPort(r.Host)
		if err != nil {
			host = "127.0.0.1"
			portStr = "23100"
		}
		switch strings.ToLower(strings.TrimSpace(host)) {
		case "", "0.0.0.0", "::", "[::]":
			host = "127.0.0.1"
		}
		port, conv := strconv.Atoi(portStr)
		if conv != nil || port <= 0 {
			port = 23100
		}
		result, err := codexrestore.Inject(home, fmt.Sprintf("http://%s:%d/v1", host, port))
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return true
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": true, "clientId": "codex", "changed": result.Changed,
			"state": "current", "desiredEnabled": true, "message": result.Message,
		})
		return true
	}
	result, err := codexrestore.Restore(home)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return true
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "clientId": "codex", "changed": result.ConfigRestored || result.Stripped,
		"state": "absent", "desiredEnabled": false, "message": result.Message,
	})
	return true
}
