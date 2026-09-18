package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/Wibias/Benes/internal/timeline"
)

const requestTimelinePath = "/v1/request-timeline"

func (h *handler) handleRequestTimeline(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "id is required")
		return
	}
	if h.timeline == nil {
		writeError(w, http.StatusNotFound, "not_found", "timeline not found")
		return
	}
	tr, err := h.timeline.Lookup(id)
	if err != nil {
		if errors.Is(err, timeline.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "timeline not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "timeline lookup failed")
		return
	}
	events := tr.Events()
	out := make([]map[string]any, 0, len(events))
	for _, ev := range events {
		item := map[string]any{
			"stage": string(ev.Stage),
			"ok":    ev.OK,
		}
		if ev.Side != "" {
			item["side"] = string(ev.Side)
		}
		if ev.Milestone != "" {
			item["milestone"] = string(ev.Milestone)
		}
		if ev.Cause != "" {
			item["cause"] = ev.Cause
		}
		if ev.Attempt > 0 {
			item["attempt"] = ev.Attempt
		}
		out = append(out, item)
	}
	payload := map[string]any{"id": tr.ID(), "events": out}
	if route := tr.Route(); route != (timeline.Route{}) {
		payload["route"] = map[string]string{
			"requestedProvider":  route.RequestedProvider,
			"providerConnection": route.ProviderConnection,
			"model":              route.Model,
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(payload)
}
