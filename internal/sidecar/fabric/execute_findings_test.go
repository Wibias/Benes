package fabric

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestExecG4_RecoverOrphansRequiresLeaseAuthority(t *testing.T) {
	dir := t.TempDir()
	repo := New(dir)
	taskID, err := repo.Create(CreateTaskInput{Title: "orphan", Goal: "ship"})
	if err != nil {
		t.Fatal(err)
	}
	id, err := repo.BeginExecute(taskID, ExecuteInput{Owner: "worker", Model: "p/m"})
	if err != nil {
		t.Fatal(err)
	}
	// Corrupt lease to a different owner/fence so recovery must fail closed.
	ls := &Leases{Root: dir, TaskID: taskID}
	if _, err := ls.CommitHandoff("other-owner"); err != nil {
		t.Fatal(err)
	}
	n, err := repo.RecoverOrphans()
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("recovered %d; want 0 when lease authority mismatches", n)
	}
	detail, err := repo.Get(taskID)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range detail.Events {
		if ev.EventType == EventRunInterrupted {
			t.Fatal("must not silently terminal without lease authority")
		}
	}
	_ = id
}

func TestExecG5_RecoverOrphansMalformedLeaseFailClosed(t *testing.T) {
	dir := t.TempDir()
	repo := New(dir)
	taskID, err := repo.Create(CreateTaskInput{Title: "badlease", Goal: "ship"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.BeginExecute(taskID, ExecuteInput{Owner: "worker", Model: "p/m"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "leases", taskID+".lease.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"fencing_token":1,"owner":"worker","prompt":"secret"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	n, err := repo.RecoverOrphans()
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("recovered %d on malformed lease; want 0", n)
	}
}

func TestExecG6_ValidV2StillInterrupted(t *testing.T) {
	dir := t.TempDir()
	repo := New(dir)
	taskID, err := repo.Create(CreateTaskInput{Title: "ok", Goal: "ship"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.BeginExecute(taskID, ExecuteInput{Owner: "worker", Model: "p/m"}); err != nil {
		t.Fatal(err)
	}
	n, err := repo.RecoverOrphans()
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("recovered %d; want 1", n)
	}
	detail, err := repo.Get(taskID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Summary.RunState != "interrupted" {
		t.Fatalf("runState=%s", detail.Summary.RunState)
	}
}

func TestExecPersistFailClosedNoReleaseOnAppendFailure(t *testing.T) {
	dir := t.TempDir()
	repo := New(dir)
	taskID, err := repo.Create(CreateTaskInput{Title: "persist", Goal: "ship"})
	if err != nil {
		t.Fatal(err)
	}
	id, err := repo.BeginExecute(taskID, ExecuteInput{Owner: "worker", Model: "p/m"})
	if err != nil {
		t.Fatal(err)
	}
	SetAppendLockedHookForTest(func(taskID, eventType string) error {
		if eventType == EventRunCompleted {
			return errors.New("injected append failure")
		}
		return nil
	})
	t.Cleanup(func() { SetAppendLockedHookForTest(nil) })
	if err := repo.CompleteExecute(id); err == nil {
		t.Fatal("expected complete failure")
	}
	ls := &Leases{Root: dir, TaskID: taskID}
	if err := ls.CheckOwnerAndFence(id.Owner, id.Fence); err != nil {
		t.Fatalf("lease must remain held after failed terminal persist: %v", err)
	}
	detail, err := repo.Get(taskID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Summary.RunState == "completed" {
		t.Fatal("completed must not be visible after persist failure")
	}
}
