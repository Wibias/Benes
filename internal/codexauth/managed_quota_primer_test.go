package codexauth

import (
	"context"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type managedQuotaTokenSourceFunc func(context.Context, string) (ManagedToken, error)

func (f managedQuotaTokenSourceFunc) Get(ctx context.Context, accountID string) (ManagedToken, error) {
	return f(ctx, accountID)
}

type managedQuotaFetcherFunc func(context.Context, ManagedToken, string) (WHAMFetchResult, error)

func (f managedQuotaFetcherFunc) Fetch(ctx context.Context, token ManagedToken, plan string) (WHAMFetchResult, error) {
	return f(ctx, token, plan)
}

type managedQuotaSnapshotReader struct {
	mu      sync.Mutex
	records map[string]ManagedCredentialRecord
	status  ManagedCredentialStoreStatus
}

func (r *managedQuotaSnapshotReader) Read() ManagedCredentialSnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	records := make(map[string]ManagedCredentialRecord, len(r.records))
	for id, record := range r.records {
		records[id] = record
	}
	status := r.status
	if status == "" {
		status = ManagedCredentialStoreOK
	}
	return ManagedCredentialSnapshot{Status: status, Records: records}
}

func (r *managedQuotaSnapshotReader) setGeneration(id string, generation int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record := r.records[id]
	record.Generation = generation
	r.records[id] = record
}

func liveManagedQuotaRecord(generation int64) ManagedCredentialRecord {
	credential := ManagedCredential{AccessToken: "stored", RefreshToken: "refresh", ExpiresAtMS: time.Now().Add(time.Hour).UnixMilli(), ChatGPTAccountID: "chat"}
	return ManagedCredentialRecord{Credential: &credential, Generation: generation}
}

func managedQuotaAccounts(ids ...string) ManagedAccountConfig {
	accounts := make([]ManagedAccount, 0, len(ids))
	for _, id := range ids {
		accounts = append(accounts, ManagedAccount{ID: id, Plan: "plus"})
	}
	return ManagedAccountConfig{Accounts: accounts, PausedAccountIDs: map[string]bool{}}
}

func TestManagedQuotaPrimerRefreshesStalePausedButSkipsFreshMainInvalidAndMissingCredentials(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	quotas := NewQuotaState()
	fresh := 20.0
	quotas.SetParsedForCredential("fresh", 1, QuotaReading{WeeklyPercent: &fresh}, 1, now.Add(-4*time.Minute))
	reader := &managedQuotaSnapshotReader{records: map[string]ManagedCredentialRecord{
		"fresh":     liveManagedQuotaRecord(1),
		"stale":     liveManagedQuotaRecord(1),
		"paused":    liveManagedQuotaRecord(1),
		"bad/id":    liveManagedQuotaRecord(1),
		"__proto__": liveManagedQuotaRecord(1),
	}}
	accounts := managedQuotaAccounts("fresh", "stale", "paused", "missing", "bad/id", "__proto__")
	accounts.Accounts = append(accounts.Accounts, ManagedAccount{ID: MainAccountID, IsMain: true, Plan: "plus"})
	accounts.PausedAccountIDs["paused"] = true

	var mu sync.Mutex
	var fetched []string
	primer, err := NewManagedQuotaPrimer(ManagedQuotaPrimerConfig{
		Accounts: accounts,
		Credentials: reader,
		Tokens: managedQuotaTokenSourceFunc(func(_ context.Context, id string) (ManagedToken, error) {
			return ManagedToken{AccessToken: id, ChatGPTAccountID: "chat", Generation: 1}, nil
		}),
		Fetcher: managedQuotaFetcherFunc(func(_ context.Context, token ManagedToken, _ string) (WHAMFetchResult, error) {
			mu.Lock()
			fetched = append(fetched, token.AccessToken)
			mu.Unlock()
			v := 30.0
			return WHAMFetchResult{StatusCode: http.StatusOK, Quota: WHAMQuotaResult{Quota: &QuotaReading{WeeklyPercent: &v}, Plan: "plus"}}, nil
		}),
		Quotas: quotas,
		Reauth: NewReauthState(),
		WriterGeneration: 1,
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	primer.Prime(context.Background())
	mu.Lock()
	sort.Strings(fetched)
	got := append([]string(nil), fetched...)
	mu.Unlock()
	want := []string{"paused", "stale"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("fetched=%#v", got)
	}
}

func TestManagedQuotaPrimerRefreshesAtFiveMinuteBoundary(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	quotas := NewQuotaState()
	v := 10.0
	quotas.SetParsed("a", QuotaReading{WeeklyPercent: &v}, 1, now.Add(-5*time.Minute))
	reader := &managedQuotaSnapshotReader{records: map[string]ManagedCredentialRecord{"a": liveManagedQuotaRecord(1)}}
	calls := 0
	primer, err := NewManagedQuotaPrimer(ManagedQuotaPrimerConfig{
		Accounts: managedQuotaAccounts("a"), Credentials: reader,
		Tokens: managedQuotaTokenSourceFunc(func(context.Context, string) (ManagedToken, error) { return ManagedToken{AccessToken: "a", ChatGPTAccountID: "c", Generation: 1}, nil }),
		Fetcher: managedQuotaFetcherFunc(func(context.Context, ManagedToken, string) (WHAMFetchResult, error) { calls++; return WHAMFetchResult{StatusCode: 500}, nil }),
		Quotas: quotas, Reauth: NewReauthState(), WriterGeneration: 1, Now: func() time.Time { return now },
	})
	if err != nil { t.Fatal(err) }
	primer.Prime(context.Background())
	if calls != 1 { t.Fatalf("calls=%d", calls) }
}

func TestManagedQuotaPrimerIsSingleFlightAndCapsConcurrencyAtFour(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	ids := []string{"a", "b", "c", "d", "e", "f"}
	records := make(map[string]ManagedCredentialRecord)
	for _, id := range ids { records[id] = liveManagedQuotaRecord(1) }
	reader := &managedQuotaSnapshotReader{records: records}
	var current atomic.Int32
	var maximum atomic.Int32
	var calls atomic.Int32
	start := make(chan struct{})
	fetcher := managedQuotaFetcherFunc(func(ctx context.Context, token ManagedToken, plan string) (WHAMFetchResult, error) {
		calls.Add(1)
		n := current.Add(1)
		for {
			old := maximum.Load()
			if n <= old || maximum.CompareAndSwap(old, n) { break }
		}
		defer current.Add(-1)
		select {
		case <-start:
		case <-ctx.Done(): return WHAMFetchResult{}, ctx.Err()
		}
		v := 10.0
		return WHAMFetchResult{StatusCode: http.StatusOK, Quota: WHAMQuotaResult{Quota: &QuotaReading{WeeklyPercent: &v}, Plan: plan}}, nil
	})
	primer, err := NewManagedQuotaPrimer(ManagedQuotaPrimerConfig{
		Accounts: managedQuotaAccounts(ids...), Credentials: reader,
		Tokens: managedQuotaTokenSourceFunc(func(_ context.Context, id string) (ManagedToken, error) { return ManagedToken{AccessToken: id, ChatGPTAccountID: "c", Generation: 1}, nil }),
		Fetcher: fetcher, Quotas: NewQuotaState(), Reauth: NewReauthState(), WriterGeneration: 1, Now: func() time.Time { return now },
	})
	if err != nil { t.Fatal(err) }
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); primer.Prime(context.Background()) }()
	go func() { defer wg.Done(); primer.Prime(context.Background()) }()
	deadline := time.Now().Add(time.Second)
	for calls.Load() < 4 && time.Now().Before(deadline) { time.Sleep(time.Millisecond) }
	close(start)
	wg.Wait()
	if calls.Load() != int32(len(ids)) { t.Fatalf("calls=%d", calls.Load()) }
	if maximum.Load() > 4 { t.Fatalf("max concurrency=%d", maximum.Load()) }
}

func TestManagedQuotaPrimerFencesCredentialGenerationBeforePublishing(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	reader := &managedQuotaSnapshotReader{records: map[string]ManagedCredentialRecord{"a": liveManagedQuotaRecord(1)}}
	quotas := NewQuotaState()
	primer, err := NewManagedQuotaPrimer(ManagedQuotaPrimerConfig{
		Accounts: managedQuotaAccounts("a"), Credentials: reader,
		Tokens: managedQuotaTokenSourceFunc(func(context.Context, string) (ManagedToken, error) { return ManagedToken{AccessToken: "a", ChatGPTAccountID: "c", Generation: 1}, nil }),
		Fetcher: managedQuotaFetcherFunc(func(context.Context, ManagedToken, string) (WHAMFetchResult, error) {
			reader.setGeneration("a", 2)
			v := 77.0
			return WHAMFetchResult{StatusCode: http.StatusOK, Quota: WHAMQuotaResult{Quota: &QuotaReading{WeeklyPercent: &v}, Plan: "plus"}}, nil
		}),
		Quotas: quotas, Reauth: NewReauthState(), WriterGeneration: 1, Now: func() time.Time { return now },
	})
	if err != nil { t.Fatal(err) }
	primer.Prime(context.Background())
	if got := quotas.Get("a"); got != nil { t.Fatalf("stale generation published=%#v", got) }
}

func TestManagedQuotaPrimerMarksOnlyProvenReauthFailures(t *testing.T) {
	tests := []struct {
		name string
		tokenErr error
		status int
		wantReauth bool
	}{
		{name: "wham 401", status: http.StatusUnauthorized, wantReauth: true},
		{name: "wham 403", status: http.StatusForbidden},
		{name: "wham 500", status: http.StatusInternalServerError},
		{name: "refresh rejected", tokenErr: &ManagedTokenRefreshError{Reason: ManagedRefreshRevoked}, wantReauth: true},
		{name: "refresh busy", tokenErr: ErrManagedCredentialRefreshBusy},
		{name: "generation conflict", tokenErr: ErrManagedCredentialGenerationConflict},
		{name: "credential raced away", tokenErr: ErrManagedCredentialUnavailable},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Unix(1_700_000_000, 0)
			reader := &managedQuotaSnapshotReader{records: map[string]ManagedCredentialRecord{"a": liveManagedQuotaRecord(1)}}
			reauth := NewReauthState()
			old := 22.0
			quotas := NewQuotaState()
			quotas.SetParsed("a", QuotaReading{WeeklyPercent: &old}, 1, now.Add(-10*time.Minute))
			primer, err := NewManagedQuotaPrimer(ManagedQuotaPrimerConfig{
				Accounts: managedQuotaAccounts("a"), Credentials: reader,
				Tokens: managedQuotaTokenSourceFunc(func(context.Context, string) (ManagedToken, error) {
					if tc.tokenErr != nil { return ManagedToken{}, tc.tokenErr }
					return ManagedToken{AccessToken: "a", ChatGPTAccountID: "c", Generation: 1}, nil
				}),
				Fetcher: managedQuotaFetcherFunc(func(context.Context, ManagedToken, string) (WHAMFetchResult, error) { return WHAMFetchResult{StatusCode: tc.status}, nil }),
				Quotas: quotas, Reauth: reauth, WriterGeneration: 1, Now: func() time.Time { return now },
			})
			if err != nil { t.Fatal(err) }
			primer.Prime(context.Background())
			if got := reauth.Needs("a"); got != tc.wantReauth { t.Fatalf("reauth=%v want=%v", got, tc.wantReauth) }
			if got := quotas.Get("a"); got == nil || got.WeeklyPercent == nil || *got.WeeklyPercent != 22 { t.Fatalf("old quota not preserved=%#v", got) }
		})
	}
}

func TestManagedQuotaPrimerPublishesFreshQuotaWithConfiguredPlanFallback(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	reader := &managedQuotaSnapshotReader{records: map[string]ManagedCredentialRecord{"a": liveManagedQuotaRecord(4)}}
	quotas := NewQuotaState()
	primer, err := NewManagedQuotaPrimer(ManagedQuotaPrimerConfig{
		Accounts: ManagedAccountConfig{Accounts: []ManagedAccount{{ID: "a", Plan: "free"}}, PausedAccountIDs: map[string]bool{}}, Credentials: reader,
		Tokens: managedQuotaTokenSourceFunc(func(context.Context, string) (ManagedToken, error) { return ManagedToken{AccessToken: "a", ChatGPTAccountID: "c", Generation: 4}, nil }),
		Fetcher: managedQuotaFetcherFunc(func(_ context.Context, _ ManagedToken, plan string) (WHAMFetchResult, error) {
			if plan != "free" { t.Fatalf("plan=%q", plan) }
			monthly := 31.0
			return WHAMFetchResult{StatusCode: http.StatusOK, Quota: WHAMQuotaResult{Quota: &QuotaReading{MonthlyPercent: &monthly}, Plan: plan}}, nil
		}),
		Quotas: quotas, Reauth: NewReauthState(), WriterGeneration: 1, Now: func() time.Time { return now },
	})
	if err != nil { t.Fatal(err) }
	primer.Prime(context.Background())
	got := quotas.Get("a")
	if got == nil || got.MonthlyPercent == nil || *got.MonthlyPercent != 31 { t.Fatalf("quota=%#v", got) }
}

func TestNewManagedQuotaPrimerRejectsMissingDependencies(t *testing.T) {
	if _, err := NewManagedQuotaPrimer(ManagedQuotaPrimerConfig{}); err == nil {
		t.Fatal("expected dependency error")
	}
}
