package server

import "net/http"

// Close stops fabric live workers then closes request history.
// Limitation: fabricRuntime.shutdown does not propagate dual-storage-failure /
// half-commit persistence errors. Close therefore cannot claim durable Fabric
// convergence when repo appends fail during child-return interrupt reconcile;
// callers must not treat a nil Close error as evidence that every active run
// reached a durable terminal. RecoverOrphans / restart recovery remain the
// durable convergence path for those cases.
func (h *handler) Close() error {
	if h == nil {
		return nil
	}
	if h.fabricRuntime != nil {
		repo, err := h.fabricRepo()
		if err != nil {
			h.fabricRuntime.shutdown(nil)
		} else {
			h.fabricRuntime.shutdown(repo)
		}
	}
	if h.requestHistory == nil {
		return nil
	}
	err := h.requestHistory.Close()
	h.requestHistory = nil
	return err
}

func (h *handler) SetStop(fn func()) {
	if h == nil {
		return
	}
	h.stop = func() {
		if h.storageCancel != nil {
			h.storageCancel()
		}
		_ = h.Close()
		if fn != nil {
			fn()
		}
	}
}

func (h *handler) serveStop(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/api/stop" {
		return false
	}
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	if h.stop != nil {
		go h.stop()
	}
	return true
}
