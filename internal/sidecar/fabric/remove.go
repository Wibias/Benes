package fabric

import (
	"fmt"
	"strings"
)

const (
	OutcomeExecuted = "executed"
	OutcomeAlready  = "already"
	OutcomeRejected = "rejected"

	InitialInputNone      InitialInputStatus = "none"
	InitialInputReserved  InitialInputStatus = "reserved"
	InitialInputSent      InitialInputStatus = "sent"
	InitialInputUncertain InitialInputStatus = "uncertain"
	InitialInputAbandoned InitialInputStatus = "abandoned"
)

type Identity struct {
	Principal            string
	ExpectedOwner        string
	ExpectedFencingToken int
}

type Outcome struct {
	Status string
	Reason string
}

type InitialInputStatus string

type InitialInput struct {
	Status            InitialInputStatus
	ReservedSessionID string
}

func (r *Repo) RemoveTask(taskID string, id Identity) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.removeLocked(taskID, &id)
}

// Delete is the operator HTTP remove path. It does not take a fencing token
// from the caller; the kernel lock is the authority.
func (r *Repo) Delete(taskID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.removeLocked(taskID, nil)
}

func (r *Repo) removeLocked(taskID string, id *Identity) error {
	detail, err := r.getLocked(taskID)
	if err != nil {
		return err
	}
	if detail.Projection.Removed {
		return nil
	}
	if id != nil {
		if err := checkIdentity("remove", *id, detail.Projection); err != nil {
			return err
		}
	}
	if hasActivePrimaryRun(detail.Events) {
		return ErrActiveRun
	}
	actor := "kernel"
	if id != nil && strings.TrimSpace(id.Principal) != "" {
		actor = id.Principal
	} else {
		actor = "operator"
	}
	return r.appendLocked(taskID, EventTaskRemoved, "", "operator", actor, map[string]any{
		"reason": "operator_removed",
		"status": "removed",
	})
}

func (r *Repo) CompleteTask(taskID string, id Identity) error {
	return r.appendLifecycle(taskID, EventTaskCompleted, id)
}

func (r *Repo) CancelTask(taskID string, id Identity) error {
	r.mu.Lock()
	detail, err := r.getLocked(taskID)
	if err != nil {
		r.mu.Unlock()
		return err
	}
	active := hasActivePrimaryRun(detail.Events)
	run := activeRunIdentity(detail.Events)
	r.mu.Unlock()
	if active && run.RunID != "" {
		// Prefer current lease/projection authority (child N+1 or returned N+2).
		claimOwner, claimFence := detail.Projection.CurrentOwner, detail.Projection.FencingToken
		if child, ok := activeChildRunIdentity(detail.Events); ok {
			claimOwner, claimFence = child.Owner, child.Fence
		} else if o, f, ok := latestHandoffCommittedClaim(detail.Events); ok {
			claimOwner, claimFence = o, f
		}
		if claimOwner == "" || claimFence <= 0 {
			claimOwner, claimFence = run.Owner, run.Fence
		}
		if id.ExpectedOwner == "" {
			id.ExpectedOwner = claimOwner
		}
		if id.ExpectedFencingToken == 0 && claimFence != 0 {
			id.ExpectedFencingToken = claimFence
		}
		if id.ExpectedOwner != claimOwner || id.ExpectedFencingToken != claimFence {
			return InvalidTransition("fencing mismatch")
		}
		return r.CancelExecute(RunIdentity{
			RunID:  run.RunID,
			TaskID: taskID,
			Owner:  claimOwner,
			Fence:  claimFence,
		}, "operator_cancelled")
	}
	return r.appendLifecycle(taskID, EventTaskCancelled, id)
}

func (r *Repo) appendLifecycle(taskID, eventType string, id Identity) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	detail, err := r.getLocked(taskID)
	if err != nil {
		return err
	}
	if detail.Projection.Removed {
		return ErrRemoved
	}
	if detail.Projection.Terminal {
		return InvalidTransition("task already reached a terminal lifecycle state")
	}
	for _, ev := range detail.Events {
		if ev.EventType == EventTaskCompleted || ev.EventType == EventTaskCancelled {
			return InvalidTransition("task already reached a terminal lifecycle state")
		}
	}
	if hasActivePrimaryRun(detail.Events) {
		return InvalidTransition("cannot mutate task while the primary run is active")
	}
	if err := checkIdentity("lifecycle", id, detail.Projection); err != nil {
		return err
	}
	payload := map[string]any{"status": strings.ToLower(strings.TrimPrefix(eventType, "Task"))}
	if eventType == EventTaskCancelled {
		payload["reason"] = "operator_cancelled"
	}
	actor, err := validateIdentity(id.Principal, "principal")
	if err != nil {
		return err
	}
	if actor == "" {
		actor = "kernel"
	}
	return r.appendLocked(taskID, eventType, "", "operator", actor, payload)
}

func (r *Repo) InitialInputState(taskID string) (InitialInput, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	detail, err := r.getLocked(taskID)
	if err != nil {
		return InitialInput{Status: InitialInputNone}, err
	}
	return initialInputFrom(detail.Events), nil
}

func (r *Repo) ReserveInitialInput(taskID, sessionID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	detail, err := r.getLocked(taskID)
	if err != nil {
		return err
	}
	st := initialInputFrom(detail.Events)
	switch st.Status {
	case InitialInputSent:
		return fmt.Errorf("initial input already sent")
	case InitialInputUncertain:
		return fmt.Errorf("initial input state is uncertain; operator must explicitly abandon before a fresh attempt")
	case InitialInputReserved:
		return nil
	}
	return r.appendLocked(taskID, EventInitialInputReserved, sessionID, "operator", "kernel", map[string]any{
		"session_id": sessionID,
		"status":     "reserved",
	})
}

func (r *Repo) ConfirmInitialInputSent(taskID, sessionID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	detail, err := r.getLocked(taskID)
	if err != nil {
		return err
	}
	if initialInputFrom(detail.Events).Status == InitialInputSent {
		return nil
	}
	return r.appendLocked(taskID, EventInitialInputSent, sessionID, "operator", "kernel", map[string]any{
		"session_id": sessionID,
		"status":     "sent",
	})
}

func (r *Repo) MarkInitialInputUncertain(taskID, sessionID, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, err := r.getLocked(taskID); err != nil {
		return err
	}
	return r.appendLocked(taskID, EventInitialInputUncertain, sessionID, "operator", "kernel", map[string]any{
		"session_id": sessionID,
		"status":     "uncertain",
		"reason":     clip(reason, maxFieldLen),
	})
}

func (r *Repo) AbandonInitialInput(taskID, sessionID string, id Identity) Outcome {
	r.mu.Lock()
	defer r.mu.Unlock()
	detail, err := r.getLocked(taskID)
	if err != nil {
		return Outcome{Status: OutcomeRejected, Reason: err.Error()}
	}
	if err := checkIdentity("abandon", id, detail.Projection); err != nil {
		return Outcome{Status: OutcomeRejected, Reason: err.Error()}
	}
	state := "none"
	for i := len(detail.Events) - 1; i >= 0; i-- {
		switch detail.Events[i].EventType {
		case EventInitialInputAbandoned:
			return Outcome{Status: OutcomeAlready}
		case EventInitialInputSent:
			return Outcome{Status: OutcomeRejected, Reason: "initial input already sent; nothing to abandon"}
		case EventInitialInputUncertain:
			state = "uncertain"
			i = -1
		case EventInitialInputReserved:
			state = "reserved"
			i = -1
		}
	}
	if state == "none" {
		return Outcome{Status: OutcomeRejected, Reason: "no pending initial input to abandon"}
	}
	actor := id.Principal
	if actor == "" {
		actor = "kernel"
	}
	if err := r.appendLocked(taskID, EventInitialInputAbandoned, sessionID, "operator", actor, map[string]any{
		"session_id":      sessionID,
		"abandoned_state": state,
		"reason":          "operator_abandoned",
		"status":          "abandoned",
	}); err != nil {
		return Outcome{Status: OutcomeRejected, Reason: err.Error()}
	}
	return Outcome{Status: OutcomeExecuted}
}

func initialInputFrom(events []Event) InitialInput {
	out := InitialInput{Status: InitialInputNone}
	for i := len(events) - 1; i >= 0; i-- {
		pl := payloadMap(events[i].Payload)
		sid := payloadString(pl, "session_id", "sessionId")
		switch events[i].EventType {
		case EventInitialInputSent:
			return InitialInput{Status: InitialInputSent, ReservedSessionID: sid}
		case EventInitialInputUncertain:
			return InitialInput{Status: InitialInputUncertain, ReservedSessionID: sid}
		case EventInitialInputReserved:
			return InitialInput{Status: InitialInputReserved, ReservedSessionID: sid}
		case EventInitialInputAbandoned:
			return InitialInput{Status: InitialInputAbandoned, ReservedSessionID: sid}
		}
	}
	return out
}

func checkIdentity(op string, id Identity, p Projection) error {
	if id.ExpectedOwner != p.CurrentOwner {
		if id.ExpectedOwner == "" {
			return InvalidTransition(op + " requires expectedOwner")
		}
		return InvalidTransition("ownership changed")
	}
	if id.ExpectedFencingToken != p.FencingToken {
		return InvalidTransition("fencing mismatch")
	}
	return nil
}
