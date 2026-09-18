package fabric

import (
	"errors"
	"strings"
	"testing"
)

func TestProjection_HalfCommitReconcileUpdatesOwnerFence(t *testing.T) {
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
	detail, _ := repo.Get(id)
	// Before reconcile Projection may still show parent owner/fence (stale).
	staleOwner := detail.Projection.CurrentOwner
	staleFence := detail.Projection.FencingToken
	if staleOwner != "worker" || staleFence != primary.Fence {
		t.Fatalf("pre-reconcile projection owner=%s fence=%d", staleOwner, staleFence)
	}
	// RED: checkIdentity would accept the stale parent claim while lease is child N+1.
	if err := checkIdentity("cancel", Identity{Principal: "op", ExpectedOwner: staleOwner, ExpectedFencingToken: staleFence}, detail.Projection); err != nil {
		t.Fatalf("stale identity unexpectedly rejected pre-fix: %v", err)
	}
	n, rerr := repo.RecoverOrphans()
	if rerr != nil || n != 1 {
		t.Fatalf("recover n=%d err=%v", n, rerr)
	}
	detail, _ = repo.Get(id)
	if detail.Summary.RunState != "interrupted" {
		t.Fatalf("runState=%s", detail.Summary.RunState)
	}
	if !strings.HasPrefix(detail.Projection.CurrentOwner, childOwnerPrefix) {
		t.Fatalf("projection owner=%s want child after reconcile", detail.Projection.CurrentOwner)
	}
	if detail.Projection.FencingToken != primary.Fence+1 {
		t.Fatalf("projection fence=%d want %d", detail.Projection.FencingToken, primary.Fence+1)
	}
	// GREEN: stale parent identity must now be rejected.
	if err := checkIdentity("cancel", Identity{Principal: "op", ExpectedOwner: staleOwner, ExpectedFencingToken: staleFence}, detail.Projection); err == nil {
		t.Fatal("stale parent identity must be rejected after projection reconcile")
	}
}

func TestProjection_ReturnHalfCommitReconcileUpdatesParentFence(t *testing.T) {
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
	detail, _ := repo.Get(id)
	if detail.Projection.CurrentOwner != child.Owner || detail.Projection.FencingToken != child.Fence {
		t.Fatalf("pre-reconcile projection owner=%s fence=%d", detail.Projection.CurrentOwner, detail.Projection.FencingToken)
	}
	staleOwner, staleFence := detail.Projection.CurrentOwner, detail.Projection.FencingToken
	n, rerr := repo.RecoverOrphans()
	if rerr != nil || n != 1 {
		t.Fatalf("recover n=%d err=%v", n, rerr)
	}
	detail, _ = repo.Get(id)
	if detail.Projection.CurrentOwner != "worker" || detail.Projection.FencingToken != primary.Fence+2 {
		t.Fatalf("projection owner=%s fence=%d want worker/%d", detail.Projection.CurrentOwner, detail.Projection.FencingToken, primary.Fence+2)
	}
	if err := checkIdentity("cancel", Identity{Principal: "op", ExpectedOwner: staleOwner, ExpectedFencingToken: staleFence}, detail.Projection); err == nil {
		t.Fatal("stale child identity must be rejected after return reconcile")
	}
}
