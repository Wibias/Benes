package codexauth

import (
	"context"
	"testing"
	"time"
)

func TestPoolCredentialResolverSignalsOnlySelectedMissingQuota(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	for _, tc := range []struct {
		name       string
		quota      map[string]*QuotaSnapshot
		wantCalls  int
	}{
		{name: "missing selected quota", quota: map[string]*QuotaSnapshot{}, wantCalls: 1},
		{name: "known selected quota", quota: map[string]*QuotaSnapshot{"acct": {WeeklyPercent: float64PtrForLazyPrime(20)}}, wantCalls: 0},
		{name: "known empty quota snapshot", quota: map[string]*QuotaSnapshot{"acct": {}}, wantCalls: 0},
		{name: "only other account known", quota: map[string]*QuotaSnapshot{"other": {}}, wantCalls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			selected := ""
			resolver, err := NewPoolCredentialResolver(PoolCredentialResolverConfig{
				Selector: NewPoolSelector(PoolSelectorDependencies{}),
				ManagedTokens: &fakeManagedTokenGetter{token: ManagedToken{AccessToken: "selected", ChatGPTAccountID: "chat", Generation: 1}},
				ManagedCredentials: fakeManagedCredentialReader{snapshot: managedCredentialSnapshotForTest(1, "acct")},
				MainCredentials: &fakeMainCredentialReader{result: MainCredentialResult{Status: MainCredentialMissing}},
				OnSelectedQuotaMissing: func(accountID string) {
					calls++
					selected = accountID
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			input := managedPoolSelectionForTest(now, 1, "acct")
			input.Quotas = tc.quota
			credential, err := resolver.Resolve(context.Background(), PoolCredentialRequest{Selection: input})
			if err != nil {
				t.Fatal(err)
			}
			if credential.AccountID != "acct" || calls != tc.wantCalls {
				t.Fatalf("credential=%#v calls=%d want=%d", credential, calls, tc.wantCalls)
			}
			if tc.wantCalls == 1 && selected != "acct" {
				t.Fatalf("selected callback account=%q", selected)
			}
		})
	}
}

func TestPoolCredentialResolverDoesNotSignalWhenSelectionFails(t *testing.T) {
	calls := 0
	resolver, err := NewPoolCredentialResolver(PoolCredentialResolverConfig{
		Selector: NewPoolSelector(PoolSelectorDependencies{}),
		ManagedTokens: &fakeManagedTokenGetter{},
		ManagedCredentials: fakeManagedCredentialReader{snapshot: emptyManagedSnapshotForTest()},
		MainCredentials: &fakeMainCredentialReader{result: MainCredentialResult{Status: MainCredentialMissing}},
		OnSelectedQuotaMissing: func(string) { calls++ },
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = resolver.Resolve(context.Background(), PoolCredentialRequest{Selection: poolSelectionBaseForTest(time.Unix(1_700_000_000, 0))})
	if err == nil {
		t.Fatal("expected empty-pool failure")
	}
	if calls != 0 {
		t.Fatalf("failed selection triggered quota prime calls=%d", calls)
	}
}

func float64PtrForLazyPrime(value float64) *float64 { return &value }
