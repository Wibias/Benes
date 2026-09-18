package codexauth

import (
	"context"
	"errors"
	"testing"
	"time"
)

type forceManagedTokenGetter struct {
	fakeManagedTokenGetter
	forceToken ManagedToken
	forceErr   error
	forceCalls []string
}

func (f *forceManagedTokenGetter) ForceRefresh(ctx context.Context, accountID string) (ManagedToken, error) {
	f.forceCalls = append(f.forceCalls, accountID)
	if err := ctx.Err(); err != nil {
		return ManagedToken{}, err
	}
	if f.forceErr != nil {
		return ManagedToken{}, f.forceErr
	}
	return f.forceToken, nil
}

func TestPoolCredentialResolverForceRefreshPinsStoredAccountAndUsesNewerGeneration(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	selector := NewPoolSelector(PoolSelectorDependencies{})
	managed := &forceManagedTokenGetter{
		fakeManagedTokenGetter: fakeManagedTokenGetter{token: ManagedToken{AccessToken: "stale-a", ChatGPTAccountID: "chat-a", Generation: 1}},
		forceToken:             ManagedToken{AccessToken: "fresh-a", ChatGPTAccountID: "chat-a", Generation: 2},
	}
	resolver := newResolverHarness(t, selector, &managed.fakeManagedTokenGetter, managedCredentialSnapshotForTest(2, "acct-a"), MainCredentialResult{Status: MainCredentialMissing})
	resolver.managedTokens = managed

	input := managedPoolSelectionForTest(now, 1, "acct-a")
	input.Accounts.Accounts = []ManagedAccount{{ID: "acct-a", Plan: "plus"}, {ID: "acct-b", Plan: "plus"}}
	input.Credentials.Records["acct-b"] = ManagedCredentialRecord{
		Generation: 1,
		Credential: &ManagedCredential{AccessToken: "b", RefreshToken: "refresh-b", ChatGPTAccountID: "chat-b"},
	}
	credential, err := resolver.Resolve(context.Background(), PoolCredentialRequest{
		Selection:                 input,
		WriterGeneration:          7,
		ForceStoredAccountRefresh: true,
		RefreshAccountID:          "acct-a",
		FixedAccountID:            "acct-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	if credential.AccountID != "acct-a" || credential.AccessToken != "fresh-a" || credential.Generation != 2 {
		t.Fatalf("credential=%#v", credential)
	}
	if len(managed.calls) != 0 || len(managed.forceCalls) != 1 || managed.forceCalls[0] != "acct-a" {
		t.Fatalf("get=%v force=%v", managed.calls, managed.forceCalls)
	}
}

func TestPoolCredentialResolverForceRefreshDoesNotResurrectLogoutOrReplacement(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	selector := NewPoolSelector(PoolSelectorDependencies{})
	managed := &forceManagedTokenGetter{
		fakeManagedTokenGetter: fakeManagedTokenGetter{token: ManagedToken{AccessToken: "stale-a", Generation: 1}},
		forceErr:               ErrManagedCredentialGenerationConflict,
	}
	resolver := newResolverHarness(t, selector, &managed.fakeManagedTokenGetter, managedCredentialSnapshotForTest(3, "acct-a"), MainCredentialResult{Status: MainCredentialMissing})
	resolver.managedTokens = managed
	_, err := resolver.Resolve(context.Background(), PoolCredentialRequest{
		Selection:                 managedPoolSelectionForTest(now, 1, "acct-a"),
		ForceStoredAccountRefresh: true,
		RefreshAccountID:          "acct-a",
		FixedAccountID:            "acct-a",
	})
	if !errors.Is(err, ErrManagedCredentialGenerationConflict) {
		t.Fatalf("err=%v", err)
	}
	if selector.Reauth.Needs("acct-a") {
		t.Fatal("generation conflict marked reauth")
	}
}
