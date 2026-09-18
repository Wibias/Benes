package codexauth

import (
	"context"
	"testing"
	"time"
)

func TestPoolCredentialResolverAlternateSkipsNormalSelectorSideEffects(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	selector := NewPoolSelector(PoolSelectorDependencies{})
	input := poolSelectorFixture(now)
	input.ThreadID = "thread"
	input.ExcludeAccountID = "a"
	input.Accounts.Accounts = []ManagedAccount{{ID: "a", Plan: "plus"}, {ID: "b", Plan: "plus"}}
	input.Accounts.ActiveAccountID = "a"
	input.Credentials = credentialSnapshotFor("a", "b")
	input.Quotas = map[string]*QuotaSnapshot{
		"a": {WeeklyPercent: float64Ptr(10)},
		"b": {WeeklyPercent: float64Ptr(20)},
	}
	if !selector.Affinity.Bind("thread", "a", AffinityScopeLegacy, now, input.Credentials) {
		t.Fatal("bind failed")
	}
	managed := &fakeManagedTokenGetter{token: ManagedToken{AccessToken: "access-b", ChatGPTAccountID: "chat-b", Generation: 1}}
	resolver := newResolverHarness(t, selector, managed, input.Credentials, MainCredentialResult{Status: MainCredentialMissing})

	credential, err := resolver.Resolve(context.Background(), PoolCredentialRequest{
		Selection: input,
		Alternate: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if credential.AccountID != "b" || credential.AccessToken != "access-b" {
		t.Fatalf("credential=%#v", credential)
	}
	if len(managed.calls) != 1 || managed.calls[0] != "b" {
		t.Fatalf("managed calls=%#v", managed.calls)
	}
	if active := selector.RuntimeActive(); active != "" {
		t.Fatalf("alternate materialization ran normal active selection first: %q", active)
	}
	affinity := selector.Affinity.Resolve("thread", AffinityScopeLegacy, now, input.Credentials)
	if affinity.Status != AffinitySelected || affinity.AccountID != "a" {
		t.Fatalf("alternate materialization changed affinity=%#v", affinity)
	}
}
