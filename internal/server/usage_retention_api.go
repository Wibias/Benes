package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/requesthistory"
	"github.com/Wibias/Benes/internal/usageledger"
)

type usageRetentionJob struct {
	mu      sync.Mutex
	running bool
}

func (h *handler) serveUsageRetentionAPI(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/api/usage/retention" && !strings.HasPrefix(r.URL.Path, "/api/usage/retention/") {
		return false
	}
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	switch {
	case r.URL.Path == "/api/usage/retention" && (r.Method == http.MethodGet || r.Method == http.MethodHead):
		h.serveUsageRetentionGet(w, r)
		return true
	case r.URL.Path == "/api/usage/retention" && r.Method == http.MethodPut:
		h.serveUsageRetentionPut(w, r)
		return true
	case r.URL.Path == "/api/usage/retention/preview" && r.Method == http.MethodPost:
		h.serveUsageRetentionPreview(w, r)
		return true
	case r.URL.Path == "/api/usage/retention/run" && r.Method == http.MethodPost:
		h.serveUsageRetentionRun(w, r)
		return true
	default:
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
}

func (h *handler) serveUsageRetentionGet(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		return
	}
	policy := h.loadUsageRetentionPolicy()
	home := h.resolvedUsageHome()
	ledger, err := usageledger.Open(home)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"policy": policy,
			"error":  "ledger_unavailable",
		})
		return
	}
	status, err := ledger.RetentionStatus(policy)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"policy": policy,
			"error":  "read_failed",
		})
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (h *handler) serveUsageRetentionPut(w http.ResponseWriter, r *http.Request) {
	var body usageledger.RetentionPolicy
	if err := decodeLimitedJSON(r, &body, 16<<10); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": usageledger.CodeInvalidPolicy, "message": "invalid JSON body"})
		return
	}
	policy, err := usageledger.NormalizeRetentionPolicy(body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": usageledger.CodeInvalidPolicy, "message": err.Error()})
		return
	}
	if h.usageRetentionRunning() {
		writeJSON(w, http.StatusConflict, map[string]any{"error": usageledger.CodeRetentionBusy, "policy": h.loadUsageRetentionPolicy()})
		return
	}
	if err := h.saveUsageRetentionPolicy(policy); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "config_write_failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "policy": policy})
}

func (h *handler) serveUsageRetentionPreview(w http.ResponseWriter, r *http.Request) {
	var body struct {
		MaxBytes *int64 `json:"maxBytes"`
		MaxAgeMs *int64 `json:"maxAgeMs"`
	}
	if r.Body != nil && r.ContentLength != 0 {
		if err := decodeLimitedJSON(r, &body, 16<<10); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": usageledger.CodeInvalidPolicy, "message": "invalid JSON body"})
			return
		}
	}
	policy := h.loadUsageRetentionPolicy()
	if body.MaxBytes != nil {
		policy.MaxBytes = *body.MaxBytes
	}
	if body.MaxAgeMs != nil {
		policy.MaxAgeMs = *body.MaxAgeMs
	}
	policy, err := usageledger.NormalizeRetentionPolicy(policy)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": usageledger.CodeInvalidPolicy, "message": err.Error()})
		return
	}
	home := h.resolvedUsageHome()
	ledger, err := usageledger.Open(home)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "ledger_unavailable"})
		return
	}
	preview, err := ledger.PreviewRetention(policy, time.Now())
	if err != nil {
		code := preview.Error
		if code == "" {
			code = usageledger.CodeFSFailed
		}
		status := http.StatusBadRequest
		if errors.Is(err, usageledger.ErrAgeUnknown) {
			status = http.StatusConflict
		}
		writeJSON(w, status, preview)
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

func (h *handler) serveUsageRetentionRun(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Digest   string `json:"digest"`
		MaxBytes *int64 `json:"maxBytes"`
		MaxAgeMs *int64 `json:"maxAgeMs"`
	}
	if err := decodeLimitedJSON(r, &body, 16<<10); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": usageledger.CodeInvalidPolicy, "message": "invalid JSON body"})
		return
	}
	policy := h.loadUsageRetentionPolicy()
	if body.MaxBytes != nil {
		policy.MaxBytes = *body.MaxBytes
	}
	if body.MaxAgeMs != nil {
		policy.MaxAgeMs = *body.MaxAgeMs
	}
	policy, err := usageledger.NormalizeRetentionPolicy(policy)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": usageledger.CodeInvalidPolicy, "message": err.Error()})
		return
	}
	if !h.beginUsageRetentionJob() {
		writeJSON(w, http.StatusConflict, map[string]any{"error": usageledger.CodeRetentionBusy})
		return
	}
	defer h.endUsageRetentionJob()

	home := h.resolvedUsageHome()
	ledger, err := usageledger.Open(home)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "ledger_unavailable"})
		return
	}
	// Ledger-first durable prune.
	result, err := ledger.RunRetention(policy, body.Digest, time.Now())
	if err != nil {
		status := http.StatusConflict
		if errors.Is(err, usageledger.ErrInvalidPolicy) {
			status = http.StatusBadRequest
		}
		writeJSON(w, status, result)
		return
	}
	// SQLite-second under PR181 writer lock.
	if _, rhErr := requesthistory.ApplyRetention(home); rhErr != nil && !errors.Is(rhErr, requesthistory.ErrRebuildRequired) {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":               result.OK,
			"policy":           result.Policy,
			"deletedRelPaths":  result.DeletedRelPaths,
			"deletedBytes":     result.DeletedBytes,
			"extents":          result.Extents,
			"generation":       result.Generation,
			"historyTruncated": result.HistoryTruncated,
			"indexWarning":     rhErr.Error(),
		})
		return
	}
	if h.requestHistory != nil {
		_ = h.requestHistory.CatchUpBestEffort()
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *handler) loadUsageRetentionPolicy() usageledger.RetentionPolicy {
	policy := usageledger.DefaultRetentionPolicy()
	root := h.loadConfigRoot()
	if root == nil {
		return policy
	}
	raw, ok := root["usageRetention"]
	if !ok || raw == nil {
		return policy
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return policy
	}
	parsed, err := usageledger.ParseRetentionPolicy(encoded)
	if err != nil {
		return policy
	}
	return parsed
}

func (h *handler) saveUsageRetentionPolicy(policy usageledger.RetentionPolicy) error {
	if strings.TrimSpace(h.configPath) == "" {
		return nil
	}
	payload, err := json.Marshal(policy)
	if err != nil {
		return err
	}
	return h.commitConfig(func(tx *config.Transaction) error {
		return tx.Set(config.JSONPath("usageRetention"), payload)
	})
}

func (h *handler) beginUsageRetentionJob() bool {
	h.ensureUsageRetentionJob()
	h.usageRetentionJob.mu.Lock()
	defer h.usageRetentionJob.mu.Unlock()
	if h.usageRetentionJob.running {
		return false
	}
	h.usageRetentionJob.running = true
	return true
}

func (h *handler) endUsageRetentionJob() {
	h.ensureUsageRetentionJob()
	h.usageRetentionJob.mu.Lock()
	defer h.usageRetentionJob.mu.Unlock()
	h.usageRetentionJob.running = false
}

func (h *handler) usageRetentionRunning() bool {
	h.ensureUsageRetentionJob()
	h.usageRetentionJob.mu.Lock()
	defer h.usageRetentionJob.mu.Unlock()
	return h.usageRetentionJob.running
}

func (h *handler) ensureUsageRetentionJob() {
	if h.usageRetentionJob == nil {
		h.usageRetentionJob = &usageRetentionJob{}
	}
}
