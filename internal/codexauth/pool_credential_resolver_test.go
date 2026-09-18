package codexauth

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeManagedTokenGetter struct {
	token ManagedToken
	err   error
	calls []string
}

func (f *fakeManagedTokenGetter) Get(ctx context.Context, accountID string) (ManagedToken, error) {
	f.calls = append(f.calls, accountID)
	if err := ctx.Err(); err != nil {
		return ManagedToken{}, err
	}
	return f.token, f.err
}

type fakeManagedCredentialReader struct{ snapshot ManagedCredentialSnapshot }
func (f fakeManagedCredentialReader) Read() ManagedCredentialSnapshot { return f.snapshot }

type fakeMainCredentialReader struct {
	result MainCredentialResult
	calls  int
}
func (f *fakeMainCredentialReader) Read(time.Time) MainCredentialResult { f.calls++; return f.result }

func TestPoolCredentialResolverAcceptsRefreshGenerationAndRejectsStaleReplacement(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	selector := NewPoolSelector(PoolSelectorDependencies{})
	managed := &fakeManagedTokenGetter{token: ManagedToken{AccessToken: "selected-access", ChatGPTAccountID: "chat-selected", Generation: 2}}
	resolver := newResolverHarness(t, selector, managed, managedCredentialSnapshotForTest(2, "acct"), MainCredentialResult{Status: MainCredentialMissing})

	credential, err := resolver.Resolve(context.Background(), PoolCredentialRequest{
		Selection: managedPoolSelectionForTest(now, 1, "acct"), WriterGeneration: 7,
	})
	if err != nil {
		t.Fatal(err)
	}
	if credential.AccountID != "acct" || credential.AccessToken != "selected-access" || credential.ChatGPTAccountID != "chat-selected" || credential.Generation != 2 || credential.WriterGeneration != 7 {
		t.Fatalf("credential=%#v", credential)
	}

	selector = NewPoolSelector(PoolSelectorDependencies{})
	managed = &fakeManagedTokenGetter{token: ManagedToken{AccessToken: "stale", Generation: 2}}
	resolver = newResolverHarness(t, selector, managed, managedCredentialSnapshotForTest(3, "acct"), MainCredentialResult{Status: MainCredentialMissing})
	_, err = resolver.Resolve(context.Background(), PoolCredentialRequest{Selection: managedPoolSelectionForTest(now, 1, "acct")})
	if !errors.Is(err, ErrManagedCredentialGenerationConflict) {
		t.Fatalf("stale err=%v", err)
	}
	if selector.Reauth.Needs("acct") {
		t.Fatal("generation conflict marked reauth")
	}
}

func TestPoolCredentialResolverRereadsPhysicalMainAndFailsClosedIfItVanished(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	selector := NewPoolSelector(PoolSelectorDependencies{})
	main := &fakeMainCredentialReader{result: MainCredentialResult{
		Status: MainCredentialOK,
		Credential: MainCredential{AccessToken: "main-access", ChatGPTAccountID: "main-chat"},
	}}
	managed := &fakeManagedTokenGetter{}
	resolver, err := NewPoolCredentialResolver(PoolCredentialResolverConfig{
		Selector: selector, ManagedTokens: managed,
		ManagedCredentials: fakeManagedCredentialReader{snapshot: emptyManagedSnapshotForTest()},
		MainCredentials: main,
	})
	if err != nil {
		t.Fatal(err)
	}
	input := poolSelectionBaseForTest(now)
	input.IncludeMain = true
	input.Main = MainCredentialResult{Status: MainCredentialOK, Credential: MainCredential{AccessToken: "snapshot-only"}}
	credential, err := resolver.Resolve(context.Background(), PoolCredentialRequest{Selection: input, WriterGeneration: 9})
	if err != nil {
		t.Fatal(err)
	}
	if credential.AccountID != MainAccountID || credential.AccessToken != "main-access" || credential.ChatGPTAccountID != "main-chat" || credential.Generation != 0 || len(managed.calls) != 0 || main.calls != 1 {
		t.Fatalf("credential=%#v managed=%v mainCalls=%d", credential, managed.calls, main.calls)
	}

	main.result = MainCredentialResult{Status: MainCredentialMissing}
	_, err = resolver.Resolve(context.Background(), PoolCredentialRequest{Selection: input})
	if !errors.Is(err, ErrPoolSelectedCredentialUnavailable) {
		t.Fatalf("missing main err=%v", err)
	}
}

func TestPoolCredentialResolverCooldownExpiredAndEmptySelectionFailBeforeTokenIO(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	selector := NewPoolSelector(PoolSelectorDependencies{})
	managed := &fakeManagedTokenGetter{token: ManagedToken{AccessToken: "must-not-run", Generation: 1}}
	resolver := newResolverHarness(t, selector, managed, managedCredentialSnapshotForTest(1, "acct"), MainCredentialResult{Status: MainCredentialMissing})

	selector.Health.SetHardCooldownAt("acct", now, now.Add(time.Hour), CooldownSourceRetryAfter)
	_, err := resolver.Resolve(context.Background(), PoolCredentialRequest{Selection: managedPoolSelectionForTest(now, 1, "acct")})
	var cooldownErr *PoolCooldownError
	if !errors.As(err, &cooldownErr) || !errors.Is(err, ErrPoolOutcomeAwareTransportRequired) || cooldownErr.Source != CooldownSourceRetryAfter || len(managed.calls) != 0 {
		t.Fatalf("cooldown err=%v calls=%v", err, managed.calls)
	}

	_, err = resolver.Resolve(context.Background(), PoolCredentialRequest{Selection: poolSelectionBaseForTest(now)})
	if !errors.Is(err, ErrPoolNoUsableAccount) {
		t.Fatalf("empty err=%v", err)
	}

	selector.Health.ClearAccount("acct")
	expired := managedPoolSelectionForTest(now, 1, "acct")
	expired.ThreadID = "thread"
	if !selector.Affinity.Bind("thread", "acct", AffinityScopeLegacy, now, expired.Credentials) {
		t.Fatal("bind failed")
	}
	expired.Now = now.Add(ThreadAffinityIdleTTL + time.Nanosecond)
	_, err = resolver.Resolve(context.Background(), PoolCredentialRequest{Selection: expired})
	var affinityErr *PoolAffinityExpiredError
	if !errors.As(err, &affinityErr) || affinityErr.AccountID != "acct" {
		t.Fatalf("expired err=%v", err)
	}
}

func TestPoolCredentialResolverMarksOnlyPermanentManagedAuthFailuresForReauth(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	cases := []struct {
		name string
		err error
		mark bool
	}{
		{"revoked", &ManagedTokenRefreshError{Reason: ManagedRefreshRevoked}, true},
		{"unavailable", ErrManagedCredentialUnavailable, true},
		{"busy", ErrManagedCredentialRefreshBusy, false},
		{"generation-conflict", ErrManagedCredentialGenerationConflict, false},
		{"cancelled", context.Canceled, false},
		{"deadline", context.DeadlineExceeded, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			selector := NewPoolSelector(PoolSelectorDependencies{})
			managed := &fakeManagedTokenGetter{err: tc.err}
			resolver := newResolverHarness(t, selector, managed, managedCredentialSnapshotForTest(1, "acct"), MainCredentialResult{Status: MainCredentialMissing})
			_, err := resolver.Resolve(context.Background(), PoolCredentialRequest{Selection: managedPoolSelectionForTest(now, 1, "acct"), WriterGeneration: 11})
			if err == nil {
				t.Fatal("expected error")
			}
			if got := selector.Reauth.Needs("acct"); got != tc.mark {
				t.Fatalf("reauth=%v want=%v err=%v", got, tc.mark, err)
			}
		})
	}
}

func TestPoolCredentialResolverHonorsCancellationBeforeManagedMaterialization(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	selector := NewPoolSelector(PoolSelectorDependencies{})
	managed := &fakeManagedTokenGetter{token: ManagedToken{AccessToken: "unused", Generation: 1}}
	resolver := newResolverHarness(t, selector, managed, managedCredentialSnapshotForTest(1, "acct"), MainCredentialResult{Status: MainCredentialMissing})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := resolver.Resolve(ctx, PoolCredentialRequest{Selection: managedPoolSelectionForTest(now, 1, "acct")})
	if !errors.Is(err, context.Canceled) || selector.Reauth.Needs("acct") {
		t.Fatalf("err=%v reauth=%v", err, selector.Reauth.Needs("acct"))
	}
}

func TestNewPoolCredentialResolverRejectsPartialWiring(t *testing.T) {
	selector := NewPoolSelector(PoolSelectorDependencies{})
	managed := &fakeManagedTokenGetter{}
	reader := fakeManagedCredentialReader{snapshot: emptyManagedSnapshotForTest()}
	main := &fakeMainCredentialReader{}
	for _, config := range []PoolCredentialResolverConfig{
		{},
		{Selector: selector},
		{Selector: selector, ManagedTokens: managed},
		{Selector: selector, ManagedTokens: managed, ManagedCredentials: reader},
		{Selector: selector, ManagedTokens: managed, MainCredentials: main},
	} {
		if _, err := NewPoolCredentialResolver(config); !errors.Is(err, ErrPoolCredentialResolverConfiguration) {
			t.Fatalf("config=%#v err=%v", config, err)
		}
	}
}

func newResolverHarness(t *testing.T, selector *PoolSelector, managed *fakeManagedTokenGetter, snapshot ManagedCredentialSnapshot, main MainCredentialResult) *PoolCredentialResolver {
	t.Helper()
	resolver, err := NewPoolCredentialResolver(PoolCredentialResolverConfig{
		Selector: selector,
		ManagedTokens: managed,
		ManagedCredentials: fakeManagedCredentialReader{snapshot: snapshot},
		MainCredentials: &fakeMainCredentialReader{result: main},
	})
	if err != nil {
		t.Fatal(err)
	}
	return resolver
}

func poolSelectionBaseForTest(now time.Time) PoolSelectionInput {
	threshold := DefaultAutoSwitchThreshold
	failover := 3
	return PoolSelectionInput{
		Accounts: ManagedAccountConfig{PausedAccountIDs: map[string]bool{}, Priorities: map[string]int{}},
		Credentials: emptyManagedSnapshotForTest(),
		Strategy: PoolStrategyQuota, StickyLimit: 1,
		AutoSwitchThreshold: &threshold, FailoverThreshold: &failover,
		Now: now, Quotas: map[string]*QuotaSnapshot{},
	}
}

func managedPoolSelectionForTest(now time.Time, generation int64, accountID string) PoolSelectionInput {
	input := poolSelectionBaseForTest(now)
	input.Accounts.Accounts = []ManagedAccount{{ID: accountID, Plan: "plus"}}
	input.Credentials = managedCredentialSnapshotForTest(generation, accountID)
	return input
}

func managedCredentialSnapshotForTest(generation int64, accountID string) ManagedCredentialSnapshot {
	return ManagedCredentialSnapshot{Status: ManagedCredentialStoreOK, Records: map[string]ManagedCredentialRecord{
		accountID: {Generation: generation, Credential: &ManagedCredential{AccessToken: "snapshot", RefreshToken: "refresh", ChatGPTAccountID: "snapshot-chat"}},
	}}
}

func emptyManagedSnapshotForTest() ManagedCredentialSnapshot {
	return ManagedCredentialSnapshot{Status: ManagedCredentialStoreOK, Records: map[string]ManagedCredentialRecord{}}
}
