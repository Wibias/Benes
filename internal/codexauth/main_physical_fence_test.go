package codexauth

import (
	"testing"
	"time"
)

func TestMainPhysicalFenceReplacementDoesNotInheritPriorCooldownOrReauth(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	health := NewHealthState()
	failures := NewFailureState()
	reauth := NewReauthState()
	affinity := NewThreadAffinityState()
	fence := NewMainPhysicalFence(health, failures, reauth, affinity)

	fence.Bind("physical-a")
	health.SetHardCooldownAt(MainAccountID, now, now.Add(time.Hour), CooldownSourceDefault)
	reauth.Mark(MainAccountID, 3)
	failures.RecordFailure(MainAccountID, now, 401, 3)
	if !affinity.Bind("thread", MainAccountID, AffinityScopeLegacy, now, ManagedCredentialSnapshot{}) {
		t.Fatal("affinity bind")
	}

	fence.Bind("physical-b")
	if _, ok := health.HardCooldown(MainAccountID, now); ok {
		t.Fatal("replacement inherited Main cooldown")
	}
	if reauth.Needs(MainAccountID) {
		t.Fatal("replacement inherited Main reauth")
	}
	if _, ok := failures.Snapshot(MainAccountID); ok {
		t.Fatal("replacement inherited Main failure streak")
	}
	got := affinity.Resolve("thread", AffinityScopeLegacy, now, ManagedCredentialSnapshot{})
	if got.Status == AffinitySelected && got.AccountID == MainAccountID {
		t.Fatalf("replacement inherited Main affinity: %#v", got)
	}
}

func TestMainPhysicalFenceLatePriorIdentityCannotPoisonReplacement(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	recorder := NewOutcomeRecorder(OutcomeRecorderDependencies{})
	recorder.MainFence.Bind("physical-a")
	recorder.Record(MainAccountID, HTTPOutcome(429), OutcomeMeta{
		Now: now, RetryAfter: "120", MainIdentity: "physical-a", WriterGeneration: 1,
	})
	if _, ok := recorder.Health.HardCooldown(MainAccountID, now); !ok {
		t.Fatal("expected cooldown for physical A")
	}

	recorder.MainFence.Bind("physical-b")
	if _, ok := recorder.Health.HardCooldown(MainAccountID, now); ok {
		t.Fatal("B inherited A cooldown")
	}

	class := recorder.Record(MainAccountID, HTTPOutcome(401), OutcomeMeta{
		Now: now.Add(time.Second), MainIdentity: "physical-a", WriterGeneration: 1,
	})
	if class != OutcomeCredential {
		t.Fatalf("class=%s", class)
	}
	if recorder.Reauth.Needs(MainAccountID) {
		t.Fatal("late A 401 marked reauth on B")
	}
	if _, ok := recorder.Health.HardCooldown(MainAccountID, now.Add(time.Second)); ok {
		t.Fatal("late A outcome restored cooldown on B")
	}
}

func TestMainPhysicalFenceSelectorUnblocksReplacementAfterPriorCooldown(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	selector := NewPoolSelector(PoolSelectorDependencies{})
	selector.MainFence.Bind("physical-a")
	selector.Health.SetHardCooldownAt(MainAccountID, now, now.Add(time.Hour), CooldownSourceDefault)
	input := poolSelectionBaseForTest(now)
	input.IncludeMain = true
	input.Main = MainCredentialResult{
		Status: MainCredentialOK, Identity: "physical-b",
		Credential: MainCredential{AccessToken: "b-token", ChatGPTAccountID: "chat-b"},
	}
	input.Accounts.Accounts = nil
	got := selector.Resolve(input)
	if got.Status != PoolSelectionSelected || got.AccountID != MainAccountID {
		t.Fatalf("replacement Main stayed blocked: %#v", got)
	}
	if _, ok := selector.Health.HardCooldown(MainAccountID, now); ok {
		t.Fatal("selector left prior Main cooldown in place")
	}
}

func TestMainPhysicalFenceKeepsLogicalSlotPauseSeparateFromPhysicalEvidence(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	selector := NewPoolSelector(PoolSelectorDependencies{})
	selector.Health.SetHardCooldownAt(MainAccountID, now, now.Add(time.Hour), CooldownSourceDefault)
	selector.MainFence.Bind("physical-a")
	selector.MainFence.Bind("physical-b")

	input := poolSelectionBaseForTest(now)
	input.IncludeMain = true
	input.Main = MainCredentialResult{
		Status: MainCredentialOK, Identity: "physical-b",
		Credential: MainCredential{AccessToken: "b-token", ChatGPTAccountID: "chat-b"},
	}
	input.Accounts.PausedAccountIDs = map[string]bool{MainAccountID: true}
	got := selector.Resolve(input)
	if got.Status == PoolSelectionSelected && got.AccountID == MainAccountID {
		t.Fatalf("paused logical Main slot was selected: %#v", got)
	}
}
