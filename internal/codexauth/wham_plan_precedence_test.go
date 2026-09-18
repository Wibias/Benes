package codexauth

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestMainQuotaPrimerKeepsObservedWHAMPlanAheadOfConfiguredFallback(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	reader := &mainQuotaReaderStub{current: mainQuotaCredential("account", "access", "chat")}
	state := NewMainQuotaState()
	weekly := 20.0
	state.Publish("account", "pro", &QuotaReading{WeeklyPercent: &weekly}, now.Add(-10*time.Minute))

	primer, err := NewMainQuotaPrimer(MainQuotaPrimerConfig{
		Credentials: reader,
		Fetcher: mainQuotaFetcherFunc(func(_ context.Context, _ ManagedToken, fallbackPlan string) (WHAMFetchResult, error) {
			if fallbackPlan != "pro" {
				t.Fatalf("WHAM-observed plan lost to configured fallback: %q", fallbackPlan)
			}
			monthly := 5.0
			return WHAMFetchResult{StatusCode: http.StatusOK, Quota: WHAMQuotaResult{Quota: &QuotaReading{MonthlyPercent: &monthly}}}, nil
		}),
		State: state, Reauth: NewReauthState(), WriterGeneration: 1,
		ConfiguredPlan: "free", Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	primer.Prime(context.Background())
	if got := state.Snapshot("account"); got.Plan != "pro" {
		t.Fatalf("plan=%q want pro", got.Plan)
	}
}
