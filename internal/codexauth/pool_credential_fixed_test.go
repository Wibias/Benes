package codexauth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPoolCredentialResolverFixedAccountBypassesPoolStateAndMarksCredentialFixed(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	selector := NewPoolSelector(PoolSelectorDependencies{})
	input := poolSelectionBaseForTest(now)
	input.ThreadID = "thread"
	input.Accounts.Accounts = []ManagedAccount{{ID: "a", Plan: "plus"}, {ID: "b", Plan: "plus"}}
	input.Accounts.ActiveAccountID = "a"
	input.Credentials = managedCredentialSnapshotForTest(1, "a")
	input.Credentials.Records["b"] = ManagedCredentialRecord{Generation: 2, Credential: &ManagedCredential{AccessToken: "snapshot-b", RefreshToken: "refresh-b", ChatGPTAccountID: "chat-b"}}
	if !selector.Affinity.Bind("thread", "a", AffinityScopeLegacy, now, input.Credentials) {
		t.Fatal("bind failed")
	}
	managed := &fakeManagedTokenGetter{token: ManagedToken{AccessToken: "live-b", ChatGPTAccountID: "chat-b", Generation: 2}}
	resolver := newResolverHarness(t, selector, managed, input.Credentials, MainCredentialResult{Status: MainCredentialMissing})

	credential, err := resolver.Resolve(context.Background(), PoolCredentialRequest{
		Selection: input, FixedAccountID: "b", WriterGeneration: 7, OutcomeAware: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if credential.AccountID != "b" || credential.AccessToken != "live-b" || !credential.FixedAccount {
		t.Fatalf("credential=%#v", credential)
	}
	if len(managed.calls) != 1 || managed.calls[0] != "b" {
		t.Fatalf("managed calls=%#v", managed.calls)
	}
	if active := selector.RuntimeActive(); active != "" {
		t.Fatalf("fixed account moved runtime active=%q", active)
	}
	affinity := selector.Affinity.Resolve("thread", AffinityScopeLegacy, now, input.Credentials)
	if affinity.Status != AffinitySelected || affinity.AccountID != "a" {
		t.Fatalf("fixed account changed affinity=%#v", affinity)
	}
}

func TestPoolCredentialResolverFixedAccountFailsClosedAcrossRoutingFences(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	for _, tc := range []struct {
		name string
		mark func(*PoolSelector, *PoolSelectionInput)
	}{
		{name: "paused", mark: func(_ *PoolSelector, input *PoolSelectionInput) { input.Accounts.PausedAccountIDs["b"] = true }},
		{name: "reauth", mark: func(selector *PoolSelector, _ *PoolSelectionInput) { selector.Reauth.Mark("b", 1) }},
		{name: "soft avoid", mark: func(selector *PoolSelector, _ *PoolSelectionInput) { selector.Health.SetSoftAvoid("b", now.Add(time.Minute)) }},
		{name: "hard cooldown", mark: func(selector *PoolSelector, _ *PoolSelectionInput) { selector.Health.SetHardCooldownAt("b", now, now.Add(time.Minute), CooldownSourceRetryAfter) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			selector := NewPoolSelector(PoolSelectorDependencies{})
			input := poolSelectionBaseForTest(now)
			input.Accounts.Accounts = []ManagedAccount{{ID: "a"}, {ID: "b"}}
			input.Credentials = managedCredentialSnapshotForTest(1, "a")
			input.Credentials.Records["b"] = ManagedCredentialRecord{Generation: 1, Credential: &ManagedCredential{AccessToken: "snapshot-b", RefreshToken: "refresh-b"}}
			tc.mark(selector, &input)
			managed := &fakeManagedTokenGetter{token: ManagedToken{AccessToken: "must-not-run", Generation: 1}}
			resolver := newResolverHarness(t, selector, managed, input.Credentials, MainCredentialResult{Status: MainCredentialMissing})

			_, err := resolver.Resolve(context.Background(), PoolCredentialRequest{Selection: input, FixedAccountID: "b", OutcomeAware: true})
			if err == nil {
				t.Fatal("expected fixed-account rejection")
			}
			if tc.name == "hard cooldown" {
				var cooldown *PoolCooldownError
				if !errors.As(err, &cooldown) || cooldown.AccountID != "b" {
					t.Fatalf("cooldown err=%v", err)
				}
			}
			if len(managed.calls) != 0 {
				t.Fatalf("fixed rejected account reached token IO: %#v", managed.calls)
			}
		})
	}
}

func TestPoolCredentialResolverFixedMainUsesPhysicalMainWithoutPoolSelection(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	selector := NewPoolSelector(PoolSelectorDependencies{})
	input := poolSelectionBaseForTest(now)
	input.IncludeMain = true
	input.Main = MainCredentialResult{Status: MainCredentialOK, Credential: MainCredential{AccessToken: "snapshot-main"}}
	managed := &fakeManagedTokenGetter{}
	main := &fakeMainCredentialReader{result: MainCredentialResult{Status: MainCredentialOK, Credential: MainCredential{AccessToken: "live-main", ChatGPTAccountID: "main-chat"}, Identity: "physical-main"}}
	resolver, err := NewPoolCredentialResolver(PoolCredentialResolverConfig{Selector: selector, ManagedTokens: managed, ManagedCredentials: fakeManagedCredentialReader{snapshot: emptyManagedSnapshotForTest()}, MainCredentials: main})
	if err != nil {
		t.Fatal(err)
	}
	credential, err := resolver.Resolve(context.Background(), PoolCredentialRequest{Selection: input, FixedAccountID: MainAccountID})
	if err != nil {
		t.Fatal(err)
	}
	if credential.AccountID != MainAccountID || credential.AccessToken != "live-main" || credential.MainIdentity != "physical-main" || !credential.FixedAccount || main.calls != 1 || len(managed.calls) != 0 {
		t.Fatalf("credential=%#v mainCalls=%d managed=%#v", credential, main.calls, managed.calls)
	}
}
