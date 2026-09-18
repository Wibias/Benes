package fabric

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
)

const (
	// FabricDelegateToolName is the reserved opt-in tool for single-child handoff.
	FabricDelegateToolName = "__benes_fabric_delegate_v1"
	// MaxDelegateInstructionBytes caps the delegate instruction payload (16 KiB).
	MaxDelegateInstructionBytes = 16 << 10
	childOwnerPrefix            = "fabric-child-"

	HandoffDirectionParentToChild = "parent_to_child"
	HandoffDirectionChildToParent = "child_to_parent"
)

// ChildRunIdentity is the opaque claim for the single active child under a primary run.
type ChildRunIdentity struct {
	ChildID   string `json:"childId"`
	RunID     string `json:"runId"` // always the primary run id
	TaskID    string `json:"taskId"`
	Owner     string `json:"owner"`
	Fence     int    `json:"fencingToken"`
	Model     string `json:"model,omitempty"`
	HandoffID string `json:"handoffId,omitempty"`
}

// BeginChildHandoff runs the durable parent→child handoff state machine:
// HandoffProposed → CAS N→N+1 → HandoffCommitted → ChildRunStarted.
// Half-commits are left recoverable from event history + lease (no silent N+2 rollback).
func (r *Repo) BeginChildHandoff(primary RunIdentity, childModel string) (ChildRunIdentity, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.beginChildHandoffLocked(primary, childModel)
}

func (r *Repo) beginChildHandoffLocked(primary RunIdentity, childModel string) (ChildRunIdentity, error) {
	detail, err := r.getLocked(primary.TaskID)
	if err != nil {
		return ChildRunIdentity{}, err
	}
	if detail.Projection.Removed {
		return ChildRunIdentity{}, ErrRemoved
	}
	if !hasActivePrimaryRun(detail.Events) {
		return ChildRunIdentity{}, InvalidTransition("task is not started")
	}
	active, ok := activeExecuteRunIdentity(detail.Events)
	if !ok || active.RunID == "" {
		return ChildRunIdentity{}, InvalidTransition("stale run identity")
	}
	if primary.RunID == "" || primary.RunID != active.RunID {
		return ChildRunIdentity{}, InvalidTransition("stale run identity")
	}
	if hasPriorChildRunStarted(detail.Events, active.RunID) {
		return ChildRunIdentity{}, InvalidTransition("task already had a child run")
	}
	ls := r.leases(primary.TaskID)
	if err := ls.CheckOwnerAndFence(primary.Owner, primary.Fence); err != nil {
		return ChildRunIdentity{}, InvalidTransition(err.Error())
	}

	childModel = clip(strings.TrimSpace(childModel), maxFieldLen)
	if childModel == "" {
		return ChildRunIdentity{}, InvalidTask("child model is required")
	}
	if secretRe.MatchString(childModel) || homeRe.MatchString(childModel) {
		return ChildRunIdentity{}, InvalidTask("model is not allowed")
	}

	// Full one-child convergence capacity before ownership transfer begins.
	if err := checkChildHandoffAdmissionCapacity(detail.Events); err != nil {
		return ChildRunIdentity{}, err
	}

	childID, err := newChildID()
	if err != nil {
		return ChildRunIdentity{}, err
	}
	handoffID, err := newHandoffID()
	if err != nil {
		return ChildRunIdentity{}, err
	}
	childOwner := childOwnerPrefix + childID
	prevFence := primary.Fence
	proposedFence := prevFence + 1

	proposePayload := map[string]any{
		"handoff_id":     handoffID,
		"run_id":         primary.RunID,
		"child_id":       childID,
		"direction":      HandoffDirectionParentToChild,
		"from_owner":     primary.Owner,
		"to_owner":       childOwner,
		"previous_fence": prevFence,
		"proposed_fence": proposedFence,
	}
	if err := r.appendLocked(primary.TaskID, EventHandoffProposed, primary.Owner, "operator", primary.Owner, proposePayload); err != nil {
		return ChildRunIdentity{}, err
	}

	fence, err := ls.CommitHandoffFrom(primary.Owner, primary.Fence, childOwner)
	if err != nil {
		_ = r.appendLocked(primary.TaskID, EventHandoffFailed, primary.Owner, "operator", primary.Owner, map[string]any{
			"handoff_id": handoffID,
			"run_id":     primary.RunID,
			"child_id":   childID,
			"direction":  HandoffDirectionParentToChild,
			"reason":     clip(err.Error(), maxFieldLen),
			"stage":      "cas",
		})
		return ChildRunIdentity{}, InvalidTransition(err.Error())
	}
	if fence != proposedFence {
		_ = r.appendLocked(primary.TaskID, EventHandoffFailed, childOwner, "operator", "kernel", map[string]any{
			"handoff_id": handoffID,
			"run_id":     primary.RunID,
			"reason":     "unexpected_fencing_token",
			"stage":      "cas",
		})
		return ChildRunIdentity{}, InvalidTransition("unexpected fencing token after handoff CAS")
	}

	commitPayload := map[string]any{
		"handoff_id":     handoffID,
		"run_id":         primary.RunID,
		"child_id":       childID,
		"direction":      HandoffDirectionParentToChild,
		"new_owner":      childOwner,
		"previous_fence": prevFence,
		"fencing_token":  fence,
	}
	if err := r.appendLocked(primary.TaskID, EventHandoffCommitted, childOwner, "operator", childOwner, commitPayload); err != nil {
		// Lease already at child N+1; do NOT CAS-rollback (would produce unrecoverable N+2).
		// Leave HandoffProposed open so recovery can match lease toOwner@proposedFence.
		return ChildRunIdentity{}, err
	}

	startPayload := map[string]any{
		"run_id":        primary.RunID,
		"child_id":      childID,
		"handoff_id":    handoffID,
		"owner":         childOwner,
		"fencing_token": fence,
		"status":        "running",
		"kind":          "child",
		"model":         childModel,
	}
	if err := r.appendLocked(primary.TaskID, EventChildRunStarted, childOwner, "operator", childOwner, startPayload); err != nil {
		_ = r.appendLocked(primary.TaskID, EventHandoffFailed, childOwner, "operator", "kernel", map[string]any{
			"handoff_id":    handoffID,
			"run_id":        primary.RunID,
			"child_id":      childID,
			"fencing_token": fence,
			"reason":        "child_started_append_failed",
			"stage":         "child_started",
		})
		return ChildRunIdentity{}, err
	}
	return ChildRunIdentity{
		ChildID:   childID,
		RunID:     primary.RunID,
		TaskID:    primary.TaskID,
		Owner:     childOwner,
		Fence:     fence,
		Model:     childModel,
		HandoffID: handoffID,
	}, nil
}

// CompleteChildHandoff records ChildRunCompleted then durable return handoff (N+1→N+2).
func (r *Repo) CompleteChildHandoff(child ChildRunIdentity, primaryOwner string) (RunIdentity, error) {
	return r.terminalChildHandoff(child, primaryOwner, EventChildRunCompleted, "completed", "")
}

// FailChildHandoff records ChildRunFailed then durable return handoff.
func (r *Repo) FailChildHandoff(child ChildRunIdentity, primaryOwner, reason string) (RunIdentity, error) {
	return r.terminalChildHandoff(child, primaryOwner, EventChildRunFailed, "failed", reason)
}

// CancelChildHandoff records ChildRunCancelled then durable return handoff.
func (r *Repo) CancelChildHandoff(child ChildRunIdentity, primaryOwner, reason string) (RunIdentity, error) {
	if reason == "" {
		reason = "operator_cancelled"
	}
	return r.terminalChildHandoff(child, primaryOwner, EventChildRunCancelled, "cancelled", reason)
}

// InterruptChildHandoff records ChildRunInterrupted then durable return handoff.
func (r *Repo) InterruptChildHandoff(child ChildRunIdentity, primaryOwner, reason string) (RunIdentity, error) {
	if reason == "" {
		reason = "interrupted"
	}
	return r.terminalChildHandoff(child, primaryOwner, EventChildRunInterrupted, "interrupted", reason)
}

func (r *Repo) terminalChildHandoff(child ChildRunIdentity, primaryOwner, eventType, status, reason string) (RunIdentity, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	detail, err := r.getLocked(child.TaskID)
	if err != nil {
		return RunIdentity{}, err
	}
	if detail.Projection.Removed {
		return RunIdentity{}, ErrRemoved
	}
	if err := validateChildClaim(child); err != nil {
		return RunIdentity{}, err
	}
	active, ok := activeChildRunIdentity(detail.Events)
	if !ok || active.ChildID != child.ChildID || active.RunID != child.RunID {
		return RunIdentity{}, InvalidTransition("no active child run")
	}
	if active.Owner != child.Owner || active.Fence != child.Fence {
		return RunIdentity{}, InvalidTransition("stale child identity")
	}
	primaryOwner, err = validateIdentity(primaryOwner, "owner")
	if err != nil {
		return RunIdentity{}, err
	}
	if primaryOwner == "" {
		return RunIdentity{}, InvalidTask("owner is required")
	}
	ls := r.leases(child.TaskID)
	if err := ls.CheckOwnerAndFence(child.Owner, child.Fence); err != nil {
		return RunIdentity{}, InvalidTransition(err.Error())
	}
	if err := checkEventCapacity(detail.Events, eventType); err != nil {
		return RunIdentity{}, err
	}
	probe := append([]Event{}, detail.Events...)
	probe = append(probe, Event{EventType: eventType})
	if err := checkEventCapacity(probe, EventHandoffProposed); err != nil {
		return RunIdentity{}, err
	}
	probe = append(probe, Event{EventType: EventHandoffProposed}, Event{EventType: EventHandoffCommitted})
	if err := checkEventCapacity(probe, EventRunCompleted); err != nil {
		return RunIdentity{}, CapacityExceeded("task event capacity exceeded")
	}

	// Prefer child terminal while child authority is still provable (before return CAS).
	childPayload := map[string]any{
		"run_id":        child.RunID,
		"child_id":      child.ChildID,
		"handoff_id":    child.HandoffID,
		"owner":         child.Owner,
		"fencing_token": child.Fence,
		"status":        status,
		"kind":          "child",
	}
	if reason != "" {
		childPayload["reason"] = clip(reason, maxFieldLen)
	}
	if err := r.appendLocked(child.TaskID, eventType, child.Owner, "operator", child.Owner, childPayload); err != nil {
		return RunIdentity{}, err
	}

	handoffID, err := newHandoffID()
	if err != nil {
		return RunIdentity{}, err
	}
	prevFence := child.Fence
	proposedFence := prevFence + 1
	proposePayload := map[string]any{
		"handoff_id":     handoffID,
		"run_id":         child.RunID,
		"child_id":       child.ChildID,
		"direction":      HandoffDirectionChildToParent,
		"from_owner":     child.Owner,
		"to_owner":       primaryOwner,
		"previous_fence": prevFence,
		"proposed_fence": proposedFence,
	}
	if err := r.appendLocked(child.TaskID, EventHandoffProposed, child.Owner, "operator", child.Owner, proposePayload); err != nil {
		return RunIdentity{}, err
	}

	fence, err := ls.CommitHandoffFrom(child.Owner, child.Fence, primaryOwner)
	if err != nil {
		_ = r.appendLocked(child.TaskID, EventHandoffFailed, child.Owner, "operator", child.Owner, map[string]any{
			"handoff_id": handoffID,
			"run_id":     child.RunID,
			"child_id":   child.ChildID,
			"direction":  HandoffDirectionChildToParent,
			"reason":     clip(err.Error(), maxFieldLen),
			"stage":      "cas",
		})
		return RunIdentity{}, InvalidTransition(err.Error())
	}

	commitPayload := map[string]any{
		"handoff_id":     handoffID,
		"run_id":         child.RunID,
		"child_id":       child.ChildID,
		"direction":      HandoffDirectionChildToParent,
		"new_owner":      primaryOwner,
		"previous_fence": prevFence,
		"fencing_token":  fence,
	}
	if err := r.appendLocked(child.TaskID, EventHandoffCommitted, primaryOwner, "operator", primaryOwner, commitPayload); err != nil {
		// Lease already at parent N+2; do NOT CAS-rollback.
		// Leave return HandoffProposed open for recovery against lease authority.
		return RunIdentity{}, err
	}
	return RunIdentity{RunID: child.RunID, TaskID: child.TaskID, Owner: primaryOwner, Fence: fence}, nil
}

// childHandoffAdmissionOrdinaryEvents is ChildRunStarted plus the one optional
// RunProgress("child") the live path emits after ownership transfer.
const childHandoffAdmissionOrdinaryEvents = 2

// childHandoffAdmissionControlEvents is the minimum control headroom for one
// executable child after admission: outbound Proposed+Committed, child terminal,
// return Proposed+Committed, primary terminal (6). Failure/recovery alternatives
// (HandoffFailed, HandoffRolledBack, ChildRunInterrupted, RunInterrupted) fit
// within the same bound and do not require raising lifecycleEventReserve (8).
const childHandoffAdmissionControlEvents = 6

// checkChildHandoffAdmissionCapacity reserves worst-case bounded convergence
// capacity AFTER ownership transfer begins for ONE executable child.
// Progress is included in the ordinary budget so it cannot steal terminal
// control headroom. Failure paths are verified to fit within the same budget.
func checkChildHandoffAdmissionCapacity(events []Event) error {
	probe := append([]Event{}, events...)

	// outbound Proposed + Committed
	if err := checkEventCapacity(probe, EventHandoffProposed); err != nil {
		return err
	}
	probe = append(probe, Event{EventType: EventHandoffProposed})
	if err := checkEventCapacity(probe, EventHandoffCommitted); err != nil {
		return err
	}
	probe = append(probe, Event{EventType: EventHandoffCommitted})

	// ChildRunStarted (ordinary)
	if err := checkEventCapacity(probe, EventChildRunStarted); err != nil {
		return err
	}
	probe = append(probe, Event{EventType: EventChildRunStarted})

	// optional bounded child progress the implementation actually emits
	if err := checkEventCapacity(probe, EventRunProgress); err != nil {
		return err
	}
	probe = append(probe, Event{EventType: EventRunProgress})

	// ChildRun terminal
	if err := checkEventCapacity(probe, EventChildRunCompleted); err != nil {
		return CapacityExceeded("task event capacity exceeded")
	}
	probe = append(probe, Event{EventType: EventChildRunCompleted})

	// return Proposed + Committed
	if err := checkEventCapacity(probe, EventHandoffProposed); err != nil {
		return err
	}
	probe = append(probe, Event{EventType: EventHandoffProposed})
	if err := checkEventCapacity(probe, EventHandoffCommitted); err != nil {
		return err
	}
	probe = append(probe, Event{EventType: EventHandoffCommitted})

	// primary terminal
	if err := checkEventCapacity(probe, EventRunCompleted); err != nil {
		return CapacityExceeded("task event capacity exceeded")
	}

	// Failure/recovery headroom (must fit within the same reserved budget).
	// D: outbound CAS failure after Proposed -> HandoffFailed + RunInterrupted
	failProbe := append([]Event{}, events...)
	failProbe = append(failProbe, Event{EventType: EventHandoffProposed})
	if err := checkEventCapacity(failProbe, EventHandoffFailed); err != nil {
		return err
	}
	failProbe = append(failProbe, Event{EventType: EventHandoffFailed})
	if err := checkEventCapacity(failProbe, EventRunInterrupted); err != nil {
		return CapacityExceeded("task event capacity exceeded")
	}

	// E: outbound post-CAS append failure (Proposed open) -> RolledBack + RunInterrupted
	rbProbe := append([]Event{}, events...)
	rbProbe = append(rbProbe, Event{EventType: EventHandoffProposed})
	if err := checkEventCapacity(rbProbe, EventHandoffRolledBack); err != nil {
		return err
	}
	rbProbe = append(rbProbe, Event{EventType: EventHandoffRolledBack})
	if err := checkEventCapacity(rbProbe, EventRunInterrupted); err != nil {
		return CapacityExceeded("task event capacity exceeded")
	}

	// F: return post-CAS append failure after child terminal + return Proposed
	// needs RolledBack + RunInterrupted (2 control) — already reserved by return
	// Committed + primary terminal in the success probe above.
	// G: cancel/shutdown while child-owned uses ChildRunCancelled + return
	// Proposed+Committed + RunCancelled — same 4 control slots as child terminal
	// + return Proposed+Committed + primary terminal.

	return nil
}

func validateChildClaim(child ChildRunIdentity) error {
	if strings.TrimSpace(child.TaskID) == "" || strings.TrimSpace(child.RunID) == "" || strings.TrimSpace(child.ChildID) == "" {
		return InvalidTransition("malformed child identity")
	}
	if strings.TrimSpace(child.Owner) == "" || child.Fence <= 0 {
		return InvalidTransition("malformed child identity")
	}
	return nil
}

// activeChildRunIdentity returns the open ChildRunStarted identity when present.
func activeChildRunIdentity(events []Event) (ChildRunIdentity, bool) {
	var id ChildRunIdentity
	var ok bool
	for _, ev := range events {
		pl := payloadMap(ev.Payload)
		switch ev.EventType {
		case EventChildRunStarted:
			childID := payloadString(pl, "child_id", "childId")
			runID := payloadString(pl, "run_id", "runId")
			owner := firstNonEmpty(payloadString(pl, "owner"), ev.RuntimeSessionID, ev.ActorID)
			fence := payloadInt(pl, "fencing_token", "fencingToken")
			model := payloadString(pl, "model")
			handoffID := payloadString(pl, "handoff_id", "handoffId")
			if childID != "" && runID != "" && owner != "" && fence > 0 {
				id = ChildRunIdentity{ChildID: childID, RunID: runID, TaskID: ev.TaskID, Owner: owner, Fence: fence, Model: model, HandoffID: handoffID}
				ok = true
			} else {
				id = ChildRunIdentity{}
				ok = false
			}
		case EventChildRunCompleted, EventChildRunFailed, EventChildRunCancelled, EventChildRunInterrupted:
			id = ChildRunIdentity{}
			ok = false
		}
	}
	return id, ok
}

func hasPriorChildRunStarted(events []Event, runID string) bool {
	for _, ev := range events {
		if ev.EventType != EventChildRunStarted {
			continue
		}
		pl := payloadMap(ev.Payload)
		evRun := payloadString(pl, "run_id", "runId")
		if runID == "" || evRun == "" || evRun == runID {
			return true
		}
	}
	return false
}

func newChildID() (string, error) {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func newHandoffID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "ho_" + hex.EncodeToString(b[:]), nil
}

// ValidateDelegateInstruction enforces the 16 KiB instruction cap without
// retaining the instruction in Fabric state.
func ValidateDelegateInstruction(instruction string) error {
	if len(instruction) == 0 {
		return InvalidTask("instruction is required")
	}
	if len(instruction) > MaxDelegateInstructionBytes {
		return InvalidTask(fmt.Sprintf("instruction exceeds %d bytes", MaxDelegateInstructionBytes))
	}
	return nil
}

// LeaseSnapshot returns the current lease owner and fencing token.
func (r *Repo) LeaseSnapshot(taskID string) (owner string, fence int, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ls := r.leases(taskID)
	s, err := ls.read()
	if err != nil {
		return "", 0, err
	}
	return s.Owner, s.FencingToken, nil
}
