package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/transport"
)

type providerPacingRuntimeHandler struct {
	next       http.Handler
	pacers     map[string]*transport.Pacer
	configPath string
}

// WithProviderPacingRuntime augments the loopback pacing endpoint with live
// queue/slot/model telemetry from the provider registry. The underlying data
// plane handler remains unchanged for all other routes.
func WithProviderPacingRuntime(next http.Handler, pacers map[string]*transport.Pacer, configPath string) http.Handler {
	if next == nil || len(pacers) == 0 {
		return next
	}
	cloned := make(map[string]*transport.Pacer, len(pacers))
	for id, pacer := range pacers {
		if strings.TrimSpace(id) != "" && pacer != nil {
			cloned[id] = pacer
		}
	}
	if len(cloned) == 0 {
		return next
	}
	return &providerPacingRuntimeHandler{next: next, pacers: cloned, configPath: strings.TrimSpace(configPath)}
}

func (h *providerPacingRuntimeHandler) Close() error {
	if h == nil {
		return nil
	}
	if closer, ok := h.next.(interface{ Close() error }); ok {
		return closer.Close()
	}
	return nil
}

func (h *providerPacingRuntimeHandler) SetStop(fn func()) {
	if h == nil {
		return
	}
	if setter, ok := h.next.(interface{ SetStop(func()) }); ok {
		setter.SetStop(fn)
	}
}

func (h *providerPacingRuntimeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/provider-request-pacing" || !isLoopbackRequestHost(r.Host) {
		h.next.ServeHTTP(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return
	}
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		return
	}
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	configured := h.configuredPacing()
	if name != "" {
		if _, ok := configured[name]; !ok {
			if _, runtime := h.pacers[name]; !runtime {
				writeError(w, http.StatusNotFound, "unknown_provider", "unknown provider")
				return
			}
		}
	}

	pacing := map[string]int{}
	for id, ms := range configured {
		if name == "" || name == id {
			pacing[id] = ms
		}
	}
	runtime := map[string]map[string]any{}
	for id, pacer := range h.pacers {
		if name != "" && name != id {
			continue
		}
		snapshot := pacer.Snapshot()
		runtime[id] = map[string]any{
			"currentQueue":    snapshot.Queue,
			"untilNextSlotMs": durationMillisecondsCeil(snapshot.UntilNextSlot),
			"lastModel":       snapshot.LastLabel,
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"pacing": pacing, "runtime": runtime})
}

func (h *providerPacingRuntimeHandler) configuredPacing() map[string]int {
	out := map[string]int{}
	if h.configPath == "" {
		return out
	}
	disk, err := config.LoadDiskConfig(h.configPath, 0)
	if err != nil {
		return out
	}
	for id, raw := range disk.Providers {
		var rec struct {
			RequestPacingMs int `json:"requestPacingMs"`
		}
		if json.Unmarshal(raw, &rec) == nil {
			out[id] = rec.RequestPacingMs
		}
	}
	return out
}

func durationMillisecondsCeil(value time.Duration) int64 {
	if value <= 0 {
		return 0
	}
	return int64((value + time.Millisecond - 1) / time.Millisecond)
}
