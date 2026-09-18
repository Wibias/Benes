package server

import "net/http"

func (h *handler) serveResourceMetrics(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/api/resource-metrics" {
		return false
	}
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
	if h.resourceBudget == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"activeTurns": 0, "queuedTurns": 0, "activeReaders": 0, "processBytes": 0,
			"budgetWaiters": 0, "postCommitRetryAttempts": 0,
		})
		return true
	}
	m := h.resourceBudget.Metrics()
	writeJSON(w, http.StatusOK, map[string]any{
		"activeTurns":             m.ActiveTurns,
		"queuedTurns":             m.QueuedTurns,
		"activeReaders":           m.ActiveReaders,
		"processBytes":            m.ProcessBytes,
		"bytes":                   m.Bytes,
		"activeByPhase":           m.ActiveByPhase,
		"budgetWaiters":           m.BudgetWaiters,
		"postCommitRetryAttempts": m.PostCommitRetryAttempts,
	})
	return true
}
