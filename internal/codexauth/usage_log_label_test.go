package codexauth

import (
	"context"
	"testing"
	"time"
)

func TestUsageLogLabelUsesCanonicalSafeIdentity(t *testing.T) {
	accounts := ManagedAccountConfig{Accounts: []ManagedAccount{
		{ID: "acct-uuid", LogLabel: "alpha"},
		{ID: "acct-other", LogLabel: "beta", IsMain: false},
		{ID: "legacy-main-row", LogLabel: "should-not-win", IsMain: true},
	}}
	if got := UsageLogLabel("acct-uuid", accounts); got != "alpha" {
		t.Fatalf("id vs logLabel: got=%q", got)
	}
	if got := UsageLogLabel(MainAccountID, accounts); got != "main" {
		t.Fatalf("main id: got=%q", got)
	}
	if got := UsageLogLabel("legacy-main-row", accounts); got != "main" {
		t.Fatalf("isMain row: got=%q", got)
	}
	if got := UsageLogLabel("acct-missing", accounts); got != "" {
		t.Fatalf("unknown id invented label=%q", got)
	}
	if got := UsageLogLabel("acct-other", ManagedAccountConfig{Accounts: []ManagedAccount{{ID: "acct-other"}}}); got != "" {
		t.Fatalf("missing logLabel fell back to id: %q", got)
	}
	if got := UsageLogLabel("", accounts); got != "" {
		t.Fatalf("empty id=%q", got)
	}
}

func TestPoolCredentialBindsUsageAccountFromSelectionSnapshot(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	selector := NewPoolSelector(PoolSelectorDependencies{})
	managed := &fakeManagedTokenGetter{token: ManagedToken{AccessToken: "live-b", ChatGPTAccountID: "chat-b", Generation: 2}}
	input := managedPoolSelectionForTest(now, 2, "acct-b")
	input.Accounts.Accounts = []ManagedAccount{{ID: "acct-b", LogLabel: "bravo", Email: "b@example.com"}}
	resolver := newResolverHarness(t, selector, managed, input.Credentials, MainCredentialResult{Status: MainCredentialMissing})
	credential, err := resolver.Resolve(context.Background(), PoolCredentialRequest{Selection: input, WriterGeneration: 4})
	if err != nil {
		t.Fatal(err)
	}
	if credential.UsageAccount != "bravo" {
		t.Fatalf("usage account=%q credential=%#v", credential.UsageAccount, credential)
	}
	input.Accounts.Accounts[0].LogLabel = "changed"
	input.Accounts.Accounts[0].ID = "gone"
	if credential.UsageAccount != "bravo" {
		t.Fatalf("selection mutation rewrote bound label=%q", credential.UsageAccount)
	}

	mainInput := poolSelectionBaseForTest(now)
	mainInput.IncludeMain = true
	mainInput.Main = MainCredentialResult{Status: MainCredentialOK, Credential: MainCredential{AccessToken: "main-access"}}
	main := &fakeMainCredentialReader{result: MainCredentialResult{
		Status:     MainCredentialOK,
		Credential: MainCredential{AccessToken: "main-access", ChatGPTAccountID: "main-chat"},
	}}
	mainResolver, err := NewPoolCredentialResolver(PoolCredentialResolverConfig{
		Selector: selector, ManagedTokens: &fakeManagedTokenGetter{},
		ManagedCredentials: fakeManagedCredentialReader{snapshot: emptyManagedSnapshotForTest()},
		MainCredentials:    main,
	})
	if err != nil {
		t.Fatal(err)
	}
	mainCred, err := mainResolver.Resolve(context.Background(), PoolCredentialRequest{Selection: mainInput, WriterGeneration: 1})
	if err != nil {
		t.Fatal(err)
	}
	if mainCred.UsageAccount != "main" {
		t.Fatalf("main usage account=%q", mainCred.UsageAccount)
	}
}
