package server

import (
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	debugMaxLines     = 2000
	debugMaxLineBytes = 16 * 1024
	debugDefaultLimit = 500
)

type debugLogEntry struct {
	Seq  int    `json:"seq"`
	At   int64  `json:"at"`
	Line string `json:"line"`
}

type debugState struct {
	mu        sync.Mutex
	debug     *bool
	usage     *bool
	injection *bool
	claude    *bool
	logs      []debugLogEntry
	usageLogs []debugLogEntry
	nextSeq   int
	nextUsage int
}

func newDebugState() *debugState {
	return &debugState{nextSeq: 1, nextUsage: 1}
}

func envFlag(name string) bool {
	return os.Getenv(name) == "1"
}

func (s *debugState) view() map[string]any {
	envDebug := envFlag("BENES_DEBUG") || envFlag("BENES_DEBUG_FRAMES")
	envUsage := envFlag("BENES_USAGE_DEBUG")
	envInjection := envFlag("BENES_INJECTION_DEBUG")
	envClaude := envFlag("BENES_CLAUDE_DEBUG")
	enabled := envDebug
	if s.debug != nil {
		enabled = *s.debug
	}
	usage := envUsage
	if s.usage != nil {
		usage = *s.usage
	}
	injection := envInjection
	if s.injection != nil {
		injection = *s.injection
	}
	claude := envClaude
	if s.claude != nil {
		claude = *s.claude
	}
	return map[string]any{
		"enabled":         enabled,
		"usage":           usage,
		"injection":       injection,
		"claude":          claude,
		"runtimeOverride": s.overrides(),
		"env": map[string]bool{
			"debug":     envDebug,
			"usage":     envUsage,
			"injection": envInjection,
			"claude":    envClaude,
		},
	}
}

func (s *debugState) overrides() map[string]any {
	out := map[string]any{}
	if s.debug != nil {
		out["debug"] = *s.debug
	}
	if s.usage != nil {
		out["usage"] = *s.usage
	}
	if s.injection != nil {
		out["injection"] = *s.injection
	}
	if s.claude != nil {
		out["claude"] = *s.claude
	}
	return out
}

func (s *debugState) providerEnabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.debug != nil {
		return *s.debug
	}
	return envFlag("BENES_DEBUG") || envFlag("BENES_DEBUG_FRAMES")
}

func (s *debugState) appendProvider(line string) {
	if s == nil || !s.providerEnabled() {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logs = appendLogLocked(s.logs, &s.nextSeq, line)
}

func appendLogLocked(buf []debugLogEntry, next *int, line string) []debugLogEntry {
	if utf8.RuneCountInString(line) > debugMaxLineBytes {
		line = string([]rune(line)[:debugMaxLineBytes])
	}
	entry := debugLogEntry{Seq: *next, At: time.Now().UnixMilli(), Line: line}
	*next++
	buf = append(buf, entry)
	if len(buf) > debugMaxLines {
		buf = buf[len(buf)-debugMaxLines:]
	}
	return buf
}

func filterLogs(buf []debugLogEntry, after, limit int) []debugLogEntry {
	filtered := buf
	if after > 0 {
		out := make([]debugLogEntry, 0, len(buf))
		for _, entry := range buf {
			if entry.Seq > after {
				out = append(out, entry)
			}
		}
		filtered = out
	}
	if limit <= 0 {
		limit = debugDefaultLimit
	}
	if len(filtered) > limit {
		filtered = filtered[len(filtered)-limit:]
	}
	if filtered == nil {
		return []debugLogEntry{}
	}
	return filtered
}

func (h *handler) serveDebugAPI(w http.ResponseWriter, r *http.Request) bool {
	if h.debug == nil {
		h.debug = newDebugState()
	}
	switch r.URL.Path {
	case "/api/debug":
		if !isLoopbackRequestHost(r.Host) {
			return false
		}
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			h.debug.mu.Lock()
			view := h.debug.view()
			h.debug.mu.Unlock()
			if r.Method == http.MethodHead {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				return true
			}
			writeJSON(w, http.StatusOK, view)
			return true
		case http.MethodPut:
			return h.putDebugSettings(w, r)
		default:
			writeError(w, http.StatusNotFound, "not_found", "route not found")
			return true
		}
	case "/api/debug/logs", "/api/debug/usage-logs", "/api/debug/injection-logs":
		if !isLoopbackRequestHost(r.Host) {
			return false
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeError(w, http.StatusNotFound, "not_found", "route not found")
			return true
		}
		after, _ := strconv.Atoi(r.URL.Query().Get("after"))
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		h.debug.mu.Lock()
		var entries []debugLogEntry
		switch r.URL.Path {
		case "/api/debug/usage-logs":
			entries = filterLogs(h.debug.usageLogs, after, limit)
		default:
			entries = filterLogs(h.debug.logs, after, limit)
		}
		h.debug.mu.Unlock()
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			return true
		}
		writeJSON(w, http.StatusOK, entries)
		return true
	default:
		return false
	}
}

func (h *handler) putDebugSettings(w http.ResponseWriter, r *http.Request) bool {
	var body struct {
		Debug     *bool `json:"debug"`
		Usage     *bool `json:"usage"`
		Injection *bool `json:"injection"`
		Claude    *bool `json:"claude"`
		Reset     any   `json:"reset"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid JSON body"})
		return true
	}
	h.debug.mu.Lock()
	defer h.debug.mu.Unlock()
	if body.Reset == true {
		h.debug.debug, h.debug.usage, h.debug.injection, h.debug.claude = nil, nil, nil, nil
		writeJSON(w, http.StatusOK, h.debug.view())
		return true
	}
	if reset, ok := body.Reset.(string); ok {
		switch reset {
		case "debug", "provider":
			h.debug.debug = nil
		case "usage":
			h.debug.usage = nil
		case "injection":
			h.debug.injection = nil
		case "claude":
			h.debug.claude = nil
		default:
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "provide debug/usage/injection/claude booleans or reset:true"})
			return true
		}
		writeJSON(w, http.StatusOK, h.debug.view())
		return true
	}
	if body.Debug == nil && body.Usage == nil && body.Injection == nil && body.Claude == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "provide debug/usage/injection/claude booleans or reset:true"})
		return true
	}
	if body.Debug != nil {
		h.debug.debug = body.Debug
	}
	if body.Usage != nil {
		h.debug.usage = body.Usage
	}
	if body.Injection != nil {
		h.debug.injection = body.Injection
	}
	if body.Claude != nil {
		h.debug.claude = body.Claude
	}
	writeJSON(w, http.StatusOK, h.debug.view())
	return true
}

func (h *handler) recordDataPlaneDebug(r *http.Request) {
	if h == nil || h.debug == nil || r == nil {
		return
	}
	h.debug.appendProvider(strings.TrimSpace(r.Method + " " + r.URL.Path))
}
