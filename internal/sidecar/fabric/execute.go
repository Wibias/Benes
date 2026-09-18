package fabric

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// RunIdentity is the opaque execution claim for one primary run.
type RunIdentity struct {
	RunID  string `json:"runId"`
	TaskID string `json:"taskId"`
	Owner  string `json:"owner"`
	Fence  int    `json:"fencingToken"`
}

// ExecuteInput starts a fenced primary run. Caller-supplied prompt/input must
// never be written into Fabric events, leases, or goals.
type ExecuteInput struct {
	Owner string
	Model string
}

// ExecuteResult is the safe metadata returned to callers.
type ExecuteResult struct {
	RunID  string `json:"runId"`
	TaskID string `json:"taskId"`
	Owner  string `json:"owner"`
	Fence  int    `json:"fencingToken"`
	Status string `json:"status"`
	Model  string `json:"model,omitempty"`
}

// BeginExecute acquires the write lease, bumps the fencing token, and appends
// RunStarted for a new primary execution run. It does not invoke a model.
func (r *Repo) BeginExecute(taskID string, in ExecuteInput) (RunIdentity, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.beginExecuteLocked(taskID, in)
}

func (r *Repo) beginExecuteLocked(taskID string, in ExecuteInput) (RunIdentity, error) {
	detail, err := r.getLocked(taskID)
	if err != nil {
		return RunIdentity{}, err
	}
	if detail.Projection.Removed {
		return RunIdentity{}, ErrRemoved
	}
	if detail.Projection.Terminal {
		return RunIdentity{}, InvalidTransition("task is already closed")
	}
	if hasActivePrimaryRun(detail.Events) {
		return RunIdentity{}, InvalidTransition("task already has an active primary run")
	}
	owner, err := validateIdentity(in.Owner, "owner")
	if err != nil {
		return RunIdentity{}, err
	}
	if owner == "" {
		owner = "operator"
	}
	model := clip(strings.TrimSpace(in.Model), maxFieldLen)
	if model != "" {
		if secretRe.MatchString(model) || homeRe.MatchString(model) {
			return RunIdentity{}, InvalidTask("model is not allowed")
		}
	}
	runID, err := newRunID()
	if err != nil {
		return RunIdentity{}, err
	}
	ls := r.leases(taskID)
	fence, err := ls.CommitHandoff(owner)
	if err != nil {
		return RunIdentity{}, err
	}
	payload := map[string]any{
		"run_id":        runID,
		"owner":         owner,
		"fencing_token": fence,
		"status":        "running",
		"kind":          "execute",
	}
	if model != "" {
		payload["model"] = model
	}
	if err := r.appendLocked(taskID, EventRunStarted, owner, "operator", owner, payload); err != nil {
		_, _ = ls.Release()
		return RunIdentity{}, err
	}
	return RunIdentity{RunID: runID, TaskID: taskID, Owner: owner, Fence: fence}, nil
}

// CompleteExecute records a fence-protected successful terminal for the run.
func (r *Repo) CompleteExecute(id RunIdentity) error {
	return r.terminalExecute(id, EventRunCompleted, "completed", "")
}

// FailExecute records a fence-protected failure terminal for the run.
func (r *Repo) FailExecute(id RunIdentity, reason string) error {
	return r.terminalExecute(id, EventRunFailed, "failed", reason)
}

// CancelExecute records a fence-protected cancel terminal for the run.
func (r *Repo) CancelExecute(id RunIdentity, reason string) error {
	if reason == "" {
		reason = "operator_cancelled"
	}
	return r.terminalExecute(id, EventRunCancelled, "cancelled", reason)
}

// InterruptExecute records a fence-protected interrupt/lost terminal.
func (r *Repo) InterruptExecute(id RunIdentity, reason string) error {
	if reason == "" {
		reason = "interrupted"
	}
	return r.terminalExecute(id, EventRunInterrupted, "interrupted", reason)
}

func (r *Repo) terminalExecute(id RunIdentity, eventType, status, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	detail, err := r.getLocked(id.TaskID)
	if err != nil {
		return err
	}
	if detail.Projection.Removed {
		return ErrRemoved
	}
	if !hasActivePrimaryRun(detail.Events) {
		if detail.Projection.Terminal {
			return InvalidTransition("task is already closed")
		}
		return InvalidTransition("task is not started")
	}
	active := activeRunIdentity(detail.Events)
	if active.RunID != "" && id.RunID != "" && active.RunID != id.RunID {
		return InvalidTransition("stale run identity")
	}
	ls := r.leases(id.TaskID)
	if err := ls.CheckOwnerAndFence(id.Owner, id.Fence); err != nil {
		return InvalidTransition(err.Error())
	}
	if child, ok := activeChildRunIdentity(detail.Events); ok {
		childEvent := EventChildRunInterrupted
		childStatus := "interrupted"
		switch eventType {
		case EventRunCancelled:
			childEvent = EventChildRunCancelled
			childStatus = "cancelled"
		case EventRunFailed:
			childEvent = EventChildRunFailed
			childStatus = "failed"
		case EventRunCompleted:
			childEvent = EventChildRunCompleted
			childStatus = "completed"
		}
		childPayload := map[string]any{
			"run_id":        child.RunID,
			"child_id":      child.ChildID,
			"owner":         id.Owner,
			"fencing_token": id.Fence,
			"status":        childStatus,
			"kind":          "child",
		}
		if reason != "" {
			childPayload["reason"] = clip(reason, maxFieldLen)
		}
		if err := r.appendLocked(id.TaskID, childEvent, id.Owner, "operator", id.Owner, childPayload); err != nil {
			return err
		}
	}
	payload := map[string]any{
		"run_id":        id.RunID,
		"owner":         id.Owner,
		"fencing_token": id.Fence,
		"status":        status,
		"kind":          "execute",
	}
	if reason != "" {
		payload["reason"] = clip(reason, maxFieldLen)
	}
	if err := r.appendLocked(id.TaskID, eventType, id.Owner, "operator", id.Owner, payload); err != nil {
		// Fail closed: do not release the lease when the terminal event did not persist.
		return err
	}
	if _, err := ls.Release(); err != nil {
		// Terminal is durable; lease release is reconciliation best-effort.
		return nil
	}
	return nil
}

// RecordProgress appends a non-terminal progress mark that must not consume
// reserved terminal event headroom.
func (r *Repo) RecordProgress(id RunIdentity, phase string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	detail, err := r.getLocked(id.TaskID)
	if err != nil {
		return err
	}
	if !hasActivePrimaryRun(detail.Events) {
		return InvalidTransition("task is not started")
	}
	ls := r.leases(id.TaskID)
	if err := ls.CheckOwnerAndFence(id.Owner, id.Fence); err != nil {
		return InvalidTransition(err.Error())
	}
	return r.appendLocked(id.TaskID, EventRunProgress, id.Owner, "operator", id.Owner, map[string]any{
		"run_id": id.RunID,
		"phase":  clip(phase, 80),
		"status": "running",
	})
}

// RecoverOrphans marks positively identified v2 execute RunStarted events
// (kind=execute, run_id, owner, fence) without a terminal as Interrupted/lost.
// Half-committed handoffs are reconciled from event history + the exact current
// lease only - never by replaying child input or parent turns.
// v1 lifecycle MarkStarted is never rewritten. It does not invoke providers.
// Per-task authority work is shared with ReconcileExecuteInterrupted so live
// same-process half-commit recovery uses the same algorithm as restart recovery.
func (r *Repo) RecoverOrphans() (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entries, err := os.ReadDir(r.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	n := 0
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".events.jsonl") {
			continue
		}
		taskID := strings.TrimSuffix(name, ".events.jsonl")
		ok, err := r.reconcileExecuteInterruptedLocked(taskID, "", "lost_on_restart")
		if err != nil {
			return n, err
		}
		if ok {
			n++
		}
	}
	return n, nil
}

// ReconcileExecuteInterrupted converges an execute run that lost live authority
// (for example a recoverable handoff storage failure after lease CAS) to a single
// primary RunInterrupted using the same recoverAuthorityClaim rules as RecoverOrphans.
// It never opens providers, never replays input, and never invents a fencing rollback.
// If reconciliation cannot persist, it returns a distinct error and does not claim success.
func (r *Repo) ReconcileExecuteInterrupted(taskID, runID, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	ok, err := r.reconcileExecuteInterruptedLocked(taskID, runID, reason)
	if err != nil {
		return err
	}
	if !ok {
		return InvalidTransition("execute interrupt reconciliation could not claim authority")
	}
	return nil
}

// reconcileExecuteInterruptedLocked is the single per-task authority algorithm used by
// RecoverOrphans and live same-process half-commit reconciliation.
// Returns (true, nil) when a RunInterrupted was appended (or the run was already absent).
func (r *Repo) reconcileExecuteInterruptedLocked(taskID, runID, reason string) (bool, error) {
	detail, err := r.getLocked(taskID)
	if err != nil || detail.Projection.Removed {
		return false, nil
	}
	if !hasActivePrimaryRun(detail.Events) {
		return false, nil
	}
	id, ok := activeExecuteRunIdentity(detail.Events)
	if !ok {
		return false, nil
	}
	if id.Owner == "" || id.Fence <= 0 || id.RunID == "" {
		return false, nil
	}
	if runID != "" && id.RunID != runID {
		return false, nil
	}
	if reason == "" {
		reason = "interrupted"
	}
	reason = clip(reason, maxFieldLen)

	ls := r.leases(taskID)
	lease, lerr := ls.read()
	if lerr != nil {
		return false, nil
	}
	claimOwner, claimFence, claimOK := recoverAuthorityClaim(detail.Events, lease.Owner, lease.FencingToken)
	if !claimOK {
		return false, nil
	}
	if err := ls.CheckOwnerAndFence(claimOwner, claimFence); err != nil {
		return false, nil
	}

	// Re-read events after authority checks so open-proposal / active-child reflect
	// any appends we already made (rollback) in this same locked section.
	detail, err = r.getLocked(taskID)
	if err != nil {
		return false, err
	}

	// Close any open parent->child/return propose that never committed.
	if hid, stage := openHandoffProposal(detail.Events); hid != "" {
		if err := r.appendLocked(taskID, EventHandoffRolledBack, claimOwner, "operator", "kernel", map[string]any{
			"handoff_id":    hid,
			"run_id":        id.RunID,
			"fencing_token": claimFence,
			"reason":        reason,
			"stage":         stage,
		}); err != nil {
			return false, err
		}
		detail, err = r.getLocked(taskID)
		if err != nil {
			return false, err
		}
	}

	if child, childOK := activeChildRunIdentity(detail.Events); childOK {
		childPayload := map[string]any{
			"run_id":        child.RunID,
			"child_id":      child.ChildID,
			"handoff_id":    child.HandoffID,
			"owner":         child.Owner,
			"fencing_token": child.Fence,
			"status":        "interrupted",
			"reason":        reason,
			"kind":          "child",
		}
		if err := r.appendLocked(taskID, EventChildRunInterrupted, child.Owner, "operator", "kernel", childPayload); err != nil {
			return false, err
		}
	}
	payload := map[string]any{
		"run_id":        id.RunID,
		"owner":         claimOwner,
		"fencing_token": claimFence,
		"status":        "interrupted",
		"reason":        reason,
		"kind":          "execute",
	}
	if err := r.appendLocked(taskID, EventRunInterrupted, claimOwner, "operator", "kernel", payload); err != nil {
		return false, err
	}
	if _, err := ls.Release(); err != nil {
		_ = err
	}
	return true, nil
}

// recoverAuthorityClaim reconstructs the only claim that may interrupt from
// event history + the exact current lease. No raw instruction/output is used.
func recoverAuthorityClaim(events []Event, leaseOwner string, leaseFence int) (string, int, bool) {
	if leaseOwner == "" || leaseFence <= 0 {
		return "", 0, false
	}
	if child, ok := activeChildRunIdentity(events); ok {
		if child.Owner == leaseOwner && child.Fence == leaseFence {
			return child.Owner, child.Fence, true
		}
	}
	if owner, fence, ok := latestHandoffCommittedClaim(events); ok {
		if owner == leaseOwner && fence == leaseFence {
			return owner, fence, true
		}
	}
	if id, ok := activeExecuteRunIdentity(events); ok {
		if id.Owner == leaseOwner && id.Fence == leaseFence {
			return id.Owner, id.Fence, true
		}
	}
	if owner, fence, ok := openProposedMatchingLease(events, leaseOwner, leaseFence); ok {
		return owner, fence, true
	}
	return "", 0, false
}

func latestHandoffCommittedClaim(events []Event) (string, int, bool) {
	owner, fence := "", 0
	ok := false
	for _, ev := range events {
		if ev.EventType != EventHandoffCommitted {
			continue
		}
		pl := payloadMap(ev.Payload)
		next := payloadString(pl, "new_owner", "newOwner")
		tok := payloadInt(pl, "fencing_token", "fencingToken")
		if next != "" && tok > 0 {
			owner, fence, ok = next, tok, true
		}
	}
	return owner, fence, ok
}

func openHandoffProposal(events []Event) (handoffID, stage string) {
	type prop struct {
		id    string
		stage string
	}
	var open *prop
	for _, ev := range events {
		pl := payloadMap(ev.Payload)
		hid := payloadString(pl, "handoff_id", "handoffId")
		switch ev.EventType {
		case EventHandoffProposed:
			if hid != "" {
				open = &prop{id: hid, stage: "proposed"}
			}
		case EventHandoffCommitted, EventHandoffFailed, EventHandoffRolledBack:
			if open != nil && (hid == "" || hid == open.id) {
				open = nil
			}
		}
	}
	if open == nil {
		return "", ""
	}
	return open.id, open.stage
}

func openProposedMatchingLease(events []Event, leaseOwner string, leaseFence int) (string, int, bool) {
	type prop struct {
		id            string
		fromOwner     string
		toOwner       string
		previousFence int
		proposedFence int
	}
	var open *prop
	for _, ev := range events {
		pl := payloadMap(ev.Payload)
		hid := payloadString(pl, "handoff_id", "handoffId")
		switch ev.EventType {
		case EventHandoffProposed:
			open = &prop{
				id:            hid,
				fromOwner:     payloadString(pl, "from_owner", "fromOwner"),
				toOwner:       payloadString(pl, "to_owner", "toOwner"),
				previousFence: payloadInt(pl, "previous_fence", "previousFence"),
				proposedFence: payloadInt(pl, "proposed_fence", "proposedFence"),
			}
		case EventHandoffCommitted, EventHandoffFailed, EventHandoffRolledBack:
			if open != nil && (hid == "" || hid == open.id) {
				open = nil
			}
		}
	}
	if open == nil {
		return "", 0, false
	}
	if leaseOwner == open.toOwner && leaseFence == open.proposedFence && open.proposedFence > 0 {
		return leaseOwner, leaseFence, true
	}
	if leaseOwner == open.fromOwner && leaseFence == open.previousFence && open.previousFence > 0 {
		return leaseOwner, leaseFence, true
	}
	return "", 0, false
}

func activeExecuteRunIdentity(events []Event) (RunIdentity, bool) {
	var id RunIdentity
	var ok bool
	for _, ev := range events {
		pl := payloadMap(ev.Payload)
		switch ev.EventType {
		case EventRunStarted:
			kind := payloadString(pl, "kind")
			runID := payloadString(pl, "run_id", "runId")
			owner := firstNonEmpty(payloadString(pl, "owner"), ev.RuntimeSessionID, ev.ActorID)
			fence := payloadInt(pl, "fencing_token", "fencingToken")
			if kind == "execute" && runID != "" && owner != "" && fence > 0 {
				id = RunIdentity{RunID: runID, TaskID: ev.TaskID, Owner: owner, Fence: fence}
				ok = true
			} else {
				id = RunIdentity{}
				ok = false
			}
		case EventRunCompleted, EventRunFailed, EventRunCancelled, EventRunInterrupted:
			id = RunIdentity{}
			ok = false
		}
	}
	return id, ok
}

func activeRunIdentity(events []Event) RunIdentity {
	var id RunIdentity
	for _, ev := range events {
		pl := payloadMap(ev.Payload)
		switch ev.EventType {
		case EventRunStarted:
			id = RunIdentity{
				RunID:  payloadString(pl, "run_id", "runId"),
				TaskID: ev.TaskID,
				Owner:  firstNonEmpty(payloadString(pl, "owner"), ev.RuntimeSessionID, ev.ActorID),
				Fence:  payloadInt(pl, "fencing_token", "fencingToken"),
			}
		case EventRunCompleted, EventRunFailed, EventRunCancelled, EventRunInterrupted:
			id = RunIdentity{}
		}
	}
	return id
}

func payloadInt(m map[string]any, keys ...string) int {
	for _, k := range keys {
		switch v := m[k].(type) {
		case float64:
			return int(v)
		case int:
			return v
		case int64:
			return int(v)
		case json.Number:
			n, _ := v.Int64()
			return int(n)
		}
	}
	return 0
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func newRunID() (string, error) {
	var b [10]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return fmt.Sprintf("run_%d_%s", time.Now().UnixMilli(), hex.EncodeToString(b[:])), nil
}
