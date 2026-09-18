package server

import (
	"net/http"
	"runtime"
	"strconv"
	"strings"

	"github.com/Wibias/Benes/internal/sessions"
)

const requestLogMax = 2000

type requestLogEntry struct {
	ID         string `json:"id"`
	Timestamp  string `json:"timestamp"`
	Method     string `json:"method"`
	Path       string `json:"path"`
	Status     int    `json:"status"`
	DurationMs int64  `json:"durationMs"`
	SessionID  string `json:"sessionId,omitempty"`
}

type statusCapture struct {
	http.ResponseWriter
	status   int
	bytes    int64
	panicked bool
}

func (c *statusCapture) WriteHeader(code int) {
	if c.status == 0 {
		c.status = code
	}
	c.ResponseWriter.WriteHeader(code)
}

func (c *statusCapture) Write(p []byte) (int, error) {
	if c.status == 0 {
		c.status = http.StatusOK
	}
	n, err := c.ResponseWriter.Write(p)
	c.bytes += int64(n)
	return n, err
}

func (c *statusCapture) Flush() {
	if f, ok := c.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (c *statusCapture) Unwrap() http.ResponseWriter {
	return c.ResponseWriter
}

func finalObservedStatus(capture *statusCapture) int {
	if capture != nil && capture.status != 0 {
		return capture.status
	}
	if capture != nil && capture.panicked {
		return http.StatusInternalServerError
	}
	return http.StatusOK
}

func (h *handler) serveLogsAPI(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/api/logs" {
		return false
	}
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
	if h.diagnostics == nil {
		h.diagnostics = newRequestTelemetryState()
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	sessionID := strings.TrimSpace(r.URL.Query().Get("sessionId"))
	if sessionID != "" && !sessions.ValidSessionID(sessionID) {
		writeError(w, http.StatusBadRequest, "invalid_session_id", "session id is malformed")
		return true
	}
	entries := h.diagnostics.legacyList(r.URL.Query().Get("status"), sessionID, limit)
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		return true
	}
	writeJSON(w, http.StatusOK, entries)
	return true
}

func (h *handler) serveSystemMemoryAPI(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/api/system/memory" {
		return false
	}
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		return true
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"rss":       mem.Sys,
		"heapAlloc": mem.HeapAlloc,
		"heapSys":   mem.HeapSys,
		"gcPauseNs": mem.PauseTotalNs,
	})
	return true
}

func (h *handler) serveClaudeInboundDebugAPI(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/api/claude/inbound-debug" {
		return false
	}
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
	enabled := false
	if h.debug != nil {
		h.debug.mu.Lock()
		view := h.debug.view()
		h.debug.mu.Unlock()
		if v, ok := view["claude"].(bool); ok {
			enabled = v
		}
	}
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		return true
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": enabled, "entries": []any{}})
	return true
}
