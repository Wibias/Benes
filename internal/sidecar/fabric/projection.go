package fabric

import (
	"fmt"
	"strings"
)

const ProjectionSchema = "1.0.0"

// Projection is rebuilt current state. It is a pure function of the log.
type Projection struct {
	TaskID       string            `json:"taskId"`
	Title        string            `json:"title"`
	Goal         string            `json:"goal"`
	CurrentOwner string            `json:"currentOwner"`
	FencingToken int               `json:"fencingToken"`
	Workspaces   map[string]string `json:"workspaces"`
	AcceptedHash string            `json:"acceptedHash"`
	Permissions  map[string]string `json:"permissions"`
	Claims       map[string]string `json:"claims"`
	Terminal     bool              `json:"terminal"`
	Removed      bool              `json:"removed"`
}

type TaskSummary struct {
	TaskID         string `json:"taskId"`
	Title          string `json:"title"`
	TaskState      string `json:"taskState"`
	CurrentOwner   string `json:"currentOwner"`
	RunState       string `json:"runState"`
	LatestActivity int64  `json:"latestActivity"`
	EventCount     int    `json:"eventCount"`
	Terminal       bool   `json:"terminal"`
	CompletionGate string `json:"completionGate"`
}

type TimelineEntry struct {
	Sequence   uint64         `json:"sequence"`
	EventType  string         `json:"eventType"`
	OccurredAt int64          `json:"occurredAt"`
	ActorType  string         `json:"actorType"`
	ActorID    string         `json:"actorId"`
	Category   string         `json:"category"`
	Payload    map[string]any `json:"payload"`
}

type TaskDetail struct {
	Summary    TaskSummary     `json:"summary"`
	Projection Projection      `json:"projection"`
	Timeline   []TimelineEntry `json:"timeline"`
	Events     []Event         `json:"events,omitempty"`
}

var eventCategories = map[string]string{
	EventTaskCreated:           "lifecycle",
	EventTaskCompleted:         "lifecycle",
	EventTaskCancelled:         "lifecycle",
	EventTaskRemoved:           "lifecycle",
	EventRunStarted:            "lifecycle",
	EventRunCompleted:          "lifecycle",
	EventRunFailed:             "lifecycle",
	EventRunInterrupted:        "lifecycle",
	EventRunCancelled:          "lifecycle",
	EventRunProgress:           "lifecycle",
	EventChildRunStarted:       "handoffs",
	EventChildRunCompleted:     "handoffs",
	EventChildRunFailed:        "handoffs",
	EventChildRunCancelled:     "handoffs",
	EventChildRunInterrupted:   "handoffs",
	EventApprovalRequested:     "approvals",
	EventApprovalResolved:      "approvals",
	EventHandoffProposed:       "handoffs",
	EventHandoffCommitted:      "handoffs",
	EventHandoffRolledBack:     "handoffs",
	EventHandoffFailed:         "handoffs",
	EventLeaseLost:             "security",
	EventClaimValidated:        "evidence",
	EventWorkspaceRegistered:   "workspaces",
	EventSessionStarted:        "sessions",
	EventSessionClosed:         "sessions",
	EventInitialInputReserved:  "lifecycle",
	EventInitialInputSent:      "lifecycle",
	EventInitialInputUncertain: "lifecycle",
	EventInitialInputAbandoned: "lifecycle",
}

// Rebuild verifies the hash chain and projects current state from events.
func Rebuild(events []Event) (*Projection, error) {
	p := &Projection{
		Workspaces:  map[string]string{},
		Permissions: map[string]string{},
		Claims:      map[string]string{},
	}
	prevHash := ""
	for i, ev := range events {
		if ev.Sequence != uint64(i+1) {
			return nil, fmt.Errorf("sequence gap at %d", i+1)
		}
		if ev.PreviousEventHash != prevHash {
			return nil, fmt.Errorf("hash chain broken at sequence %d", ev.Sequence)
		}
		h, err := HashEvent(ev)
		if err != nil {
			return nil, err
		}
		if h != ev.EventHash {
			return nil, fmt.Errorf("event hash mismatch at sequence %d", ev.Sequence)
		}
		prevHash = ev.EventHash
		apply(p, ev)
	}
	if err := validateHandoffFenceProgression(events); err != nil {
		return nil, err
	}
	return p, nil
}

// validateHandoffFenceProgression fail-closes on duplicate/skipped/backwards
// generation, wrong runId, mismatched handoffId, or child terminal for a
// different active child. Events WITH handoff_id (Proposed/Committed/ChildRun*
// /return) must form a coherent semantic chain. Legacy HandoffCommitted without
// handoff_id stays valid (monotonic bump only).
func validateHandoffFenceProgression(events []Event) error {
	primaryRun := ""
	primaryFence := 0
	var openProp struct {
		id, direction, runID, fromOwner, toOwner string
		prev, next                               int
		active                                   bool
	}
	activeChild := ""
	activeChildHandoff := ""
	committedFence := 0
	lastCommittedHandoff := ""
	lastCommittedDirection := ""
	seenCommit := map[string]bool{}

	for _, ev := range events {
		pl := payloadMap(ev.Payload)
		switch ev.EventType {
		case EventRunStarted:
			if payloadString(pl, "kind") == "execute" {
				primaryRun = payloadString(pl, "run_id", "runId")
				primaryFence = payloadInt(pl, "fencing_token", "fencingToken")
				committedFence = primaryFence
				activeChild = ""
				activeChildHandoff = ""
				lastCommittedHandoff = ""
				lastCommittedDirection = ""
			}
		case EventHandoffProposed:
			hid := payloadString(pl, "handoff_id", "handoffId")
			dir := payloadString(pl, "direction")
			runID := payloadString(pl, "run_id", "runId")
			prev := payloadInt(pl, "previous_fence", "previousFence")
			next := payloadInt(pl, "proposed_fence", "proposedFence")
			fromOwner := payloadString(pl, "from_owner", "fromOwner")
			toOwner := payloadString(pl, "to_owner", "toOwner")
			if hid == "" {
				continue // legacy-compatible skip
			}
			if openProp.active {
				return fmt.Errorf("duplicate open handoff proposal")
			}
			if primaryRun != "" && runID != "" && runID != primaryRun {
				return fmt.Errorf("handoff runId mismatch")
			}
			if next != prev+1 || prev < 0 {
				return fmt.Errorf("handoff proposed fence progression invalid")
			}
			if committedFence > 0 && prev != committedFence {
				return fmt.Errorf("handoff proposed previous_fence mismatch")
			}
			if dir == HandoffDirectionParentToChild {
				if activeChild != "" {
					return fmt.Errorf("parent_to_child propose while child active")
				}
			} else if dir == HandoffDirectionChildToParent {
				// Child terminal is recorded before return propose; require a prior
				// parent_to_child commit and no still-active child.
				if activeChild != "" {
					return fmt.Errorf("child_to_parent propose while child still active")
				}
				if lastCommittedDirection != "" && lastCommittedDirection != HandoffDirectionParentToChild {
					return fmt.Errorf("child_to_parent propose without parent_to_child commit")
				}
			} else if dir != "" {
				return fmt.Errorf("unknown handoff direction")
			}
			openProp.id, openProp.direction, openProp.runID = hid, dir, runID
			openProp.fromOwner, openProp.toOwner = fromOwner, toOwner
			openProp.prev, openProp.next, openProp.active = prev, next, true
		case EventHandoffCommitted:
			hid := payloadString(pl, "handoff_id", "handoffId")
			tok := payloadInt(pl, "fencing_token", "fencingToken")
			runID := payloadString(pl, "run_id", "runId")
			newOwner := payloadString(pl, "new_owner", "newOwner")
			if hid == "" {
				// Legacy commit: monotonic bump only.
				if tok > 0 {
					if committedFence > 0 && tok < committedFence {
						return fmt.Errorf("handoff fencing went backwards")
					}
					committedFence = tok
				} else if committedFence > 0 {
					committedFence++
				}
				continue
			}
			if seenCommit[hid] {
				return fmt.Errorf("duplicate handoff commit")
			}
			if !openProp.active || openProp.id != hid {
				return fmt.Errorf("handoff commit without matching proposal")
			}
			if primaryRun != "" && runID != "" && runID != primaryRun {
				return fmt.Errorf("handoff commit runId mismatch")
			}
			prev := payloadInt(pl, "previous_fence", "previousFence")
			if prev != openProp.prev || tok != openProp.next {
				return fmt.Errorf("handoff commit fence progression invalid")
			}
			if committedFence > 0 && tok != committedFence+1 && tok != openProp.next {
				return fmt.Errorf("handoff commit skipped or duplicate generation")
			}
			if tok <= committedFence && committedFence > 0 {
				return fmt.Errorf("handoff fencing went backwards")
			}
			if openProp.toOwner != "" && newOwner != "" && openProp.toOwner != newOwner {
				return fmt.Errorf("handoff commit new_owner mismatch")
			}
			dir := payloadString(pl, "direction")
			if dir == "" {
				dir = openProp.direction
			}
			if openProp.direction != "" && dir != "" && dir != openProp.direction {
				return fmt.Errorf("handoff commit direction mismatch")
			}
			seenCommit[hid] = true
			committedFence = tok
			lastCommittedHandoff = hid
			lastCommittedDirection = dir
			openProp.active = false
			if dir == HandoffDirectionChildToParent {
				// Return commit ends child authority; child terminal should already
				// have cleared activeChild, but tolerate commit-after-terminal.
				activeChild = ""
				activeChildHandoff = ""
			}
		case EventHandoffFailed, EventHandoffRolledBack:
			hid := payloadString(pl, "handoff_id", "handoffId")
			if openProp.active && (hid == "" || hid == openProp.id) {
				openProp.active = false
			}
		case EventChildRunStarted:
			childID := payloadString(pl, "child_id", "childId")
			runID := payloadString(pl, "run_id", "runId")
			tok := payloadInt(pl, "fencing_token", "fencingToken")
			hid := payloadString(pl, "handoff_id", "handoffId")
			if primaryRun != "" && runID != "" && runID != primaryRun {
				return fmt.Errorf("child runId mismatch")
			}
			if activeChild != "" {
				return fmt.Errorf("duplicate active child")
			}
			if committedFence > 0 && tok > 0 && tok != committedFence {
				return fmt.Errorf("child fencing mismatch")
			}
			if hid != "" {
				if lastCommittedHandoff == "" || hid != lastCommittedHandoff {
					return fmt.Errorf("child handoff_id mismatch")
				}
				if lastCommittedDirection != "" && lastCommittedDirection != HandoffDirectionParentToChild {
					return fmt.Errorf("child start without parent_to_child commit")
				}
			}
			activeChild = childID
			activeChildHandoff = hid
		case EventChildRunCompleted, EventChildRunFailed, EventChildRunCancelled, EventChildRunInterrupted:
			childID := payloadString(pl, "child_id", "childId")
			hid := payloadString(pl, "handoff_id", "handoffId")
			if activeChild == "" {
				return fmt.Errorf("child terminal without active child")
			}
			if childID != "" && childID != activeChild {
				return fmt.Errorf("child terminal for different active child")
			}
			if hid != "" && activeChildHandoff != "" && hid != activeChildHandoff {
				return fmt.Errorf("child terminal handoff_id mismatch")
			}
			activeChild = ""
			activeChildHandoff = ""
		}
	}
	_ = primaryFence
	return nil
}

func apply(p *Projection, ev Event) {
	p.TaskID = ev.TaskID
	pl := payloadMap(ev.Payload)
	switch ev.EventType {
	case EventTaskCreated:
		p.AcceptedHash = payloadString(pl, "acceptance_criteria_hash", "acceptanceCriteriaHash")
		p.Title = payloadString(pl, "title")
		p.Goal = payloadString(pl, "goal")
	case EventRunStarted:
		if sid := strings.TrimSpace(ev.RuntimeSessionID); sid != "" {
			p.CurrentOwner = sid
			p.Permissions[sid] = "base"
		}
		if owner := payloadString(pl, "owner"); owner != "" && p.CurrentOwner == "" {
			p.CurrentOwner = owner
			p.Permissions[owner] = "base"
		}
		if _, ok := pl["fencing_token"]; ok {
			p.FencingToken = payloadInt(pl, "fencing_token", "fencingToken")
		} else if _, ok := pl["fencingToken"]; ok {
			p.FencingToken = payloadInt(pl, "fencing_token", "fencingToken")
		}
	case EventApprovalResolved:
		sid := payloadString(pl, "runtime_session_id", "runtimeSessionId")
		if sid == "" {
			sid = ev.RuntimeSessionID
		}
		policy := payloadString(pl, "policy")
		if policy == "" {
			policy = "base"
		}
		p.Permissions[sid] = policy
	case EventClaimValidated:
		cid := payloadString(pl, "claim_id", "claimId")
		if cid == "" {
			cid = "?"
		}
		p.Claims[cid] = "validated"
	case EventHandoffProposed:
		// Proposal does not move projection authority; CAS + commit does.
	case EventHandoffCommitted:
		if next := payloadString(pl, "new_owner", "newOwner"); next != "" {
			p.CurrentOwner = next
		}
		if _, ok := pl["fencing_token"]; ok {
			p.FencingToken = payloadInt(pl, "fencing_token", "fencingToken")
		} else if _, ok := pl["fencingToken"]; ok {
			p.FencingToken = payloadInt(pl, "fencing_token", "fencingToken")
		} else {
			p.FencingToken++
		}
	case EventHandoffFailed:
	case EventHandoffRolledBack:
	case EventTaskCompleted, EventTaskCancelled, EventRunCompleted, EventRunFailed, EventRunCancelled:
		p.Terminal = true
	case EventRunInterrupted:
		// Run is no longer active; task stays open for a later execute.
		// After half-commit reconcile, payload carries the latest durably proven
		// authority claim (lease CAS owner/fence). Projection must follow that claim
		// so checkIdentity rejects stale pre-CAS owner/fence. Do not invent authority
		// from HandoffRolledBack alone (pre-CAS never moved).
		if owner := payloadString(pl, "owner"); owner != "" {
			p.CurrentOwner = owner
		}
		if tok := payloadInt(pl, "fencing_token", "fencingToken"); tok > 0 {
			p.FencingToken = tok
		}
	case EventChildRunStarted, EventChildRunCompleted, EventChildRunFailed, EventChildRunCancelled, EventChildRunInterrupted:
		if owner := payloadString(pl, "owner"); owner != "" {
			p.CurrentOwner = owner
		}
		if _, ok := pl["fencing_token"]; ok {
			p.FencingToken = payloadInt(pl, "fencing_token", "fencingToken")
		} else if _, ok := pl["fencingToken"]; ok {
			p.FencingToken = payloadInt(pl, "fencing_token", "fencingToken")
		}
		// ChildRun* never flips primary Terminal / derivePrimaryRunState.
	case EventInitialInputReserved, EventInitialInputSent, EventInitialInputUncertain, EventInitialInputAbandoned:
	case EventTaskRemoved:
		p.Removed = true
		p.Terminal = true
	case EventWorkspaceRegistered:
		if wid := payloadString(pl, "workspace_id", "workspaceId"); wid != "" {
			p.Workspaces[wid] = payloadString(pl, "path")
		}
	}
}

func deriveTaskLifecycle(events []Event) (taskState, gate string) {
	completed, cancelled, runClosed, runFailed, runCancelled := false, false, false, false, false
	for _, ev := range events {
		switch ev.EventType {
		case EventTaskCompleted:
			completed = true
		case EventTaskCancelled:
			cancelled = true
		case EventRunCompleted:
			runClosed = true
		case EventRunFailed:
			runFailed = true
		case EventRunCancelled:
			runCancelled = true
		}
	}
	if completed || runClosed {
		return "completed", "passed"
	}
	if cancelled || runCancelled {
		return "cancelled", "pending"
	}
	if runFailed {
		return "failed", "failed"
	}
	return "open", "pending"
}

func derivePrimaryRunState(events []Event) string {
	runState := ""
	for _, ev := range events {
		switch ev.EventType {
		case EventRunStarted:
			runState = "running"
		case EventRunCompleted:
			runState = "completed"
		case EventRunFailed:
			runState = "failed"
		case EventRunCancelled:
			runState = "cancelled"
		case EventRunInterrupted:
			runState = "interrupted"
		case EventLeaseLost:
			runState = "lost"
		}
	}
	return runState
}

func hasActivePrimaryRun(events []Event) bool {
	s := derivePrimaryRunState(events)
	return s == "running" || s == "starting"
}

func projectTaskSummary(p Projection, events []Event) TaskSummary {
	state, gate := deriveTaskLifecycle(events)
	var latest int64
	for _, ev := range events {
		if ev.OccurredAt > latest {
			latest = ev.OccurredAt
		}
	}
	return TaskSummary{
		TaskID:         p.TaskID,
		Title:          p.Title,
		TaskState:      state,
		CurrentOwner:   p.CurrentOwner,
		RunState:       derivePrimaryRunState(events),
		LatestActivity: latest,
		EventCount:     len(events),
		Terminal:       p.Terminal,
		CompletionGate: gate,
	}
}

func projectTimeline(events []Event) []TimelineEntry {
	out := make([]TimelineEntry, 0, len(events))
	for _, ev := range events {
		cat := eventCategories[ev.EventType]
		if cat == "" {
			cat = "other"
		}
		out = append(out, TimelineEntry{
			Sequence:   ev.Sequence,
			EventType:  ev.EventType,
			OccurredAt: ev.OccurredAt,
			ActorType:  ev.ActorType,
			ActorID:    ev.ActorID,
			Category:   cat,
			Payload:    payloadMap(ev.Payload),
		})
	}
	return out
}
