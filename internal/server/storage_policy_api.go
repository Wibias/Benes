package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/storage"
)

type storageJobState struct {
	mu               sync.Mutex
	running          bool
	gen              int64
	startedAt        int64
	recovered        bool
	persistFailedGen int64
	persistFailed    *storage.Policy
}

func (h *handler) serveStoragePolicyGet(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		return
	}
	policy := h.loadCleanupPolicy()
	writeJSON(w, http.StatusOK, policy)
}

func (h *handler) serveStoragePolicyPut(w http.ResponseWriter, r *http.Request) {
	var body storage.Policy
	if err := decodeLimitedJSON(r, &body, 16<<10); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": storage.CodeInvalidPolicy, "message": "invalid JSON body"})
		return
	}
	policy, err := storage.NormalizePolicy(body)
	if err != nil {
		e := storageErr(err)
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": e.Code, "message": e.Message})
		return
	}
	current := h.loadCleanupPolicy()
	if h.storageJobRunning() || (current.Job != nil && current.Job.Status == storage.JobRunning) {
		writeJSON(w, http.StatusConflict, map[string]any{"error": storage.CodeAlreadyRunning, "policy": current})
		return
	}
	policy.LastRun = current.LastRun
	policy.Job = current.Job
	policy.NextRun = storage.PreserveNextRun(current, policy, time.Now(), time.Local)
	if err := h.saveCleanupPolicy(policy); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "config_write_failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "policy": policy})
}

func (h *handler) serveStoragePolicyRun(w http.ResponseWriter, r *http.Request) {
	if !h.beginStorageJob("manual") {
		policy := h.loadCleanupPolicy()
		writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": storage.CodeAlreadyRunning, "policy": policy})
		return
	}
	policy := h.loadCleanupPolicy()
	gen, startedAt := h.currentStorageJob()
	policy.Job = &storage.PolicyJob{Status: storage.JobRunning, StartedAt: startedAt, Reason: "manual", Generation: gen}
	if err := h.saveCleanupPolicy(policy); err != nil {
		h.endStorageJob(gen)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "config_write_failed", "policy": policy})
		return
	}
	home := h.storageHome()
	go h.runCleanupJob(home, policy, startedAt, gen)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "started": true, "job": policy.Job, "policy": policy})
}

func (h *handler) runCleanupJob(home string, policy storage.Policy, startedAt, gen int64) {
	updated, _, _ := h.engine().RunPolicy(home, policy)
	h.finishStorageJob(gen, startedAt, updated)
}

func (h *handler) finishStorageJob(gen, startedAt int64, updated storage.Policy) {
	h.storageJob.mu.Lock()
	defer h.storageJob.mu.Unlock()
	if h.storageJob.gen != gen {
		return
	}
	if updated.Job == nil {
		updated.Job = &storage.PolicyJob{Status: storage.JobIdle}
	}
	updated.Job.StartedAt = startedAt
	updated.Job.Status = storage.JobIdle
	updated.Job.Generation = gen
	if err := h.saveCleanupPolicy(updated); err != nil {
		h.storageJob.running = false
		h.storageJob.persistFailedGen = gen
		if updated.Job.LastError == "" {
			updated.Job.LastError = "config_write_failed"
		}
		if updated.Job.LastOutcome == nil {
			updated.Job.LastOutcome = &storage.JobOutcome{OK: false, Error: "config_write_failed"}
		} else {
			updated.Job.LastOutcome.OK = false
			if updated.Job.LastOutcome.Error == "" {
				updated.Job.LastOutcome.Error = "config_write_failed"
			}
		}
		copy := updated
		h.storageJob.persistFailed = &copy
		return
	}
	h.storageJob.running = false
	h.storageJob.persistFailedGen = 0
	h.storageJob.persistFailed = nil
}

func (h *handler) beginStorageJob(reason string) bool {
	h.storageJob.mu.Lock()
	defer h.storageJob.mu.Unlock()
	if h.storageJob.running {
		return false
	}
	h.storageJob.running = true
	h.storageJob.gen++
	h.storageJob.startedAt = time.Now().UnixMilli()
	h.storageJob.persistFailedGen = 0
	h.storageJob.persistFailed = nil
	_ = reason
	return true
}

func (h *handler) currentStorageJob() (gen, startedAt int64) {
	h.storageJob.mu.Lock()
	defer h.storageJob.mu.Unlock()
	return h.storageJob.gen, h.storageJob.startedAt
}

func (h *handler) endStorageJob(gen int64) {
	h.storageJob.mu.Lock()
	defer h.storageJob.mu.Unlock()
	if h.storageJob.gen == gen {
		h.storageJob.running = false
	}
}

func (h *handler) storageJobRunning() bool {
	h.storageJob.mu.Lock()
	defer h.storageJob.mu.Unlock()
	return h.storageJob.running
}

func (h *handler) loadCleanupPolicy() storage.Policy {
	policy := h.readCleanupPolicy()
	h.storageJob.mu.Lock()
	defer h.storageJob.mu.Unlock()
	if h.storageJob.running {
		if policy.Job == nil {
			policy.Job = &storage.PolicyJob{}
		}
		policy.Job.Status = storage.JobRunning
		policy.Job.StartedAt = h.storageJob.startedAt
		policy.Job.Generation = h.storageJob.gen
		return policy
	}
	if h.storageJob.persistFailed != nil && h.storageJob.persistFailedGen != 0 {
		if policy.Job != nil && policy.Job.Status == storage.JobRunning && policy.Job.Generation == h.storageJob.persistFailedGen {
			return *h.storageJob.persistFailed
		}
	}
	return policy
}

func (h *handler) readCleanupPolicy() storage.Policy {
	policy := storage.DefaultPolicy()
	root := h.loadConfigRoot()
	if root == nil {
		return policy
	}
	raw, ok := root["storageCleanup"]
	if !ok || raw == nil {
		return policy
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return policy
	}
	parsed, err := storage.ParsePolicy(encoded)
	if err != nil {
		return policy
	}
	return parsed
}

func (h *handler) saveCleanupPolicy(policy storage.Policy) error {
	if h != nil && h.storagePersistErr != nil {
		return h.storagePersistErr
	}
	if strings.TrimSpace(h.configPath) == "" {
		return nil
	}
	payload, err := json.Marshal(policy)
	if err != nil {
		return err
	}
	return h.commitConfig(func(tx *config.Transaction) error {
		return tx.Set(config.JSONPath("storageCleanup"), payload)
	})
}

func (h *handler) StartStorageCleanup(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	h.storageOnce.Do(func() {
		child, cancel := context.WithCancel(ctx)
		h.storageCancel = cancel
		prev := h.stop
		h.stop = func() {
			cancel()
			if prev != nil {
				prev()
			}
		}
		h.recoverStorageJob()
		go h.loopStoragePolicy(child)
		home := h.storageHome()
		if home != "" {
			_ = h.engine().Reconcile(home)
		}
		if child.Err() != nil {
			return
		}
		policy := h.readCleanupPolicy()
		if storage.ShouldRunScheduled(policy, time.Now(), time.Local, true) {
			h.kickScheduled(policy, "startup")
		}
	})
}

func (h *handler) recoverStorageJob() {
	h.storageJob.mu.Lock()
	defer h.storageJob.mu.Unlock()
	if h.storageJob.recovered {
		return
	}
	h.storageJob.recovered = true
	policy := h.readCleanupPolicy()
	if policy.Job != nil && policy.Job.Status == storage.JobRunning {
		cleared := storage.ClearStaleRunning(policy, time.Now())
		_ = h.saveCleanupPolicy(cleared)
	}
}

func (h *handler) loopStoragePolicy(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			policy := h.readCleanupPolicy()
			if storage.ShouldRunScheduled(policy, time.Now(), time.Local, false) {
				h.kickScheduled(policy, policy.Schedule)
			}
		}
	}
}

func (h *handler) kickScheduled(policy storage.Policy, reason string) {
	if !h.beginStorageJob(reason) {
		return
	}
	gen, startedAt := h.currentStorageJob()
	if !policy.Enabled || policy.Schedule == storage.ScheduleManual {
		h.endStorageJob(gen)
		return
	}
	policy.Job = &storage.PolicyJob{Status: storage.JobRunning, StartedAt: startedAt, Reason: reason, Generation: gen}
	if err := h.saveCleanupPolicy(policy); err != nil {
		h.endStorageJob(gen)
		return
	}
	home := h.storageHome()
	go h.runCleanupJob(home, policy, startedAt, gen)
}
