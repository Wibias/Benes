package codexauth

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCredentialGenerationLiveUsesMainZeroAndManagedExactGeneration(t *testing.T) {
	if !CredentialGenerationLive(ManagedCredentialSnapshot{}, MainAccountID, 0) {
		t.Fatal("main generation 0 was not live")
	}
	if CredentialGenerationLive(ManagedCredentialSnapshot{}, MainAccountID, 1) {
		t.Fatal("main accepted nonzero generation")
	}

	deletedAt := int64(10)
	snapshot := ManagedCredentialSnapshot{Status: ManagedCredentialStoreOK, Records: map[string]ManagedCredentialRecord{
		"acct":    {Generation: 7, Credential: &ManagedCredential{AccessToken: "a"}},
		"deleted": {Generation: 3, Credential: &ManagedCredential{AccessToken: "d"}, DeletedAtMS: &deletedAt},
		"empty":   {Generation: 2},
	}}
	if !CredentialGenerationLive(snapshot, "acct", 7) {
		t.Fatal("exact managed generation was not live")
	}
	if CredentialGenerationLive(snapshot, "acct", 6) || CredentialGenerationLive(snapshot, "acct", 8) {
		t.Fatal("managed generation mismatch accepted")
	}
	if CredentialGenerationLive(snapshot, "deleted", 3) || CredentialGenerationLive(snapshot, "empty", 2) {
		t.Fatal("deleted/credentialless generation accepted")
	}
	if CredentialGenerationLive(ManagedCredentialSnapshot{Status: ManagedCredentialStoreInvalid, Records: snapshot.Records}, "acct", 7) {
		t.Fatal("invalid store accepted managed generation")
	}
}

func TestThreadAffinityBindsMainAndManagedByScope(t *testing.T) {
	state := NewThreadAffinityState()
	now := time.Unix(1_700_000_000, 0)
	snapshot := ManagedCredentialSnapshot{Status: ManagedCredentialStoreOK, Records: map[string]ManagedCredentialRecord{
		"acct": {Generation: 4, Credential: &ManagedCredential{AccessToken: "a"}},
	}}
	if !state.Bind("thread", MainAccountID, AffinityScopeLegacy, now, snapshot) {
		t.Fatal("main bind failed")
	}
	if !state.Bind("thread", "acct", AffinityScopeSpark, now, snapshot) {
		t.Fatal("managed spark bind failed")
	}

	legacy := state.Resolve("thread", AffinityScopeLegacy, now.Add(time.Minute), snapshot)
	if legacy.Status != AffinitySelected || legacy.AccountID != MainAccountID || legacy.Generation != 0 {
		t.Fatalf("legacy=%#v", legacy)
	}
	spark := state.Resolve("thread", AffinityScopeSpark, now.Add(time.Minute), snapshot)
	if spark.Status != AffinitySelected || spark.AccountID != "acct" || spark.Generation != 4 {
		t.Fatalf("spark=%#v", spark)
	}
	if got := state.Resolve("thread", AffinityScopeShared, now, snapshot); got.Status != AffinityNone {
		t.Fatalf("shared=%#v", got)
	}
}

func TestThreadAffinityRejectsStaleManagedGenerationAndTombstone(t *testing.T) {
	state := NewThreadAffinityState()
	now := time.Unix(1_700_000_000, 0)
	initial := ManagedCredentialSnapshot{Status: ManagedCredentialStoreOK, Records: map[string]ManagedCredentialRecord{
		"acct": {Generation: 4, Credential: &ManagedCredential{AccessToken: "a"}},
	}}
	if !state.Bind("thread", "acct", AffinityScopeShared, now, initial) {
		t.Fatal("bind failed")
	}

	replaced := ManagedCredentialSnapshot{Status: ManagedCredentialStoreOK, Records: map[string]ManagedCredentialRecord{
		"acct": {Generation: 5, Credential: &ManagedCredential{AccessToken: "b"}},
	}}
	if got := state.Resolve("thread", AffinityScopeShared, now.Add(time.Minute), replaced); got.Status != AffinityStale || got.AccountID != "acct" {
		t.Fatalf("replaced resolution=%#v", got)
	}
	if got := state.Resolve("thread", AffinityScopeShared, now.Add(time.Minute), replaced); got.Status != AffinityNone {
		t.Fatalf("stale binding was not deleted: %#v", got)
	}

	deleted := int64(20)
	tombstone := ManagedCredentialSnapshot{Status: ManagedCredentialStoreOK, Records: map[string]ManagedCredentialRecord{
		"acct": {Generation: 6, DeletedAtMS: &deleted},
	}}
	if state.Bind("other", "acct", AffinityScopeShared, now, tombstone) {
		t.Fatal("tombstoned account was bound")
	}
}

func TestThreadAffinityIdleTTLMatchesBunBoundary(t *testing.T) {
	state := NewThreadAffinityState()
	now := time.Unix(1_700_000_000, 0)
	if !state.Bind("thread", MainAccountID, AffinityScopeLegacy, now, ManagedCredentialSnapshot{}) {
		t.Fatal("bind failed")
	}
	atBoundary := state.Resolve("thread", AffinityScopeLegacy, now.Add(ThreadAffinityIdleTTL), ManagedCredentialSnapshot{})
	if atBoundary.Status != AffinitySelected {
		t.Fatalf("exact ttl status=%q", atBoundary.Status)
	}
	justAfter := state.Resolve("thread", AffinityScopeLegacy, now.Add(2*ThreadAffinityIdleTTL+time.Nanosecond), ManagedCredentialSnapshot{})
	if justAfter.Status != AffinityExpired || justAfter.AccountID != MainAccountID {
		t.Fatalf("expired=%#v", justAfter)
	}
	if got := state.Resolve("thread", AffinityScopeLegacy, now.Add(2*ThreadAffinityIdleTTL+time.Second), ManagedCredentialSnapshot{}); got.Status != AffinityNone {
		t.Fatalf("expired binding survived: %#v", got)
	}
}

func TestThreadAffinityRejectsOversizedComponents(t *testing.T) {
	state := NewThreadAffinityState()
	now := time.Now()
	tooLong := strings.Repeat("x", MaxAffinityComponentBytes+1)
	if state.Bind(tooLong, MainAccountID, AffinityScopeLegacy, now, ManagedCredentialSnapshot{}) {
		t.Fatal("oversized thread id was bound")
	}
	if state.Bind("thread", tooLong, AffinityScopeLegacy, now, ManagedCredentialSnapshot{}) {
		t.Fatal("oversized account id was bound")
	}
	if got := state.Resolve(tooLong, AffinityScopeLegacy, now, ManagedCredentialSnapshot{}); got.Status != AffinityNone {
		t.Fatalf("oversized lookup=%#v", got)
	}
}

func TestThreadAffinityPrunesLRUAtCapacity(t *testing.T) {
	state := NewThreadAffinityState()
	base := time.Unix(1_700_000_000, 0)
	for index := 0; index < ThreadAffinityMaxEntries+1; index++ {
		threadID := fmt.Sprintf("thread-%04d", index)
		if !state.Bind(threadID, MainAccountID, AffinityScopeLegacy, base.Add(time.Duration(index)*time.Millisecond), ManagedCredentialSnapshot{}) {
			t.Fatalf("bind %d failed", index)
		}
	}
	if state.Count() != ThreadAffinityMaxEntries {
		t.Fatalf("count=%d", state.Count())
	}
	if got := state.Resolve("thread-0000", AffinityScopeLegacy, base.Add(time.Hour), ManagedCredentialSnapshot{}); got.Status != AffinityNone {
		t.Fatalf("oldest binding survived LRU prune: %#v", got)
	}
	latest := fmt.Sprintf("thread-%04d", ThreadAffinityMaxEntries)
	if got := state.Resolve(latest, AffinityScopeLegacy, base.Add(time.Hour), ManagedCredentialSnapshot{}); got.Status != AffinitySelected {
		t.Fatalf("latest=%#v", got)
	}
}

func TestThreadAffinityClearAccountAndConcurrentAccess(t *testing.T) {
	state := NewThreadAffinityState()
	now := time.Now()
	if !state.Bind("thread-a", MainAccountID, AffinityScopeLegacy, now, ManagedCredentialSnapshot{}) ||
		!state.Bind("thread-b", MainAccountID, AffinityScopeSpark, now, ManagedCredentialSnapshot{}) {
		t.Fatal("initial binds failed")
	}
	state.ClearAccount(MainAccountID)
	if state.Count() != 0 {
		t.Fatalf("count after clear=%d", state.Count())
	}

	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < 250; i++ {
				threadID := fmt.Sprintf("w%d-%d", worker, i%16)
				state.Bind(threadID, MainAccountID, AffinityScopeLegacy, now, ManagedCredentialSnapshot{})
				_ = state.Resolve(threadID, AffinityScopeLegacy, now, ManagedCredentialSnapshot{})
				if i%11 == 0 {
					state.ClearThread(threadID, AffinityScopeLegacy)
				}
			}
		}(worker)
	}
	wg.Wait()
}
