package fabric

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestExecG3_RecoverOrphansSkipsLifecycleMarkStarted(t *testing.T) {
	dir := t.TempDir()
	repo := New(dir)
	id, err := repo.Create(CreateTaskInput{Title: "life", Goal: "ship"})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkStarted(id, "operator"); err != nil {
		t.Fatal(err)
	}
	n, err := repo.RecoverOrphans()
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("recovered %d orphans; want 0 for lifecycle MarkStarted", n)
	}
	detail, err := repo.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Summary.RunState != "running" && detail.Summary.RunState != "starting" {
		t.Fatalf("lifecycle start rewritten: runState=%s", detail.Summary.RunState)
	}
	for _, ev := range detail.Events {
		if ev.EventType == EventRunInterrupted {
			t.Fatal("lifecycle MarkStarted must not become Interrupted")
		}
	}
}

func TestLease21_AtomicWriteFailureKeepsPreviousValid(t *testing.T) {
	dir := t.TempDir()
	ls := &Leases{Root: dir, TaskID: "task_atomic"}
	tok, err := ls.AcquireWriteLease("owner-a")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "leases", "task_atomic.lease.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	leaseAtomicWriteHook = func(string, []byte) error {
		return errors.New("injected atomic write failure")
	}
	t.Cleanup(func() { leaseAtomicWriteHook = nil })
	if _, err := ls.CommitHandoff("owner-b"); err == nil {
		t.Fatal("expected injected failure")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("previous lease mutated on failed atomic write:\nbefore=%s\nafter=%s", before, after)
	}
	if err := ls.CheckOwnerAndFence("owner-a", tok); err != nil {
		t.Fatalf("previous lease invalid after failed write: %v", err)
	}
}
