package server

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/Wibias/Benes/internal/codexappserver"
	"github.com/Wibias/Benes/internal/codexrestore"
	"github.com/Wibias/Benes/internal/config"
)

func (h *handler) serveSyncAPI(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/api/sync" {
		return false
	}
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
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
	payload := map[string]any{"ok": true, "changed": result.Changed, "message": result.Message}
	if result.Changed {
		payload["staleAppServerHint"] = codexappserver.StaleHint
	}
	writeJSON(w, http.StatusOK, payload)
	return true
}
