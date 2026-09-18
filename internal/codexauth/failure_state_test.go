package codexauth

import (
	"sync"
	"testing"
	"time"
)

func TestFailureStateTripsAtThresholdWithinWindow(t *testing.T) {
	state := NewFailureState()
	now := time.Unix(1_700_000_000, 0)
	for attempt := 1; attempt <= 3; attempt++ {
		state.RecordFailure("acct", now.Add(time.Duration(attempt)*time.Second), 502, 1)
		want := attempt >= 3
		if got := state.ShouldFailover("acct", now.Add(time.Duration(attempt)*time.Second), 3); got != want {
			t.Fatalf("attempt=%d failover=%v want=%v", attempt, got, want)
		}
	}
	if state.ShouldFailover("acct", now.Add(FailureWindow+4*time.Second), 3) {
		t.Fatal("stale streak survived failure window")
	}
	if state.ShouldFailover("acct", now, 0) {
		t.Fatal("disabled failover threshold tripped")
	}
}

func TestFailureStateStaleFailureStartsNewWindow(t *testing.T) {
	state := NewFailureState()
	now := time.Unix(1_700_000_000, 0)
	state.RecordFailure("acct", now, 500, 1)
	state.RecordFailure("acct", now.Add(FailureWindow+time.Nanosecond), 503, 1)
	snapshot, ok := state.Snapshot("acct")
	if !ok || snapshot.ConsecutiveFailures != 1 || snapshot.LastFailureStatus != 503 {
		t.Fatalf("snapshot=%#v ok=%v", snapshot, ok)
	}
}

func TestFailureStateSuccessRecoveryMatchesBunEscalationRule(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)

	levelOne := NewFailureState()
	levelOne.RecordFailure("acct", now, 500, 1)
	levelOne.RecordSuccess("acct", now.Add(time.Second), 3)
	if _, ok := levelOne.Snapshot("acct"); ok {
		t.Fatal("single-failure state did not clear on success")
	}

	escalated := NewFailureState()
	escalated.RecordFailure("acct", now, 500, 1)
	escalated.RecordFailure("acct", now.Add(time.Second), 500, 1)
	escalated.RecordSuccess("acct", now.Add(2*time.Second), 3)
	first, ok := escalated.Snapshot("acct")
	if !ok || first.ConsecutiveFailures != 2 || first.ConsecutiveSuccesses != 1 {
		t.Fatalf("first recovery=%#v ok=%v", first, ok)
	}
	escalated.RecordSuccess("acct", now.Add(3*time.Second), 3)
	if _, ok := escalated.Snapshot("acct"); ok {
		t.Fatal("second healthy terminal did not clear escalated state")
	}
}

func TestFailureStateSuccessWithFailoverDisabledClearsImmediately(t *testing.T) {
	state := NewFailureState()
	now := time.Now()
	state.RecordFailure("acct", now, 500, 1)
	state.RecordFailure("acct", now, 500, 1)
	state.RecordFailure("acct", now, 500, 1)
	state.RecordSuccess("acct", now.Add(time.Second), 0)
	if _, ok := state.Snapshot("acct"); ok {
		t.Fatal("disabled failover retained recovery streak")
	}
}

func TestFailureStateGenerationReconcileBlocksDeletedAccountResurrection(t *testing.T) {
	state := NewFailureState()
	state.Reconcile(10, map[string]struct{}{"live": {}})
	state.RecordFailure("deleted", time.Now(), 500, 9)
	if _, ok := state.Snapshot("deleted"); ok {
		t.Fatal("stale writer resurrected deleted account")
	}
	state.RecordFailure("live", time.Now(), 500, 9)
	if _, ok := state.Snapshot("live"); !ok {
		t.Fatal("stale writer for live account was dropped")
	}
	if removed := state.Reconcile(9, map[string]struct{}{}); removed != 0 {
		t.Fatalf("older reconcile removed=%d", removed)
	}
}

func TestFailureStateReconcileRemovesDeletedAndConcurrentAccessIsSafe(t *testing.T) {
	state := NewFailureState()
	now := time.Now()
	state.RecordFailure("deleted", now, 500, 1)
	state.RecordFailure("live", now, 500, 1)
	if removed := state.Reconcile(2, map[string]struct{}{"live": {}}); removed != 1 {
		t.Fatalf("removed=%d", removed)
	}
	if _, ok := state.Snapshot("deleted"); ok {
		t.Fatal("deleted streak survived")
	}

	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < 400; i++ {
				id := "a"
				if (worker+i)%2 == 0 {
					id = "b"
				}
				state.RecordFailure(id, now.Add(time.Duration(i)*time.Millisecond), 502, 2)
				_ = state.ShouldFailover(id, now, 3)
				if i%17 == 0 {
					state.RecordSuccess(id, now, 3)
				}
			}
		}(worker)
	}
	wg.Wait()
}
