package codexauth

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type mainQuotaReaderStub struct {
	mu      sync.Mutex
	current MainCredentialResult
}

func (r *mainQuotaReaderStub) Read(time.Time) MainCredentialResult {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.current
}

func (r *mainQuotaReaderStub) set(result MainCredentialResult) {
	r.mu.Lock()
	r.current = result
	r.mu.Unlock()
}

type mainQuotaFetcherFunc func(context.Context, ManagedToken, string) (WHAMFetchResult, error)

func (f mainQuotaFetcherFunc) FetchMain(ctx context.Context, token ManagedToken, plan string) (WHAMFetchResult, error) {
	return f(ctx, token, plan)
}

func mainQuotaCredential(identity, access, account string) MainCredentialResult {
	return MainCredentialResult{
		Status: MainCredentialOK,
		Identity: identity,
		Credential: MainCredential{AccessToken: access, ChatGPTAccountID: account},
	}
}

func TestMainQuotaStateIsIdentityQualifiedAndDefensive(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	weekly := 22.0
	state := NewMainQuotaState()
	state.Publish("account-a", "plus", &QuotaReading{WeeklyPercent: &weekly}, now)

	got := state.Snapshot("account-a")
	if got.Identity != "account-a" || got.Plan != "plus" || got.Quota == nil || got.Quota.WeeklyPercent == nil || *got.Quota.WeeklyPercent != 22 || !got.UpdatedAt.Equal(now) {
		t.Fatalf("snapshot=%#v", got)
	}
	*got.Quota.WeeklyPercent = 99
	again := state.Snapshot("account-a")
	if again.Quota == nil || again.Quota.WeeklyPercent == nil || *again.Quota.WeeklyPercent != 22 {
		t.Fatalf("state leaked mutable quota=%#v", again)
	}
	if other := state.Snapshot("account-b"); other.Identity != "" || other.Plan != "" || other.Quota != nil || !other.UpdatedAt.IsZero() {
		t.Fatalf("different identity inherited main state=%#v", other)
	}
}

func TestMainQuotaPrimerRetriesOnePhysicalIdentitySwitchBeforePublishing(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	reader := &mainQuotaReaderStub{current: mainQuotaCredential("account-a", "access-a", "chat-a")}
	state := NewMainQuotaState()
	reauth := NewReauthState()
	calls := 0
	primer, err := NewMainQuotaPrimer(MainQuotaPrimerConfig{
		Credentials: reader,
		Fetcher: mainQuotaFetcherFunc(func(_ context.Context, token ManagedToken, _ string) (WHAMFetchResult, error) {
			calls++
			if token.AccessToken == "access-a" {
				reader.set(mainQuotaCredential("account-b", "access-b", "chat-b"))
				old := 91.0
				return WHAMFetchResult{StatusCode: http.StatusOK, Quota: WHAMQuotaResult{Plan: "pro", Quota: &QuotaReading{WeeklyPercent: &old}}}, nil
			}
			fresh := 11.0
			return WHAMFetchResult{StatusCode: http.StatusOK, Quota: WHAMQuotaResult{Plan: "free", Quota: &QuotaReading{MonthlyPercent: &fresh}}}, nil
		}),
		State: state,
		Reauth: reauth,
		WriterGeneration: 1,
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	primer.Prime(context.Background())
	if calls != 2 {
		t.Fatalf("fetch calls=%d", calls)
	}
	if stale := state.Snapshot("account-a"); stale.Identity != "" {
		t.Fatalf("stale identity published=%#v", stale)
	}
	got := state.Snapshot("account-b")
	if got.Plan != "free" || got.Quota == nil || got.Quota.MonthlyPercent == nil || *got.Quota.MonthlyPercent != 11 {
		t.Fatalf("fresh identity snapshot=%#v", got)
	}
	if reauth.Needs(MainAccountID) {
		t.Fatal("identity switch must not mark the new main account for reauth")
	}
}

func TestMainQuotaPrimerTerminalAuthClassification(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	for _, tc := range []struct {
		name     string
		result   WHAMFetchResult
		wantNeed bool
	}{
		{name: "401 is terminal", result: WHAMFetchResult{StatusCode: http.StatusUnauthorized, TerminalMainAuth: true}, wantNeed: true},
		{name: "verified 403 is terminal", result: WHAMFetchResult{StatusCode: http.StatusForbidden, TerminalMainAuth: true}, wantNeed: true},
		{name: "generic 403 is not terminal", result: WHAMFetchResult{StatusCode: http.StatusForbidden}, wantNeed: false},
		{name: "500 is not terminal", result: WHAMFetchResult{StatusCode: http.StatusInternalServerError}, wantNeed: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := &mainQuotaReaderStub{current: mainQuotaCredential("account", "access", "chat")}
			state := NewMainQuotaState()
			weekly := 25.0
			state.Publish("account", "plus", &QuotaReading{WeeklyPercent: &weekly}, now.Add(-10*time.Minute))
			reauth := NewReauthState()
			primer, err := NewMainQuotaPrimer(MainQuotaPrimerConfig{
				Credentials: reader,
				Fetcher: mainQuotaFetcherFunc(func(context.Context, ManagedToken, string) (WHAMFetchResult, error) { return tc.result, nil }),
				State: state,
				Reauth: reauth,
				WriterGeneration: 1,
				Now: func() time.Time { return now },
			})
			if err != nil { t.Fatal(err) }
			primer.Prime(context.Background())
			if got := reauth.Needs(MainAccountID); got != tc.wantNeed {
				t.Fatalf("needsReauth=%v want=%v", got, tc.wantNeed)
			}
			if tc.wantNeed {
				if got := state.Snapshot("account"); got.Identity != "" {
					t.Fatalf("terminal auth retained cached main info=%#v", got)
				}
			} else if got := state.Snapshot("account"); got.Identity == "" {
				t.Fatal("non-terminal failure discarded cached main state")
			}
		})
	}
}

func TestMainQuotaPrimerUsesFiveMinuteIdentityCacheAndConfiguredPlanFallback(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	reader := &mainQuotaReaderStub{current: mainQuotaCredential("account", "access", "chat")}
	state := NewMainQuotaState()
	weekly := 30.0
	state.Publish("account", "plus", &QuotaReading{WeeklyPercent: &weekly}, now.Add(-4*time.Minute))
	calls := 0
	primer, err := NewMainQuotaPrimer(MainQuotaPrimerConfig{
		Credentials: reader,
		Fetcher: mainQuotaFetcherFunc(func(_ context.Context, _ ManagedToken, plan string) (WHAMFetchResult, error) {
			calls++
			monthly := 12.0
			return WHAMFetchResult{StatusCode: http.StatusOK, Quota: WHAMQuotaResult{Plan: plan, Quota: &QuotaReading{MonthlyPercent: &monthly}}}, nil
		}),
		State: state,
		Reauth: NewReauthState(),
		WriterGeneration: 1,
		ConfiguredPlan: "free",
		Now: func() time.Time { return now },
	})
	if err != nil { t.Fatal(err) }
	primer.Prime(context.Background())
	if calls != 0 { t.Fatalf("fresh cache fetch calls=%d", calls) }

	state.Publish("account", "", &QuotaReading{WeeklyPercent: &weekly}, now.Add(-5*time.Minute))
	primer.Prime(context.Background())
	if calls != 1 { t.Fatalf("boundary fetch calls=%d", calls) }
	got := state.Snapshot("account")
	if got.Plan != "free" { t.Fatalf("plan=%q", got.Plan) }
}

func TestMainQuotaPrimerIsSingleFlight(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	reader := &mainQuotaReaderStub{current: mainQuotaCredential("account", "access", "chat")}
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	var calls atomic.Int32
	primer, err := NewMainQuotaPrimer(MainQuotaPrimerConfig{
		Credentials: reader,
		Fetcher: mainQuotaFetcherFunc(func(ctx context.Context, _ ManagedToken, _ string) (WHAMFetchResult, error) {
			calls.Add(1)
			started <- struct{}{}
			select {
			case <-release:
				return WHAMFetchResult{StatusCode: http.StatusInternalServerError}, nil
			case <-ctx.Done():
				return WHAMFetchResult{}, ctx.Err()
			}
		}),
		State: NewMainQuotaState(),
		Reauth: NewReauthState(),
		WriterGeneration: 1,
		Now: func() time.Time { return now },
	})
	if err != nil { t.Fatal(err) }

	firstDone := make(chan struct{})
	go func() { primer.Prime(context.Background()); close(firstDone) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first prime did not start")
	}
	secondDone := make(chan struct{})
	go func() { primer.Prime(context.Background()); close(secondDone) }()

	select {
	case <-started:
		t.Fatal("concurrent main prime started a second WHAM fetch")
	case <-time.After(50 * time.Millisecond):
	}
	if calls.Load() != 1 {
		t.Fatalf("fetch calls=%d", calls.Load())
	}
	close(release)
	select {
	case <-firstDone:
	case <-time.After(time.Second):
		t.Fatal("first prime did not finish")
	}
	select {
	case <-secondDone:
	case <-time.After(time.Second):
		t.Fatal("joined prime did not finish")
	}
	if calls.Load() != 1 {
		t.Fatalf("final fetch calls=%d", calls.Load())
	}
}

func TestMainQuotaPrimerMissingCredentialDoesNotFetchOrMarkReauth(t *testing.T) {
	calls := 0
	primer, err := NewMainQuotaPrimer(MainQuotaPrimerConfig{
		Credentials: &mainQuotaReaderStub{current: MainCredentialResult{Status: MainCredentialMissing}},
		Fetcher: mainQuotaFetcherFunc(func(context.Context, ManagedToken, string) (WHAMFetchResult, error) { calls++; return WHAMFetchResult{}, nil }),
		State: NewMainQuotaState(),
		Reauth: NewReauthState(),
		WriterGeneration: 1,
	})
	if err != nil { t.Fatal(err) }
	primer.Prime(context.Background())
	if calls != 0 { t.Fatalf("fetch calls=%d", calls) }
	if primer.reauth.Needs(MainAccountID) { t.Fatal("local missing auth is not proof of terminal reauth") }
}
