package codexauth

import (
	"testing"
	"time"
)

func TestThreadAffinityCarriesAndUpdatesReevaluationClock(t *testing.T) {
	state := NewThreadAffinityState()
	now := time.Unix(1_700_000_000, 0)
	if !state.Bind("thread", MainAccountID, AffinityScopeShared, now, ManagedCredentialSnapshot{}) {
		t.Fatal("bind failed")
	}
	resolved := state.Resolve("thread", AffinityScopeShared, now.Add(30*time.Second), ManagedCredentialSnapshot{})
	if resolved.Status != AffinitySelected || !resolved.LastReevaluatedAt.Equal(now) {
		t.Fatalf("resolved=%#v", resolved)
	}
	if !state.MarkReevaluated("thread", AffinityScopeShared, MainAccountID, 0, now.Add(time.Minute)) {
		t.Fatal("MarkReevaluated() rejected live binding")
	}
	resolved = state.Resolve("thread", AffinityScopeShared, now.Add(61*time.Second), ManagedCredentialSnapshot{})
	if !resolved.LastReevaluatedAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("lastReevaluatedAt=%v", resolved.LastReevaluatedAt)
	}
}

func TestThreadAffinityReevaluationUpdateIsGenerationFenced(t *testing.T) {
	state := NewThreadAffinityState()
	now := time.Unix(1_700_000_000, 0)
	snapshot := ManagedCredentialSnapshot{Status: ManagedCredentialStoreOK, Records: map[string]ManagedCredentialRecord{
		"acct": {Generation: 4, Credential: &ManagedCredential{AccessToken: "a"}},
	}}
	if !state.Bind("thread", "acct", AffinityScopeShared, now, snapshot) {
		t.Fatal("bind failed")
	}
	if state.MarkReevaluated("thread", AffinityScopeShared, "acct", 3, now.Add(time.Minute)) {
		t.Fatal("stale generation updated re-evaluation clock")
	}
	if state.MarkReevaluated("thread", AffinityScopeShared, "other", 4, now.Add(time.Minute)) {
		t.Fatal("wrong account updated re-evaluation clock")
	}
	resolved := state.Resolve("thread", AffinityScopeShared, now.Add(time.Minute), snapshot)
	if !resolved.LastReevaluatedAt.Equal(now) {
		t.Fatalf("stale update changed clock=%v", resolved.LastReevaluatedAt)
	}
}

func TestThreadAffinityRebindPreservesSessionCreationButResetsReevaluationClock(t *testing.T) {
	state := NewThreadAffinityState()
	now := time.Unix(1_700_000_000, 0)
	if !state.Bind("thread", MainAccountID, AffinityScopeLegacy, now, ManagedCredentialSnapshot{}) {
		t.Fatal("initial bind failed")
	}
	if !state.Bind("thread", MainAccountID, AffinityScopeLegacy, now.Add(10*time.Minute), ManagedCredentialSnapshot{}) {
		t.Fatal("rebind failed")
	}
	resolved := state.Resolve("thread", AffinityScopeLegacy, now.Add(10*time.Minute), ManagedCredentialSnapshot{})
	if !resolved.CreatedAt.Equal(now) {
		t.Fatalf("createdAt=%v", resolved.CreatedAt)
	}
	if !resolved.LastReevaluatedAt.Equal(now.Add(10 * time.Minute)) {
		t.Fatalf("reevaluation clock=%v", resolved.LastReevaluatedAt)
	}
}

func TestAffinityReevaluationDueMatchesBunIntervalAndImmediateThreshold(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	resolution := AffinityResolution{Status: AffinitySelected, LastReevaluatedAt: now}
	if AffinityReevaluationDue(resolution, now.Add(ThreadAffinityReevaluationInterval-time.Nanosecond), false) {
		t.Fatal("reevaluation fired before interval")
	}
	if !AffinityReevaluationDue(resolution, now.Add(ThreadAffinityReevaluationInterval), false) {
		t.Fatal("reevaluation did not fire at interval")
	}
	if !AffinityReevaluationDue(resolution, now.Add(time.Second), true) {
		t.Fatal("over-threshold binding did not re-evaluate immediately")
	}
}
