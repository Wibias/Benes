package codexauth

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestMainQuotaPrimerObserveUpstreamQuotaUsesCurrentIdentityAndPreservesPlan(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	reader := &mainQuotaReaderStub{current: mainQuotaCredential("main-account", "access", "chat")}
	state := NewMainQuotaState()
	primer, err := NewMainQuotaPrimer(MainQuotaPrimerConfig{
		Credentials: reader,
		Fetcher: mainQuotaFetcherFunc(func(context.Context, ManagedToken, string) (WHAMFetchResult, error) {
			return WHAMFetchResult{}, nil
		}),
		State:  state,
		Reauth: NewReauthState(), WriterGeneration: 1, ConfiguredPlan: "free",
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	monthly := 37.0
	if !primer.ObserveUpstreamQuota("main-account", QuotaReading{MonthlyPercent: &monthly, MonthlyIsPrimaryWindow: true}) {
		t.Fatal("live physical main quota observation rejected")
	}
	snapshot := state.Snapshot("main-account")
	if snapshot.Identity != "main-account" || snapshot.Plan != "free" || snapshot.Quota == nil || snapshot.Quota.MonthlyPercent == nil || *snapshot.Quota.MonthlyPercent != 37 || !snapshot.UpdatedAt.Equal(now) {
		t.Fatalf("snapshot=%#v", snapshot)
	}
}

func TestMainQuotaPrimerObserveUpstreamQuotaRejectsStaleIdentity(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	reader := &mainQuotaReaderStub{current: mainQuotaCredential("second", "access", "chat")}
	state := NewMainQuotaState()
	primer, err := NewMainQuotaPrimer(MainQuotaPrimerConfig{
		Credentials: reader,
		Fetcher: mainQuotaFetcherFunc(func(context.Context, ManagedToken, string) (WHAMFetchResult, error) {
			return WHAMFetchResult{}, nil
		}),
		State:  state,
		Reauth: NewReauthState(), WriterGeneration: 1, ConfiguredPlan: "plus",
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	weekly := 22.0
	if primer.ObserveUpstreamQuota("first", QuotaReading{WeeklyPercent: &weekly}) {
		t.Fatal("stale physical identity published live quota")
	}
	if state.Snapshot("first").Identity != "" || state.Snapshot("second").Identity != "" {
		t.Fatal("stale observation left a visible snapshot")
	}
}

func TestMainQuotaPrimerObserveUpstreamQuotaMergesWithExistingWHAMSnapshot(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	reader := &mainQuotaReaderStub{current: mainQuotaCredential("main", "access", "chat")}
	state := NewMainQuotaState()
	primer, err := NewMainQuotaPrimer(MainQuotaPrimerConfig{
		Credentials: reader,
		Fetcher: mainQuotaFetcherFunc(func(context.Context, ManagedToken, string) (WHAMFetchResult, error) {
			weekly, monthly, burst, credits := 10.0, 20.0, 7.0, 2.0
			return WHAMFetchResult{StatusCode: 200, Quota: WHAMQuotaResult{Plan: "pro", Quota: &QuotaReading{
				WeeklyPercent: &weekly, MonthlyPercent: &monthly, ShortPercent: &burst, ResetCredits: &credits,
			}}}, nil
		}),
		State:  state,
		Reauth: NewReauthState(), WriterGeneration: 1, ConfiguredPlan: "plus",
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	primer.Prime(context.Background())
	weekly := 55.0
	if !primer.ObserveUpstreamQuota("main", QuotaReading{WeeklyPercent: &weekly}) {
		t.Fatal("live observation rejected")
	}
	snapshot := state.Snapshot("main")
	if snapshot.Plan != "pro" || snapshot.Quota == nil || snapshot.Quota.WeeklyPercent == nil || *snapshot.Quota.WeeklyPercent != 55 || snapshot.Quota.MonthlyPercent == nil || *snapshot.Quota.MonthlyPercent != 20 || snapshot.Quota.ShortPercent == nil || *snapshot.Quota.ShortPercent != 7 || snapshot.Quota.ResetCredits == nil || *snapshot.Quota.ResetCredits != 2 {
		t.Fatalf("snapshot=%#v", snapshot)
	}

	monthly := 31.0
	if !primer.ObserveUpstreamQuota("main", QuotaReading{MonthlyPercent: &monthly, MonthlyIsPrimaryWindow: true}) {
		t.Fatal("monthly observation rejected")
	}
	snapshot = state.Snapshot("main")
	if snapshot.Quota == nil || snapshot.Quota.WeeklyPercent != nil || snapshot.Quota.MonthlyPercent == nil || *snapshot.Quota.MonthlyPercent != 31 || !snapshot.Quota.MonthlyIsPrimaryWindow {
		t.Fatalf("monthly merge=%#v", snapshot)
	}
}

type blockingObservationMainReader struct {
	mu      sync.Mutex
	current MainCredentialResult
	reads   int
	entered chan struct{}
	release chan struct{}
}

func (r *blockingObservationMainReader) Read(time.Time) MainCredentialResult {
	r.mu.Lock()
	r.reads++
	read := r.reads
	r.mu.Unlock()
	if read == 2 {
		close(r.entered)
		<-r.release
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.current
}

func (r *blockingObservationMainReader) set(result MainCredentialResult) {
	r.mu.Lock()
	r.current = result
	r.mu.Unlock()
}

func TestMainQuotaPrimerObserveUpstreamQuotaCannotEraseConcurrentNewIdentityPublication(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	reader := &blockingObservationMainReader{
		current: mainQuotaCredential("account-a", "access-a", "chat-a"),
		entered: make(chan struct{}), release: make(chan struct{}),
	}
	state := NewMainQuotaState()
	primer, err := NewMainQuotaPrimer(MainQuotaPrimerConfig{
		Credentials: reader,
		Fetcher: mainQuotaFetcherFunc(func(context.Context, ManagedToken, string) (WHAMFetchResult, error) {
			return WHAMFetchResult{}, nil
		}),
		State:  state,
		Reauth: NewReauthState(), WriterGeneration: 1, ConfiguredPlan: "plus",
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}

	weekly := 40.0
	observed := make(chan bool, 1)
	go func() {
		observed <- primer.ObserveUpstreamQuota("account-a", QuotaReading{WeeklyPercent: &weekly})
	}()
	<-reader.entered

	monthly := 5.0
	published := make(chan struct{})
	go func() {
		state.Publish("account-b", "free", &QuotaReading{MonthlyPercent: &monthly}, now)
		close(published)
	}()
	reader.set(mainQuotaCredential("account-b", "access-b", "chat-b"))
	close(reader.release)

	if <-observed {
		t.Fatal("stale identity observation reported success")
	}
	<-published
	got := state.Snapshot("account-b")
	if got.Identity != "account-b" || got.Plan != "free" || got.Quota == nil || got.Quota.MonthlyPercent == nil || *got.Quota.MonthlyPercent != 5 {
		t.Fatalf("newer physical identity snapshot was lost: %#v", got)
	}
}
