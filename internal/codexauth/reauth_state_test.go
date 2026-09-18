package codexauth

import (
	"sync"
	"testing"
)

func TestReauthStateMarkClearAndReconcile(t *testing.T) {
	state := NewReauthState()
	state.Mark("acct-a", 1)
	state.Mark(MainAccountID, 1)
	if !state.Needs("acct-a") || !state.Needs(MainAccountID) {
		t.Fatalf("marked state missing")
	}
	state.Clear("acct-a")
	if state.Needs("acct-a") {
		t.Fatal("clear did not remove acct-a")
	}

	state.Mark("stale", 2)
	removed := state.Reconcile(3, map[string]struct{}{MainAccountID: {}, "live": {}})
	if removed != 1 {
		t.Fatalf("removed=%d", removed)
	}
	if state.Needs("stale") {
		t.Fatal("stale state survived reconciliation")
	}
	if !state.Needs(MainAccountID) {
		t.Fatal("live main state was removed")
	}
}

func TestReauthStateIgnoresOutOfOrderReconciliation(t *testing.T) {
	state := NewReauthState()
	state.Mark("acct", 1)
	if removed := state.Reconcile(5, map[string]struct{}{"acct": {}}); removed != 0 {
		t.Fatalf("removed=%d", removed)
	}
	if removed := state.Reconcile(4, map[string]struct{}{}); removed != 0 {
		t.Fatalf("older reconcile removed=%d", removed)
	}
	if !state.Needs("acct") {
		t.Fatal("older reconcile changed live state")
	}
}

func TestReauthStateRejectsStaleWriterForDeletedAccountButAllowsLiveAccount(t *testing.T) {
	state := NewReauthState()
	state.Reconcile(10, map[string]struct{}{"live": {}})

	state.Mark("deleted", 9)
	if state.Needs("deleted") {
		t.Fatal("stale writer resurrected deleted account")
	}

	state.Mark("live", 9)
	if !state.Needs("live") {
		t.Fatal("stale writer for still-live account was incorrectly dropped")
	}
}

func TestReauthStateConcurrentAccessIsSafe(t *testing.T) {
	state := NewReauthState()
	live := map[string]struct{}{"a": {}, "b": {}, MainAccountID: {}}
	state.Reconcile(1, live)

	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				id := "a"
				if (i+worker)%2 == 0 {
					id = "b"
				}
				state.Mark(id, 1)
				_ = state.Needs(id)
				if i%7 == 0 {
					state.Clear(id)
				}
				if i%31 == 0 {
					state.Reconcile(1, live)
				}
			}
		}(worker)
	}
	wg.Wait()
}
