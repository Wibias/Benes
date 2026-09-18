package codexauth

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestManagedQuotaPrimerTreatsFreshQuotaFromDifferentCredentialGenerationAsStale(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	quotas := NewQuotaState()
	oldUsage := 8.0
	quotas.SetParsedForCredential("a", 1, QuotaReading{WeeklyPercent: &oldUsage}, 1, now.Add(-time.Minute))
	reader := &managedQuotaSnapshotReader{records: map[string]ManagedCredentialRecord{"a": liveManagedQuotaRecord(2)}}
	calls := 0
	primer, err := NewManagedQuotaPrimer(ManagedQuotaPrimerConfig{
		Accounts: managedQuotaAccounts("a"), Credentials: reader,
		Tokens: managedQuotaTokenSourceFunc(func(context.Context, string) (ManagedToken, error) {
			return ManagedToken{AccessToken: "new", ChatGPTAccountID: "chat", Generation: 2}, nil
		}),
		Fetcher: managedQuotaFetcherFunc(func(context.Context, ManagedToken, string) (WHAMFetchResult, error) {
			calls++
			usage := 17.0
			return WHAMFetchResult{StatusCode: http.StatusOK, Quota: WHAMQuotaResult{Quota: &QuotaReading{WeeklyPercent: &usage}, Plan: "plus"}}, nil
		}),
		Quotas: quotas, Reauth: NewReauthState(), WriterGeneration: 1, Now: func() time.Time { return now },
	})
	if err != nil { t.Fatal(err) }
	primer.Prime(context.Background())
	if calls != 1 { t.Fatalf("fetch calls=%d", calls) }
	got := quotas.GetForCredential("a", 2)
	if got == nil || got.WeeklyPercent == nil || *got.WeeklyPercent != 17 {
		t.Fatalf("new generation quota=%#v", got)
	}
}

func TestManagedQuotaPrimerTagsPublishedQuotaWithMaterializedGeneration(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	reader := &managedQuotaSnapshotReader{records: map[string]ManagedCredentialRecord{"a": liveManagedQuotaRecord(6)}}
	quotas := NewQuotaState()
	primer, err := NewManagedQuotaPrimer(ManagedQuotaPrimerConfig{
		Accounts: managedQuotaAccounts("a"), Credentials: reader,
		Tokens: managedQuotaTokenSourceFunc(func(context.Context, string) (ManagedToken, error) {
			return ManagedToken{AccessToken: "token", ChatGPTAccountID: "chat", Generation: 6}, nil
		}),
		Fetcher: managedQuotaFetcherFunc(func(context.Context, ManagedToken, string) (WHAMFetchResult, error) {
			usage := 23.0
			return WHAMFetchResult{StatusCode: http.StatusOK, Quota: WHAMQuotaResult{Quota: &QuotaReading{WeeklyPercent: &usage}, Plan: "plus"}}, nil
		}),
		Quotas: quotas, Reauth: NewReauthState(), WriterGeneration: 1, Now: func() time.Time { return now },
	})
	if err != nil { t.Fatal(err) }
	primer.Prime(context.Background())
	got := quotas.Get("a")
	if got == nil || got.CredentialGeneration == nil || *got.CredentialGeneration != 6 {
		t.Fatalf("quota generation=%#v", got)
	}
}
