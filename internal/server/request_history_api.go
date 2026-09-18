package server

import (
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/Wibias/Benes/internal/requesthistory"
)

func (h *handler) serveRequestHistoryAPI(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/api/request-history" && !strings.HasPrefix(r.URL.Path, "/api/request-history/") {
		return false
	}
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
	if strings.HasPrefix(r.URL.Path, "/api/request-history/") && strings.HasSuffix(r.URL.Path, "/route-decision") {
		rawID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/request-history/"), "/route-decision")
		id, err := url.PathUnescape(rawID)
		if err != nil || strings.TrimSpace(id) == "" || strings.Contains(id, "/") {
			writeError(w, http.StatusNotFound, "not_found", "unknown request")
			return true
		}
		row, err := h.lookupRequestHistory(id)
		if err != nil {
			if errors.Is(err, requesthistory.ErrRebuildRequired) || errors.Is(err, requesthistory.ErrUnavailable) {
				writeError(w, http.StatusServiceUnavailable, "index_unavailable", "request-history index is unavailable")
				return true
			}
			if !errors.Is(err, os.ErrNotExist) {
				writeError(w, http.StatusServiceUnavailable, "index_unavailable", "request-history index is unavailable")
				return true
			}
			writeError(w, http.StatusNotFound, "not_found", "unknown request")
			return true
		}
		if row == nil {
			writeError(w, http.StatusNotFound, "not_found", "unknown request")
			return true
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"requestId":     id,
			"routeDecision": row["routeDecision"],
			"outcome": map[string]any{
				"status": row["status"],
			},
			"summary": map[string]any{
				"requestedModel": row["requestedModel"],
				"finalProvider":  row["provider"],
				"finalModel":     row["model"],
			},
		})
		return true
	}
	if r.URL.Path != "/api/request-history" {
		return false
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id != "" {
		if h.timeline == nil {
			writeError(w, http.StatusNotFound, "not_found", "timeline not found")
			return true
		}
		tr, err := h.timeline.Lookup(id)
		if err != nil {
			writeError(w, http.StatusNotFound, "not_found", "timeline not found")
			return true
		}
		events := make([]map[string]any, 0)
		for _, ev := range tr.Events() {
			events = append(events, map[string]any{
				"stage":     ev.Stage,
				"side":      ev.Side,
				"milestone": ev.Milestone,
				"ok":        ev.OK,
				"cause":     ev.Cause,
				"elapsedMs": ev.Elapsed.Milliseconds(),
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": tr.ID(), "events": events})
		return true
	}
	ids := []string{}
	if h.timeline != nil {
		ids = h.timeline.List()
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": ids})
	return true
}

func (h *handler) lookupRequestHistory(id string) (map[string]any, error) {
	if h != nil && h.requestHistory != nil {
		return h.requestHistory.Lookup(id)
	}
	home := ""
	if h != nil && strings.TrimSpace(h.configPath) != "" {
		home = filepath.Dir(h.configPath)
	}
	return requesthistory.Lookup(home, id)
}
