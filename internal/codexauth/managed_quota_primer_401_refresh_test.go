package codexauth

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"
)

type managedQuotaTokenForcer struct {
	mu     sync.Mutex
	gets   []string
	forces []string
	get    func(context.Context, string) (ManagedToken, error)
	force  func(context.Context, string) (ManagedToken, error)
}

func (f *managedQuotaTokenForcer) Get(ctx context.Context, accountID string) (ManagedToken, error) {
	f.mu.Lock()
	f.gets = append(f.gets, accountID)
	get := f.get
	f.mu.Unlock()
	return get(ctx, accountID)
}

func (f *managedQuotaTokenForcer) ForceRefresh(ctx context.Context, accountID string) (ManagedToken, error) {
	f.mu.Lock()
	f.forces = append(f.forces, accountID)
	force := f.force
	f.mu.Unlock()
	return force(ctx, accountID)
}

func (f *managedQuotaTokenForcer) snapshot() (gets, forces []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.gets...), append([]string(nil), f.forces...)
}

func TestManagedQuotaPrimerForceRefreshesSameAccountOnceAfterWHAM401(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	reader := &managedQuotaSnapshotReader{records: map[string]ManagedCredentialRecord{
		"acct-a": liveManagedQuotaRecord(1),
		"acct-b": liveManagedQuotaRecord(1),
	}}
	quotas := NewQuotaState()
	old := 44.0
	quotas.SetParsedForCredential("acct-a", 1, QuotaReading{WeeklyPercent: &old}, 1, now.Add(-10*time.Minute))
	reauth := NewReauthState()
	tokens := &managedQuotaTokenForcer{
		get: func(_ context.Context, id string) (ManagedToken, error) {
			return ManagedToken{AccessToken: "stale-" + id, ChatGPTAccountID: "chat", Generation: 1}, nil
		},
		force: func(_ context.Context, id string) (ManagedToken, error) {
			reader.setGeneration(id, 2)
			return ManagedToken{AccessToken: "fresh-" + id, ChatGPTAccountID: "chat", Generation: 2}, nil
		},
	}
	var mu sync.Mutex
	var fetched []string
	primer, err := NewManagedQuotaPrimer(ManagedQuotaPrimerConfig{
		Accounts:    managedQuotaAccounts("acct-a", "acct-b"),
		Credentials: reader,
		Tokens:      tokens,
		Fetcher: managedQuotaFetcherFunc(func(_ context.Context, token ManagedToken, _ string) (WHAMFetchResult, error) {
			mu.Lock()
			fetched = append(fetched, token.AccessToken)
			mu.Unlock()
			if token.AccessToken == "stale-acct-a" {
				return WHAMFetchResult{StatusCode: http.StatusUnauthorized}, nil
			}
			usage := 11.0
			return WHAMFetchResult{StatusCode: http.StatusOK, Quota: WHAMQuotaResult{Quota: &QuotaReading{WeeklyPercent: &usage}, Plan: "plus"}}, nil
		}),
		Quotas:           quotas,
		Reauth:           reauth,
		WriterGeneration: 9,
		Now:              func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	primer.Prime(context.Background())

	gets, forces := tokens.snapshot()
	gotGet := map[string]int{}
	for _, id := range gets {
		gotGet[id]++
	}
	if gotGet["acct-a"] != 1 || gotGet["acct-b"] != 1 {
		t.Fatalf("Get accounts=%v", gets)
	}
	if len(forces) != 1 || forces[0] != "acct-a" {
		t.Fatalf("ForceRefresh=%v", forces)
	}
	mu.Lock()
	gotFetch := append([]string(nil), fetched...)
	mu.Unlock()
	if len(gotFetch) != 3 {
		t.Fatalf("WHAM tokens=%v", gotFetch)
	}
	sawStaleA, sawFreshA, sawB := false, false, false
	for _, token := range gotFetch {
		switch token {
		case "stale-acct-a":
			sawStaleA = true
		case "fresh-acct-a":
			sawFreshA = true
		case "stale-acct-b":
			sawB = true
		case "fresh-acct-b":
			t.Fatal("ForceRefresh hopped to acct-b")
		}
	}
	if !sawStaleA || !sawFreshA || !sawB {
		t.Fatalf("WHAM tokens=%v", gotFetch)
	}
	if reauth.Needs("acct-a") || reauth.Needs("acct-b") {
		t.Fatal("same-account WHAM recovery marked reauth")
	}
	got := quotas.GetForCredential("acct-a", 2)
	if got == nil || got.WeeklyPercent == nil || *got.WeeklyPercent != 11 {
		t.Fatalf("recovered quota=%#v", got)
	}
}

func TestManagedQuotaPrimerMarksReauthOnlyAfterSameAccountWHAM401Replay(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	reader := &managedQuotaSnapshotReader{records: map[string]ManagedCredentialRecord{"acct-a": liveManagedQuotaRecord(1)}}
	reauth := NewReauthState()
	var fetchTokens []string
	var forceCalls int
	tokens := &managedQuotaTokenForcer{
		get: func(context.Context, string) (ManagedToken, error) {
			return ManagedToken{AccessToken: "stale-a", ChatGPTAccountID: "chat", Generation: 1}, nil
		},
		force: func(context.Context, string) (ManagedToken, error) {
			forceCalls++
			reader.setGeneration("acct-a", 2)
			return ManagedToken{AccessToken: "fresh-a", ChatGPTAccountID: "chat", Generation: 2}, nil
		},
	}
	primer, err := NewManagedQuotaPrimer(ManagedQuotaPrimerConfig{
		Accounts:    managedQuotaAccounts("acct-a"),
		Credentials: reader,
		Tokens:      tokens,
		Fetcher: managedQuotaFetcherFunc(func(_ context.Context, token ManagedToken, _ string) (WHAMFetchResult, error) {
			fetchTokens = append(fetchTokens, token.AccessToken)
			return WHAMFetchResult{StatusCode: http.StatusUnauthorized}, nil
		}),
		Quotas:           NewQuotaState(),
		Reauth:           reauth,
		WriterGeneration: 1,
		Now:              func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	primer.Prime(context.Background())
	if forceCalls != 1 {
		t.Fatalf("ForceRefresh calls=%d", forceCalls)
	}
	if len(fetchTokens) != 2 || fetchTokens[0] != "stale-a" || fetchTokens[1] != "fresh-a" {
		t.Fatalf("WHAM tokens=%v", fetchTokens)
	}
	if !reauth.Needs("acct-a") {
		t.Fatal("second WHAM 401 did not mark reauth")
	}
}

func TestManagedQuotaPrimerDoesNotMarkReauthWhenWHAM401RefreshIsNotTerminal(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	tests := []struct {
		name     string
		forceErr error
		wantWHAM int
	}{
		{name: "refresh busy", forceErr: ErrManagedCredentialRefreshBusy, wantWHAM: 1},
		{name: "generation conflict", forceErr: ErrManagedCredentialGenerationConflict, wantWHAM: 1},
		{name: "credential raced away", forceErr: ErrManagedCredentialUnavailable, wantWHAM: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			reader := &managedQuotaSnapshotReader{records: map[string]ManagedCredentialRecord{"acct-a": liveManagedQuotaRecord(1)}}
			reauth := NewReauthState()
			calls := 0
			primer, err := NewManagedQuotaPrimer(ManagedQuotaPrimerConfig{
				Accounts:    managedQuotaAccounts("acct-a"),
				Credentials: reader,
				Tokens: &managedQuotaTokenForcer{
					get: func(context.Context, string) (ManagedToken, error) {
						return ManagedToken{AccessToken: "stale-a", ChatGPTAccountID: "chat", Generation: 1}, nil
					},
					force: func(context.Context, string) (ManagedToken, error) {
						return ManagedToken{}, tc.forceErr
					},
				},
				Fetcher: managedQuotaFetcherFunc(func(context.Context, ManagedToken, string) (WHAMFetchResult, error) {
					calls++
					return WHAMFetchResult{StatusCode: http.StatusUnauthorized}, nil
				}),
				Quotas:           NewQuotaState(),
				Reauth:           reauth,
				WriterGeneration: 1,
				Now:              func() time.Time { return now },
			})
			if err != nil {
				t.Fatal(err)
			}
			primer.Prime(context.Background())
			if calls != tc.wantWHAM {
				t.Fatalf("WHAM calls=%d want=%d", calls, tc.wantWHAM)
			}
			if reauth.Needs("acct-a") {
				t.Fatal("non-terminal refresh marked reauth")
			}
		})
	}
}

func TestManagedQuotaPrimerDoesNotForceRefreshNon401WHAMFailures(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	reader := &managedQuotaSnapshotReader{records: map[string]ManagedCredentialRecord{"acct-a": liveManagedQuotaRecord(1)}}
	reauth := NewReauthState()
	tokens := &managedQuotaTokenForcer{
		get: func(context.Context, string) (ManagedToken, error) {
			return ManagedToken{AccessToken: "stale-a", ChatGPTAccountID: "chat", Generation: 1}, nil
		},
		force: func(context.Context, string) (ManagedToken, error) {
			return ManagedToken{AccessToken: "fresh-a", ChatGPTAccountID: "chat", Generation: 2}, nil
		},
	}
	primer, err := NewManagedQuotaPrimer(ManagedQuotaPrimerConfig{
		Accounts:    managedQuotaAccounts("acct-a"),
		Credentials: reader,
		Tokens:      tokens,
		Fetcher: managedQuotaFetcherFunc(func(context.Context, ManagedToken, string) (WHAMFetchResult, error) {
			return WHAMFetchResult{StatusCode: http.StatusForbidden}, nil
		}),
		Quotas:           NewQuotaState(),
		Reauth:           reauth,
		WriterGeneration: 1,
		Now:              func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	primer.Prime(context.Background())
	if _, forces := tokens.snapshot(); len(forces) != 0 {
		t.Fatalf("ForceRefresh=%v", forces)
	}
	if reauth.Needs("acct-a") {
		t.Fatal("403 marked reauth")
	}
}

func TestManagedQuotaPrimerMarksReauthWhenWHAM401RefreshIsRevoked(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	reader := &managedQuotaSnapshotReader{records: map[string]ManagedCredentialRecord{"acct-a": liveManagedQuotaRecord(1)}}
	reauth := NewReauthState()
	calls := 0
	primer, err := NewManagedQuotaPrimer(ManagedQuotaPrimerConfig{
		Accounts:    managedQuotaAccounts("acct-a"),
		Credentials: reader,
		Tokens: &managedQuotaTokenForcer{
			get: func(context.Context, string) (ManagedToken, error) {
				return ManagedToken{AccessToken: "stale-a", ChatGPTAccountID: "chat", Generation: 1}, nil
			},
			force: func(context.Context, string) (ManagedToken, error) {
				return ManagedToken{}, &ManagedTokenRefreshError{Reason: ManagedRefreshRevoked}
			},
		},
		Fetcher: managedQuotaFetcherFunc(func(context.Context, ManagedToken, string) (WHAMFetchResult, error) {
			calls++
			return WHAMFetchResult{StatusCode: http.StatusUnauthorized}, nil
		}),
		Quotas:           NewQuotaState(),
		Reauth:           reauth,
		WriterGeneration: 1,
		Now:              func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	primer.Prime(context.Background())
	if calls != 1 {
		t.Fatalf("WHAM calls=%d", calls)
	}
	if !reauth.Needs("acct-a") {
		t.Fatal("revoked refresh did not mark reauth")
	}
}

func TestManagedQuotaPrimerDoesNotPublishStaleGenerationAfterWHAM401Refresh(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	reader := &managedQuotaSnapshotReader{records: map[string]ManagedCredentialRecord{"acct-a": liveManagedQuotaRecord(1)}}
	quotas := NewQuotaState()
	reauth := NewReauthState()
	primer, err := NewManagedQuotaPrimer(ManagedQuotaPrimerConfig{
		Accounts:    managedQuotaAccounts("acct-a"),
		Credentials: reader,
		Tokens: &managedQuotaTokenForcer{
			get: func(context.Context, string) (ManagedToken, error) {
				return ManagedToken{AccessToken: "stale-a", ChatGPTAccountID: "chat", Generation: 1}, nil
			},
			force: func(context.Context, string) (ManagedToken, error) {
				reader.setGeneration("acct-a", 2)
				return ManagedToken{AccessToken: "fresh-a", ChatGPTAccountID: "chat", Generation: 2}, nil
			},
		},
		Fetcher: managedQuotaFetcherFunc(func(_ context.Context, token ManagedToken, _ string) (WHAMFetchResult, error) {
			if token.AccessToken == "stale-a" {
				return WHAMFetchResult{StatusCode: http.StatusUnauthorized}, nil
			}
			reader.setGeneration("acct-a", 3)
			usage := 88.0
			return WHAMFetchResult{StatusCode: http.StatusOK, Quota: WHAMQuotaResult{Quota: &QuotaReading{WeeklyPercent: &usage}, Plan: "plus"}}, nil
		}),
		Quotas:           quotas,
		Reauth:           reauth,
		WriterGeneration: 1,
		Now:              func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	primer.Prime(context.Background())
	if got := quotas.Get("acct-a"); got != nil {
		t.Fatalf("stale generation published=%#v", got)
	}
	if reauth.Needs("acct-a") {
		t.Fatal("replaced credential marked reauth")
	}
}
