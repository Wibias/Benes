package codexauth

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func TestManagedQuotaPrimerBacksOffProviderDispatchedFailures(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	clock := now
	reader := &managedQuotaSnapshotReader{records: map[string]ManagedCredentialRecord{"a": liveManagedQuotaRecord(1)}}
	var calls atomic.Int32
	primer, err := NewManagedQuotaPrimer(ManagedQuotaPrimerConfig{
		Accounts:    managedQuotaAccounts("a"),
		Credentials: reader,
		Tokens: managedQuotaTokenSourceFunc(func(context.Context, string) (ManagedToken, error) {
			return ManagedToken{AccessToken: "a", ChatGPTAccountID: "c", Generation: 1}, nil
		}),
		Fetcher: managedQuotaFetcherFunc(func(context.Context, ManagedToken, string) (WHAMFetchResult, error) {
			calls.Add(1)
			return WHAMFetchResult{StatusCode: http.StatusServiceUnavailable}, nil
		}),
		Quotas: NewQuotaState(), Reauth: NewReauthState(), WriterGeneration: 1,
		Now: func() time.Time { return clock },
	})
	if err != nil {
		t.Fatal(err)
	}
	primer.Prime(context.Background())
	primer.Prime(context.Background())
	if calls.Load() != 1 {
		t.Fatalf("calls=%d want 1 during backoff", calls.Load())
	}
	clock = now.Add(defaultManagedQuotaFailureBackoff)
	primer.Prime(context.Background())
	if calls.Load() != 2 {
		t.Fatalf("calls=%d want 2 after backoff", calls.Load())
	}
}

func TestManagedQuotaPrimerTransportErrorUsesSameBackoff(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	clock := now
	reader := &managedQuotaSnapshotReader{records: map[string]ManagedCredentialRecord{"a": liveManagedQuotaRecord(1)}}
	var calls atomic.Int32
	primer, err := NewManagedQuotaPrimer(ManagedQuotaPrimerConfig{
		Accounts:    managedQuotaAccounts("a"),
		Credentials: reader,
		Tokens: managedQuotaTokenSourceFunc(func(context.Context, string) (ManagedToken, error) {
			return ManagedToken{AccessToken: "a", ChatGPTAccountID: "c", Generation: 1}, nil
		}),
		Fetcher: managedQuotaFetcherFunc(func(context.Context, ManagedToken, string) (WHAMFetchResult, error) {
			calls.Add(1)
			return WHAMFetchResult{}, errors.New("wham transport down")
		}),
		Quotas: NewQuotaState(), Reauth: NewReauthState(), WriterGeneration: 1,
		Now: func() time.Time { return clock },
	})
	if err != nil {
		t.Fatal(err)
	}
	primer.Prime(context.Background())
	primer.Prime(context.Background())
	if calls.Load() != 1 {
		t.Fatalf("calls=%d", calls.Load())
	}
}

func TestManagedQuotaPrimerGenerationChangeClearsFailureBackoff(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	reader := &managedQuotaSnapshotReader{records: map[string]ManagedCredentialRecord{"a": liveManagedQuotaRecord(1)}}
	var calls atomic.Int32
	var generation atomic.Int64
	generation.Store(1)
	primer, err := NewManagedQuotaPrimer(ManagedQuotaPrimerConfig{
		Accounts:    managedQuotaAccounts("a"),
		Credentials: reader,
		Tokens: managedQuotaTokenSourceFunc(func(context.Context, string) (ManagedToken, error) {
			gen := generation.Load()
			return ManagedToken{AccessToken: "a", ChatGPTAccountID: "c", Generation: gen}, nil
		}),
		Fetcher: managedQuotaFetcherFunc(func(context.Context, ManagedToken, string) (WHAMFetchResult, error) {
			calls.Add(1)
			return WHAMFetchResult{StatusCode: http.StatusBadGateway}, nil
		}),
		Quotas: NewQuotaState(), Reauth: NewReauthState(), WriterGeneration: 1,
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	primer.Prime(context.Background())
	generation.Store(2)
	reader.setGeneration("a", 2)
	primer.Prime(context.Background())
	if calls.Load() != 2 {
		t.Fatalf("calls=%d want 2 after generation change", calls.Load())
	}
}

func TestManagedQuotaPrimerDoesNotBackoffLocalAdmissionDeferral(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	reader := &managedQuotaSnapshotReader{records: map[string]ManagedCredentialRecord{"a": liveManagedQuotaRecord(1)}}
	var gets atomic.Int32
	var fetches atomic.Int32
	primer, err := NewManagedQuotaPrimer(ManagedQuotaPrimerConfig{
		Accounts:    managedQuotaAccounts("a"),
		Credentials: reader,
		Tokens: managedQuotaTokenSourceFunc(func(context.Context, string) (ManagedToken, error) {
			gets.Add(1)
			return ManagedToken{}, ErrManagedCredentialRefreshBusy
		}),
		Fetcher: managedQuotaFetcherFunc(func(context.Context, ManagedToken, string) (WHAMFetchResult, error) {
			fetches.Add(1)
			return WHAMFetchResult{StatusCode: 503}, nil
		}),
		Quotas: NewQuotaState(), Reauth: NewReauthState(), WriterGeneration: 1,
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	primer.Prime(context.Background())
	primer.Prime(context.Background())
	if gets.Load() != 2 {
		t.Fatalf("gets=%d want 2", gets.Load())
	}
	if fetches.Load() != 0 {
		t.Fatalf("fetches=%d", fetches.Load())
	}
}

func TestManagedQuotaPrimerDoesNotBackoffCallerCancelBeforeDispatch(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	reader := &managedQuotaSnapshotReader{records: map[string]ManagedCredentialRecord{"a": liveManagedQuotaRecord(1)}}
	var calls atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	primer, err := NewManagedQuotaPrimer(ManagedQuotaPrimerConfig{
		Accounts:    managedQuotaAccounts("a"),
		Credentials: reader,
		Tokens: managedQuotaTokenSourceFunc(func(context.Context, string) (ManagedToken, error) {
			return ManagedToken{AccessToken: "a", ChatGPTAccountID: "c", Generation: 1}, nil
		}),
		Fetcher: managedQuotaFetcherFunc(func(context.Context, ManagedToken, string) (WHAMFetchResult, error) {
			calls.Add(1)
			return WHAMFetchResult{StatusCode: 503}, nil
		}),
		Quotas: NewQuotaState(), Reauth: NewReauthState(), WriterGeneration: 1,
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	primer.Prime(ctx)
	if calls.Load() != 0 {
		t.Fatalf("canceled prime dispatched: %d", calls.Load())
	}
	primer.Prime(context.Background())
	if calls.Load() != 1 {
		t.Fatalf("cancel poisoned later prime: %d", calls.Load())
	}
}

func TestManagedQuotaPrimerSuccessClearsFailureBackoff(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	clock := now
	reader := &managedQuotaSnapshotReader{records: map[string]ManagedCredentialRecord{"a": liveManagedQuotaRecord(1)}}
	var calls atomic.Int32
	primer, err := NewManagedQuotaPrimer(ManagedQuotaPrimerConfig{
		Accounts:    managedQuotaAccounts("a"),
		Credentials: reader,
		Tokens: managedQuotaTokenSourceFunc(func(context.Context, string) (ManagedToken, error) {
			return ManagedToken{AccessToken: "a", ChatGPTAccountID: "c", Generation: 1}, nil
		}),
		Fetcher: managedQuotaFetcherFunc(func(context.Context, ManagedToken, string) (WHAMFetchResult, error) {
			n := calls.Add(1)
			if n == 1 {
				return WHAMFetchResult{StatusCode: 503}, nil
			}
			v := 12.0
			return WHAMFetchResult{StatusCode: http.StatusOK, Quota: WHAMQuotaResult{Quota: &QuotaReading{WeeklyPercent: &v}, Plan: "plus"}}, nil
		}),
		Quotas: NewQuotaState(), Reauth: NewReauthState(), WriterGeneration: 1,
		Now: func() time.Time { return clock },
	})
	if err != nil {
		t.Fatal(err)
	}
	primer.Prime(context.Background())
	clock = now.Add(defaultManagedQuotaFailureBackoff)
	primer.Prime(context.Background())
	clock = now.Add(defaultManagedQuotaFailureBackoff + defaultManagedQuotaTTL)
	primer.Prime(context.Background())
	if calls.Load() != 3 {
		t.Fatalf("calls=%d want 3", calls.Load())
	}
}
