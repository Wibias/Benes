package fabric

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestHandoff01_CommitHandoffFromCAS(t *testing.T) {
	dir := t.TempDir()
	ls := &Leases{Root: dir, TaskID: "task_cas"}
	if _, err := ls.AcquireWriteLease("primary"); err != nil {
		t.Fatal(err)
	}
	tok, err := ls.CommitHandoffFrom("primary", 0, "child")
	if err != nil || tok != 1 {
		t.Fatalf("hand1 tok=%d err=%v", tok, err)
	}
	if _, err := ls.CommitHandoffFrom("primary", 0, "other"); err == nil {
		t.Fatal("stale owner must fail")
	}
	if _, err := ls.CommitHandoffFrom("child", 0, "other"); err == nil {
		t.Fatal("stale fence must fail")
	}
	tok, err = ls.CommitHandoffFrom("child", 1, "primary")
	if err != nil || tok != 2 {
		t.Fatalf("hand2 tok=%d err=%v", tok, err)
	}
}

func TestHandoff02_SingleChildFenceNtoN2(t *testing.T) {
	repo := New(t.TempDir())
	id, err := repo.Create(CreateTaskInput{Title: "h", Goal: "g"})
	if err != nil {
		t.Fatal(err)
	}
	primary, err := repo.BeginExecute(id, ExecuteInput{Owner: "worker", Model: "p/m"})
	if err != nil {
		t.Fatal(err)
	}
	n := primary.Fence
	child, err := repo.BeginChildHandoff(primary, "p/child")
	if err != nil {
		t.Fatal(err)
	}
	if child.Fence != n+1 || child.RunID != primary.RunID {
		t.Fatalf("child fence=%d run=%s want fence=%d same run", child.Fence, child.RunID, n+1)
	}
	if _, err := repo.BeginChildHandoff(RunIdentity{TaskID: id, RunID: primary.RunID, Owner: child.Owner, Fence: child.Fence}, "p/child2"); CodeOf(err) != CodeInvalidTransition {
		t.Fatalf("second child: %v", err)
	}
	returned, err := repo.CompleteChildHandoff(child, "worker")
	if err != nil {
		t.Fatal(err)
	}
	if returned.Fence != n+2 || returned.Owner != "worker" || returned.RunID != primary.RunID {
		t.Fatalf("returned=%+v", returned)
	}
	if err := repo.CompleteExecute(returned); err != nil {
		t.Fatal(err)
	}
	detail, _ := repo.Get(id)
	types := make([]string, 0, len(detail.Events))
	for _, ev := range detail.Events {
		types = append(types, ev.EventType)
		raw := string(ev.Payload)
		if strings.Contains(raw, "instruction") || strings.Contains(raw, "SECRET_CHILD") {
			t.Fatalf("privacy leak in %s: %s", ev.EventType, raw)
		}
	}
	joined := strings.Join(types, ",")
	if !strings.Contains(joined, EventChildRunStarted) || !strings.Contains(joined, EventChildRunCompleted) {
		t.Fatalf("events=%v", types)
	}
	runStarted := 0
	for _, et := range types {
		if et == EventRunStarted {
			runStarted++
		}
	}
	if runStarted != 1 {
		t.Fatalf("want one primary RunStarted: %v", types)
	}
	if detail.Summary.RunState != "completed" {
		t.Fatalf("runState=%s", detail.Summary.RunState)
	}
}

func TestHandoff03_PrivacyStripsInstructionPayload(t *testing.T) {
	repo := New(t.TempDir())
	id, err := repo.Create(CreateTaskInput{Title: "h", Goal: "g"})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Record(id, EventClaimValidated, "", map[string]any{"claim_id": "c1", "instruction": "nope-secret"}); err != nil {
		t.Fatal(err)
	}
	detail, _ := repo.Get(id)
	for _, ev := range detail.Events {
		if strings.Contains(string(ev.Payload), "instruction") || strings.Contains(string(ev.Payload), "nope-secret") {
			t.Fatalf("instruction leaked: %s", ev.Payload)
		}
	}
}

func TestHandoff04_InstructionCap(t *testing.T) {
	if err := ValidateDelegateInstruction(""); err == nil {
		t.Fatal("empty")
	}
	big := strings.Repeat("a", MaxDelegateInstructionBytes+1)
	if err := ValidateDelegateInstruction(big); err == nil {
		t.Fatal("oversize")
	}
	if err := ValidateDelegateInstruction(strings.Repeat("b", 8)); err != nil {
		t.Fatal(err)
	}
}

func TestHandoff05_ChildCapacityHeadroom(t *testing.T) {
	repo := New(t.TempDir())
	orig := maxOrdinaryEventsPerTask
	// create + runstarted => 2; admission needs ChildRunStarted + RunProgress("child") => +2.
	maxOrdinaryEventsPerTask = 4
	t.Cleanup(func() { maxOrdinaryEventsPerTask = orig })
	id, err := repo.Create(CreateTaskInput{Title: "h", Goal: "g"})
	if err != nil {
		t.Fatal(err)
	}
	primary, err := repo.BeginExecute(id, ExecuteInput{Owner: "worker", Model: "p/m"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := repo.BeginChildHandoff(primary, "p/c")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordProgress(RunIdentity{TaskID: child.TaskID, RunID: child.RunID, Owner: child.Owner, Fence: child.Fence}, "child"); err != nil {
		t.Fatal(err)
	}
	returned, err := repo.CompleteChildHandoff(child, "worker")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CompleteExecute(returned); err != nil {
		t.Fatal(err)
	}
}

func TestHandoff06_RecoverOrphanDuringChild(t *testing.T) {
	repo := New(t.TempDir())
	id, err := repo.Create(CreateTaskInput{Title: "h", Goal: "g"})
	if err != nil {
		t.Fatal(err)
	}
	primary, err := repo.BeginExecute(id, ExecuteInput{Owner: "worker", Model: "p/m"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.BeginChildHandoff(primary, "p/c"); err != nil {
		t.Fatal(err)
	}
	n, err := repo.RecoverOrphans()
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	detail, _ := repo.Get(id)
	if detail.Summary.RunState != "interrupted" {
		t.Fatalf("runState=%s", detail.Summary.RunState)
	}
	var sawChildInterrupt bool
	for _, ev := range detail.Events {
		if ev.EventType == EventChildRunInterrupted {
			sawChildInterrupt = true
		}
	}
	if !sawChildInterrupt {
		t.Fatal("expected ChildRunInterrupted")
	}
}

func TestHandoff07_ExactlyOneChildAfterTerminal(t *testing.T) {
	repo := New(t.TempDir())
	id, err := repo.Create(CreateTaskInput{Title: "h", Goal: "g"})
	if err != nil {
		t.Fatal(err)
	}
	primary, err := repo.BeginExecute(id, ExecuteInput{Owner: "worker", Model: "p/m"})
	if err != nil {
		t.Fatal(err)
	}
	n := primary.Fence
	child, err := repo.BeginChildHandoff(primary, "p/c")
	if err != nil {
		t.Fatal(err)
	}
	returned, err := repo.CompleteChildHandoff(child, "worker")
	if err != nil {
		t.Fatal(err)
	}
	if returned.Fence != n+2 {
		t.Fatalf("fence=%d want %d", returned.Fence, n+2)
	}
	if _, err := repo.BeginChildHandoff(returned, "p/c2"); CodeOf(err) != CodeInvalidTransition {
		t.Fatalf("second child after terminal: %v", err)
	}
}

func TestHandoff08_AppendFailureAfterParentChildCAS(t *testing.T) {
	repo := New(t.TempDir())
	id, err := repo.Create(CreateTaskInput{Title: "h", Goal: "g"})
	if err != nil {
		t.Fatal(err)
	}
	primary, err := repo.BeginExecute(id, ExecuteInput{Owner: "worker", Model: "p/m"})
	if err != nil {
		t.Fatal(err)
	}
	SetAppendLockedHookForTest(func(taskID, eventType string) error {
		if eventType == EventHandoffCommitted {
			return errors.New("injected commit append failure")
		}
		return nil
	})
	t.Cleanup(func() { SetAppendLockedHookForTest(nil) })
	_, err = repo.BeginChildHandoff(primary, "p/c")
	if err == nil {
		t.Fatal("expected append failure")
	}
	owner, fence, _ := repo.LeaseSnapshot(id)
	if !strings.HasPrefix(owner, childOwnerPrefix) || fence != primary.Fence+1 {
		t.Fatalf("lease after half-commit owner=%s fence=%d", owner, fence)
	}
	// Recovery must converge without inventing N+2 rollback.
	n, rerr := repo.RecoverOrphans()
	if rerr != nil || n != 1 {
		t.Fatalf("recover n=%d err=%v", n, rerr)
	}
	detail, _ := repo.Get(id)
	if detail.Summary.RunState != "interrupted" {
		t.Fatalf("runState=%s", detail.Summary.RunState)
	}
}

func TestHandoff09_LeaseWriteFailureBeforeTransfer(t *testing.T) {
	repo := New(t.TempDir())
	id, err := repo.Create(CreateTaskInput{Title: "h", Goal: "g"})
	if err != nil {
		t.Fatal(err)
	}
	primary, err := repo.BeginExecute(id, ExecuteInput{Owner: "worker", Model: "p/m"})
	if err != nil {
		t.Fatal(err)
	}
	leaseAtomicWriteHook = func(string, []byte) error {
		return errors.New("injected lease write failure")
	}
	t.Cleanup(func() { leaseAtomicWriteHook = nil })
	_, err = repo.BeginChildHandoff(primary, "p/c")
	if err == nil {
		t.Fatal("expected lease failure")
	}
	owner, fence, _ := repo.LeaseSnapshot(id)
	if owner != primary.Owner || fence != primary.Fence {
		t.Fatalf("lease mutated owner=%s fence=%d", owner, fence)
	}
}

func TestHandoff10_AppendFailureAfterReturnCAS(t *testing.T) {
	repo := New(t.TempDir())
	id, err := repo.Create(CreateTaskInput{Title: "h", Goal: "g"})
	if err != nil {
		t.Fatal(err)
	}
	primary, err := repo.BeginExecute(id, ExecuteInput{Owner: "worker", Model: "p/m"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := repo.BeginChildHandoff(primary, "p/c")
	if err != nil {
		t.Fatal(err)
	}
	SetAppendLockedHookForTest(func(taskID, eventType string) error {
		if eventType == EventHandoffCommitted {
			return errors.New("injected return commit failure")
		}
		return nil
	})
	t.Cleanup(func() { SetAppendLockedHookForTest(nil) })
	_, err = repo.CompleteChildHandoff(child, "worker")
	if err == nil {
		t.Fatal("expected return commit failure")
	}
	owner, fence, _ := repo.LeaseSnapshot(id)
	if owner != "worker" || fence != primary.Fence+2 {
		t.Fatalf("lease after return half-commit owner=%s fence=%d", owner, fence)
	}
	n, rerr := repo.RecoverOrphans()
	if rerr != nil || n != 1 {
		t.Fatalf("recover n=%d err=%v", n, rerr)
	}
	detail, _ := repo.Get(id)
	if detail.Summary.RunState != "interrupted" {
		t.Fatalf("runState=%s", detail.Summary.RunState)
	}
}

func TestHandoff11_RestartAfterReturnedParentBeforeResume(t *testing.T) {
	repo := New(t.TempDir())
	id, err := repo.Create(CreateTaskInput{Title: "h", Goal: "g"})
	if err != nil {
		t.Fatal(err)
	}
	primary, err := repo.BeginExecute(id, ExecuteInput{Owner: "worker", Model: "p/m"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := repo.BeginChildHandoff(primary, "p/c")
	if err != nil {
		t.Fatal(err)
	}
	returned, err := repo.CompleteChildHandoff(child, "worker")
	if err != nil {
		t.Fatal(err)
	}
	if returned.Fence != primary.Fence+2 {
		t.Fatalf("returned fence=%d", returned.Fence)
	}
	// Simulate process restart: new repo, recover, no provider calls possible here.
	repo2 := New(repo.dir)
	n, err := repo2.RecoverOrphans()
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	detail, _ := repo2.Get(id)
	if detail.Summary.RunState != "interrupted" {
		t.Fatalf("runState=%s", detail.Summary.RunState)
	}
}

func TestHandoff12_MalformedFenceProgression(t *testing.T) {
	events := []Event{
		{Sequence: 1, EventType: EventTaskCreated, EventHash: "a", Payload: json.RawMessage(mustJSON(map[string]any{"title": "t"}))},
	}
	// Build a minimal invalid progression via validateHandoffFenceProgression directly.
	bad := []Event{
		{EventType: EventRunStarted, Payload: json.RawMessage(mustJSON(map[string]any{"kind": "execute", "run_id": "r1", "owner": "w", "fencing_token": 1}))},
		{EventType: EventHandoffProposed, Payload: json.RawMessage(mustJSON(map[string]any{"handoff_id": "ho_1", "run_id": "r1", "direction": HandoffDirectionParentToChild, "previous_fence": 1, "proposed_fence": 3}))},
	}
	if err := validateHandoffFenceProgression(bad); err == nil {
		t.Fatal("expected invalid progression")
	}
	_ = events
}

func TestHandoff13_StalePrimaryIdentityRejected(t *testing.T) {
	repo := New(t.TempDir())
	id, err := repo.Create(CreateTaskInput{Title: "h", Goal: "g"})
	if err != nil {
		t.Fatal(err)
	}
	primary, err := repo.BeginExecute(id, ExecuteInput{Owner: "worker", Model: "p/m"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.BeginChildHandoff(RunIdentity{TaskID: id, RunID: "", Owner: primary.Owner, Fence: primary.Fence}, "p/c"); CodeOf(err) != CodeInvalidTransition {
		t.Fatalf("empty runId: %v", err)
	}
	if _, err := repo.BeginChildHandoff(RunIdentity{TaskID: id, RunID: "other", Owner: primary.Owner, Fence: primary.Fence}, "p/c"); CodeOf(err) != CodeInvalidTransition {
		t.Fatalf("wrong runId: %v", err)
	}
	if _, err := repo.BeginChildHandoff(RunIdentity{TaskID: id, RunID: primary.RunID, Owner: "other", Fence: primary.Fence}, "p/c"); CodeOf(err) != CodeInvalidTransition {
		t.Fatalf("wrong owner: %v", err)
	}
}

func TestHandoff14_DurableSMEventOrder(t *testing.T) {
	repo := New(t.TempDir())
	id, err := repo.Create(CreateTaskInput{Title: "h", Goal: "g"})
	if err != nil {
		t.Fatal(err)
	}
	primary, err := repo.BeginExecute(id, ExecuteInput{Owner: "worker", Model: "p/m"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := repo.BeginChildHandoff(primary, "p/c")
	if err != nil {
		t.Fatal(err)
	}
	returned, err := repo.CompleteChildHandoff(child, "worker")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CompleteExecute(returned); err != nil {
		t.Fatal(err)
	}
	detail, _ := repo.Get(id)
	types := make([]string, 0, len(detail.Events))
	for _, ev := range detail.Events {
		types = append(types, ev.EventType)
	}
	joined := strings.Join(types, ",")
	need := []string{EventHandoffProposed, EventHandoffCommitted, EventChildRunStarted, EventChildRunCompleted, EventHandoffProposed, EventHandoffCommitted, EventRunCompleted}
	pos := 0
	for _, n := range need {
		idx := strings.Index(joined[pos:], n)
		if idx < 0 {
			t.Fatalf("missing ordered %s in %v", n, types)
		}
		pos += idx + len(n)
	}
	if child.Fence != primary.Fence+1 || returned.Fence != primary.Fence+2 {
		t.Fatalf("N=%d child=%d returned=%d", primary.Fence, child.Fence, returned.Fence)
	}
}

func TestHandoff15_ConcurrentCancelClaimLinearization(t *testing.T) {
	// Deterministic: after child handoff, parent fence must not cancel via CheckOwnerAndFence semantics.
	repo := New(t.TempDir())
	id, err := repo.Create(CreateTaskInput{Title: "h", Goal: "g"})
	if err != nil {
		t.Fatal(err)
	}
	primary, err := repo.BeginExecute(id, ExecuteInput{Owner: "worker", Model: "p/m"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := repo.BeginChildHandoff(primary, "p/c")
	if err != nil {
		t.Fatal(err)
	}
	ls := &Leases{Root: repo.dir, TaskID: id}
	if err := ls.CheckOwnerAndFence(primary.Owner, primary.Fence); err == nil {
		t.Fatal("stale parent fence must fail after child CAS")
	}
	if err := ls.CheckOwnerAndFence(child.Owner, child.Fence); err != nil {
		t.Fatal(err)
	}
	returned, err := repo.CompleteChildHandoff(child, "worker")
	if err != nil {
		t.Fatal(err)
	}
	if err := ls.CheckOwnerAndFence(child.Owner, child.Fence); err == nil {
		t.Fatal("stale child claim after return must fail")
	}
	if err := ls.CheckOwnerAndFence(returned.Owner, returned.Fence); err != nil {
		t.Fatal(err)
	}
}

func TestHandoffSemanticValidatorNegativeRebuild(t *testing.T) {
	d := t.TempDir()
	log := NewLog(d, "task_1")
	mustAppend(t, log, mkEvent(1, EventTaskCreated, "w", map[string]any{"title": "t", "goal": "g", "acceptance_criteria_hash": "ac"}))
	mustAppend(t, log, mkEvent(2, EventRunStarted, "w", map[string]any{"kind": "execute", "run_id": "run_1", "owner": "w", "fencing_token": 1, "status": "running", "model": "p/m"}))
	mustAppend(t, log, mkEvent(3, EventHandoffProposed, "w", map[string]any{
		"handoff_id": "ho_a", "run_id": "run_1", "direction": HandoffDirectionParentToChild,
		"from_owner": "w", "to_owner": "fabric-child-x", "previous_fence": 1, "proposed_fence": 2,
	}))
	mustAppend(t, log, mkEvent(4, EventHandoffCommitted, "fabric-child-x", map[string]any{
		"handoff_id": "ho_a", "run_id": "run_1", "direction": HandoffDirectionParentToChild,
		"new_owner": "fabric-child-x", "previous_fence": 1, "fencing_token": 2,
	}))
	// ChildStart with mismatched handoff_id must fail closed on Rebuild.
	mustAppend(t, log, mkEvent(5, EventChildRunStarted, "fabric-child-x", map[string]any{
		"run_id": "run_1", "child_id": "x", "handoff_id": "ho_OTHER", "owner": "fabric-child-x",
		"fencing_token": 2, "status": "running", "kind": "child", "model": "p/c",
	}))
	if _, err := log.Rebuild(); err == nil {
		t.Fatal("expected rebuild failure for child handoff_id mismatch")
	}
}

func TestHandoffSemanticValidatorNegativeDuplicatePropose(t *testing.T) {
	d := t.TempDir()
	log := NewLog(d, "task_1")
	mustAppend(t, log, mkEvent(1, EventTaskCreated, "w", map[string]any{"title": "t", "goal": "g", "acceptance_criteria_hash": "ac"}))
	mustAppend(t, log, mkEvent(2, EventRunStarted, "w", map[string]any{"kind": "execute", "run_id": "run_1", "owner": "w", "fencing_token": 1, "status": "running", "model": "p/m"}))
	mustAppend(t, log, mkEvent(3, EventHandoffProposed, "w", map[string]any{
		"handoff_id": "ho_a", "run_id": "run_1", "direction": HandoffDirectionParentToChild,
		"from_owner": "w", "to_owner": "c1", "previous_fence": 1, "proposed_fence": 2,
	}))
	mustAppend(t, log, mkEvent(4, EventHandoffProposed, "w", map[string]any{
		"handoff_id": "ho_b", "run_id": "run_1", "direction": HandoffDirectionParentToChild,
		"from_owner": "w", "to_owner": "c2", "previous_fence": 1, "proposed_fence": 2,
	}))
	if _, err := log.Rebuild(); err == nil {
		t.Fatal("expected rebuild failure for duplicate open propose")
	}
}

func TestHandoffLegacyCommittedWithoutHandoffIDStillValid(t *testing.T) {
	d := t.TempDir()
	log := NewLog(d, "task_1")
	mustAppend(t, log, mkEvent(1, EventTaskCreated, "w", map[string]any{"title": "t", "goal": "g", "acceptance_criteria_hash": "ac"}))
	mustAppend(t, log, mkEvent(2, EventRunStarted, "w", map[string]any{"kind": "execute", "run_id": "run_1", "owner": "w", "fencing_token": 1, "status": "running", "model": "p/m"}))
	mustAppend(t, log, mkEvent(3, EventHandoffCommitted, "w", map[string]any{"new_owner": "w2", "fencing_token": 2}))
	if _, err := log.Rebuild(); err != nil {
		t.Fatalf("legacy commit without handoff_id should rebuild: %v", err)
	}
}

func TestHandoffAdmission_ExactBoundarySuccess(t *testing.T) {
	repo := New(t.TempDir())
	origO, origR := maxOrdinaryEventsPerTask, lifecycleEventReserve
	maxOrdinaryEventsPerTask = 4 // TaskCreated+RunStarted+ChildStarted+Progress
	lifecycleEventReserve = childHandoffAdmissionControlEvents
	t.Cleanup(func() {
		maxOrdinaryEventsPerTask = origO
		lifecycleEventReserve = origR
	})
	id, err := repo.Create(CreateTaskInput{Title: "h", Goal: "g"})
	if err != nil {
		t.Fatal(err)
	}
	primary, err := repo.BeginExecute(id, ExecuteInput{Owner: "worker", Model: "p/m"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := repo.BeginChildHandoff(primary, "p/c")
	if err != nil {
		t.Fatalf("exact-boundary admission: %v", err)
	}
	if err := repo.RecordProgress(RunIdentity{TaskID: child.TaskID, RunID: child.RunID, Owner: child.Owner, Fence: child.Fence}, "child"); err != nil {
		t.Fatal(err)
	}
	returned, err := repo.CompleteChildHandoff(child, "worker")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CompleteExecute(returned); err != nil {
		t.Fatal(err)
	}
	detail, _ := repo.Get(id)
	if detail.Summary.RunState != "completed" {
		t.Fatalf("runState=%s", detail.Summary.RunState)
	}
	var sawPrimary bool
	for _, ev := range detail.Events {
		if ev.EventType == EventRunCompleted {
			sawPrimary = true
		}
	}
	if !sawPrimary {
		t.Fatal("primary terminal missing")
	}
}

func TestHandoffAdmission_OneSlotBelowRejectsBeforePropose(t *testing.T) {
	repo := New(t.TempDir())
	origO, origR := maxOrdinaryEventsPerTask, lifecycleEventReserve
	maxOrdinaryEventsPerTask = 3 // only one ordinary slot left after create+runstarted
	lifecycleEventReserve = childHandoffAdmissionControlEvents
	t.Cleanup(func() {
		maxOrdinaryEventsPerTask = origO
		lifecycleEventReserve = origR
	})
	id, err := repo.Create(CreateTaskInput{Title: "h", Goal: "g"})
	if err != nil {
		t.Fatal(err)
	}
	primary, err := repo.BeginExecute(id, ExecuteInput{Owner: "worker", Model: "p/m"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.BeginChildHandoff(primary, "p/c")
	if CodeOf(err) != CodeCapacityExceeded {
		t.Fatalf("want capacity exceeded before propose, got %v", err)
	}
	detail, _ := repo.Get(id)
	for _, ev := range detail.Events {
		if ev.EventType == EventHandoffProposed {
			t.Fatal("HandoffProposed must not be appended on pre-admission reject")
		}
	}
	owner, fence, _ := repo.LeaseSnapshot(id)
	if owner != primary.Owner || fence != primary.Fence {
		t.Fatalf("lease moved owner=%s fence=%d", owner, fence)
	}
}

func TestHandoffAdmission_ProgressLeavesTerminalRoom(t *testing.T) {
	repo := New(t.TempDir())
	origO, origR := maxOrdinaryEventsPerTask, lifecycleEventReserve
	maxOrdinaryEventsPerTask = 4
	lifecycleEventReserve = childHandoffAdmissionControlEvents
	t.Cleanup(func() {
		maxOrdinaryEventsPerTask = origO
		lifecycleEventReserve = origR
	})
	id, err := repo.Create(CreateTaskInput{Title: "h", Goal: "g"})
	if err != nil {
		t.Fatal(err)
	}
	primary, err := repo.BeginExecute(id, ExecuteInput{Owner: "worker", Model: "p/m"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := repo.BeginChildHandoff(primary, "p/c")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordProgress(RunIdentity{TaskID: child.TaskID, RunID: child.RunID, Owner: child.Owner, Fence: child.Fence}, "child"); err != nil {
		t.Fatal(err)
	}
	// Ordinary is now at cap; child terminal + return propose/commit + primary must still fit via control reserve.
	returned, err := repo.CompleteChildHandoff(child, "worker")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CompleteExecute(returned); err != nil {
		t.Fatal(err)
	}
}

func TestHandoffAdmission_OutboundCASFailureAtCapacity(t *testing.T) {
	repo := New(t.TempDir())
	origO, origR := maxOrdinaryEventsPerTask, lifecycleEventReserve
	maxOrdinaryEventsPerTask = 4
	lifecycleEventReserve = childHandoffAdmissionControlEvents
	t.Cleanup(func() {
		maxOrdinaryEventsPerTask = origO
		lifecycleEventReserve = origR
		leaseAtomicWriteHook = nil
	})
	id, err := repo.Create(CreateTaskInput{Title: "h", Goal: "g"})
	if err != nil {
		t.Fatal(err)
	}
	primary, err := repo.BeginExecute(id, ExecuteInput{Owner: "worker", Model: "p/m"})
	if err != nil {
		t.Fatal(err)
	}
	leaseAtomicWriteHook = func(string, []byte) error {
		return errors.New("injected lease write failure")
	}
	_, err = repo.BeginChildHandoff(primary, "p/c")
	if err == nil {
		t.Fatal("expected CAS failure")
	}
	leaseAtomicWriteHook = nil
	owner, fence, _ := repo.LeaseSnapshot(id)
	if owner != primary.Owner || fence != primary.Fence {
		t.Fatalf("lease mutated owner=%s fence=%d", owner, fence)
	}
	// Failure/reconcile headroom: HandoffFailed already appended; RunInterrupted must fit.
	if err := repo.ReconcileExecuteInterrupted(id, primary.RunID, "cas_failed"); err != nil {
		t.Fatalf("reconcile at capacity: %v", err)
	}
	detail, _ := repo.Get(id)
	if detail.Summary.RunState != "interrupted" {
		t.Fatalf("runState=%s", detail.Summary.RunState)
	}
}

func TestHandoffAdmission_OutboundPostCASAppendFailureAtCapacity(t *testing.T) {
	repo := New(t.TempDir())
	origO, origR := maxOrdinaryEventsPerTask, lifecycleEventReserve
	maxOrdinaryEventsPerTask = 4
	lifecycleEventReserve = childHandoffAdmissionControlEvents
	t.Cleanup(func() {
		maxOrdinaryEventsPerTask = origO
		lifecycleEventReserve = origR
		SetAppendLockedHookForTest(nil)
	})
	id, err := repo.Create(CreateTaskInput{Title: "h", Goal: "g"})
	if err != nil {
		t.Fatal(err)
	}
	primary, err := repo.BeginExecute(id, ExecuteInput{Owner: "worker", Model: "p/m"})
	if err != nil {
		t.Fatal(err)
	}
	SetAppendLockedHookForTest(func(taskID, eventType string) error {
		if eventType == EventHandoffCommitted {
			return errors.New("injected commit append failure")
		}
		return nil
	})
	_, err = repo.BeginChildHandoff(primary, "p/c")
	if err == nil {
		t.Fatal("expected append failure")
	}
	SetAppendLockedHookForTest(nil)
	if err := repo.ReconcileExecuteInterrupted(id, primary.RunID, "half_commit"); err != nil {
		t.Fatalf("reconcile at capacity: %v", err)
	}
	detail, _ := repo.Get(id)
	if detail.Summary.RunState != "interrupted" {
		t.Fatalf("runState=%s", detail.Summary.RunState)
	}
}

func TestHandoffAdmission_ReturnPostCASAppendFailureAtCapacity(t *testing.T) {
	repo := New(t.TempDir())
	origO, origR := maxOrdinaryEventsPerTask, lifecycleEventReserve
	maxOrdinaryEventsPerTask = 4
	lifecycleEventReserve = childHandoffAdmissionControlEvents
	t.Cleanup(func() {
		maxOrdinaryEventsPerTask = origO
		lifecycleEventReserve = origR
		SetAppendLockedHookForTest(nil)
	})
	id, err := repo.Create(CreateTaskInput{Title: "h", Goal: "g"})
	if err != nil {
		t.Fatal(err)
	}
	primary, err := repo.BeginExecute(id, ExecuteInput{Owner: "worker", Model: "p/m"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := repo.BeginChildHandoff(primary, "p/c")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordProgress(RunIdentity{TaskID: child.TaskID, RunID: child.RunID, Owner: child.Owner, Fence: child.Fence}, "child"); err != nil {
		t.Fatal(err)
	}
	SetAppendLockedHookForTest(func(taskID, eventType string) error {
		if eventType == EventHandoffCommitted {
			return errors.New("injected return commit failure")
		}
		return nil
	})
	_, err = repo.CompleteChildHandoff(child, "worker")
	if err == nil {
		t.Fatal("expected return commit failure")
	}
	SetAppendLockedHookForTest(nil)
	if err := repo.ReconcileExecuteInterrupted(id, primary.RunID, "return_half_commit"); err != nil {
		t.Fatalf("reconcile at capacity: %v", err)
	}
	detail, _ := repo.Get(id)
	if detail.Summary.RunState != "interrupted" {
		t.Fatalf("runState=%s", detail.Summary.RunState)
	}
}

func TestHandoffAdmission_CancelWhileChildOwnedAtCapacity(t *testing.T) {
	repo := New(t.TempDir())
	origO, origR := maxOrdinaryEventsPerTask, lifecycleEventReserve
	maxOrdinaryEventsPerTask = 4
	lifecycleEventReserve = childHandoffAdmissionControlEvents
	t.Cleanup(func() {
		maxOrdinaryEventsPerTask = origO
		lifecycleEventReserve = origR
	})
	id, err := repo.Create(CreateTaskInput{Title: "h", Goal: "g"})
	if err != nil {
		t.Fatal(err)
	}
	primary, err := repo.BeginExecute(id, ExecuteInput{Owner: "worker", Model: "p/m"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := repo.BeginChildHandoff(primary, "p/c")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordProgress(RunIdentity{TaskID: child.TaskID, RunID: child.RunID, Owner: child.Owner, Fence: child.Fence}, "child"); err != nil {
		t.Fatal(err)
	}
	returned, err := repo.CancelChildHandoff(child, "worker", "operator_cancelled")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CancelExecute(returned, "operator_cancelled"); err != nil {
		t.Fatal(err)
	}
	detail, _ := repo.Get(id)
	if detail.Summary.RunState != "cancelled" {
		t.Fatalf("runState=%s", detail.Summary.RunState)
	}
}

func TestHandoffAdmission_AfterAcceptCapacityNeverPermanentRunning(t *testing.T) {
	repo := New(t.TempDir())
	origO, origR := maxOrdinaryEventsPerTask, lifecycleEventReserve
	maxOrdinaryEventsPerTask = 4
	lifecycleEventReserve = childHandoffAdmissionControlEvents
	t.Cleanup(func() {
		maxOrdinaryEventsPerTask = origO
		lifecycleEventReserve = origR
	})
	id, err := repo.Create(CreateTaskInput{Title: "h", Goal: "g"})
	if err != nil {
		t.Fatal(err)
	}
	primary, err := repo.BeginExecute(id, ExecuteInput{Owner: "worker", Model: "p/m"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.BeginChildHandoff(primary, "p/c")
	if err != nil {
		t.Fatal(err)
	}
	// Event capacity alone must not leave the task permanently running.
	if err := repo.ReconcileExecuteInterrupted(id, primary.RunID, "force_converge"); err != nil {
		t.Fatalf("converge: %v", err)
	}
	detail, _ := repo.Get(id)
	if detail.Summary.RunState == "running" {
		t.Fatal("capacity must not leave task permanently running")
	}
}

func TestHandoffAdmission_ControlReserveMinimumIsSix(t *testing.T) {
	if childHandoffAdmissionControlEvents > lifecycleEventReserve {
		t.Fatalf("lifecycleEventReserve=%d < required %d", lifecycleEventReserve, childHandoffAdmissionControlEvents)
	}
	if childHandoffAdmissionControlEvents != 6 {
		t.Fatalf("expected documented minimum 6, got %d", childHandoffAdmissionControlEvents)
	}
}
