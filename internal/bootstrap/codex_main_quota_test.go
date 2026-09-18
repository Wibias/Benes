package bootstrap

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/codexauth"
	"github.com/Wibias/Benes/internal/providerregistry"
	"github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/providers/openairesponses"
)

type bootstrapMainQuotaFetcherFunc func(context.Context, codexauth.ManagedToken, string) (codexauth.WHAMFetchResult, error)

func (f bootstrapMainQuotaFetcherFunc) FetchMain(ctx context.Context, token codexauth.ManagedToken, plan string) (codexauth.WHAMFetchResult, error) {
	return f(ctx, token, plan)
}

func TestBuildCodexPoolRuntimePhysicalMainPrimeFeedsQuotaAwareSelection(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	benesHome := t.TempDir()
	codexHome := t.TempDir()
	writePrivateJSON(t, filepath.Join(benesHome, "codex-accounts.json"), map[string]any{
		"managed": map[string]any{"generation": 1, "credential": map[string]any{
			"accessToken": "managed-access", "refreshToken": "managed-refresh",
			"expiresAt": now.Add(time.Hour).UnixMilli(), "chatgptAccountId": "managed-chat",
		}},
	})
	writePrivateJSON(t, filepath.Join(codexHome, "auth.json"), map[string]any{
		"tokens": map[string]any{"access_token": "main-access", "account_id": "main-chat"},
	})
	disk := poolDiskForTest(t, `{"codexAccounts":[{"id":"managed","plan":"plus"}]}`)
	managedFetcher := bootstrapManagedQuotaFetcherFunc(func(context.Context, codexauth.ManagedToken, string) (codexauth.WHAMFetchResult, error) {
		usage := 50.0
		return codexauth.WHAMFetchResult{StatusCode: http.StatusOK, Quota: codexauth.WHAMQuotaResult{Plan: "plus", Quota: &codexauth.QuotaReading{WeeklyPercent: &usage}}}, nil
	})
	mainFetcher := bootstrapMainQuotaFetcherFunc(func(_ context.Context, token codexauth.ManagedToken, plan string) (codexauth.WHAMFetchResult, error) {
		if token.AccessToken != "main-access" || token.ChatGPTAccountID != "main-chat" || plan != "" {
			t.Fatalf("token=%#v plan=%q", token, plan)
		}
		weekly, monthly := 95.0, 5.0
		return codexauth.WHAMFetchResult{StatusCode: http.StatusOK, Quota: codexauth.WHAMQuotaResult{
			Plan: "free", Quota: &codexauth.QuotaReading{WeeklyPercent: &weekly, MonthlyPercent: &monthly},
		}}, nil
	})

	runtime, err := buildCodexPoolRuntime(context.Background(), disk, []providerregistry.Spec{poolSpecForTest()}, CodexPoolOptions{
		BenesHome: benesHome, CodexHome: codexHome, Now: func() time.Time { return now },
		ManagedQuotaFetcher: managedFetcher, MainQuotaFetcher: mainFetcher,
	})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.primer == nil || runtime.mainPrimer == nil || runtime.mainQuotas == nil {
		t.Fatalf("runtime=%#v", runtime)
	}
	runtime.prime(context.Background())
	credential, err := runtime.authorities["openai"].Resolve(context.Background(), providers.DispatchRequest{
		Parsed: protocolRequestForPoolTest("gpt-5.6"),
	})
	if err != nil {
		t.Fatal(err)
	}
	// Fresh WHAM plan=free makes main score by monthly=5 rather than max(weekly=95, monthly=5).
	if credential.Authorization != "Bearer main-access" || credential.ChatGPTAccountID != "main-chat" {
		t.Fatalf("credential=%#v", credential)
	}
}

func TestMainSelectionQuotaForwardsShortWindow(t *testing.T) {
	weekly, monthly, short := 10.0, 12.0, 100.0
	got := mainSelectionQuota(&codexauth.QuotaReading{
		WeeklyPercent:  &weekly,
		MonthlyPercent: &monthly,
		ShortPercent:   &short,
	})
	if got == nil || got.WeeklyPercent == nil || *got.WeeklyPercent != 10 || got.MonthlyPercent == nil || *got.MonthlyPercent != 12 || got.ShortPercent == nil || *got.ShortPercent != 100 {
		t.Fatalf("projected=%#v", got)
	}

	shortOnly := mainSelectionQuota(&codexauth.QuotaReading{ShortPercent: &short})
	if shortOnly == nil || shortOnly.ShortPercent == nil || *shortOnly.ShortPercent != 100 || shortOnly.WeeklyPercent != nil || shortOnly.MonthlyPercent != nil {
		t.Fatalf("short-only=%#v", shortOnly)
	}
}

func TestCodexPoolSnapshotForwardsMainShortWindowQuota(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	benesHome := t.TempDir()
	codexHome := t.TempDir()
	writePrivateJSON(t, filepath.Join(codexHome, "auth.json"), map[string]any{
		"tokens": map[string]any{"access_token": "main-access", "account_id": "main-account"},
	})
	main, err := codexauth.NewMainCredentialSource(codexHome)
	if err != nil {
		t.Fatal(err)
	}
	store, err := codexauth.NewManagedCredentialStore(benesHome)
	if err != nil {
		t.Fatal(err)
	}
	mainQuotas := codexauth.NewMainQuotaState()
	weekly, monthly, short := 10.0, 8.0, 100.0
	mainQuotas.Publish("main-account", "plus", &codexauth.QuotaReading{
		WeeklyPercent:  &weekly,
		MonthlyPercent: &monthly,
		ShortPercent:   &short,
	}, now)
	source := &codexPoolSnapshotSource{
		store: store, main: main, mainQuotas: mainQuotas, quotas: codexauth.NewQuotaState(),
		now: func() time.Time { return now },
	}
	got, err := source.Snapshot(context.Background(), openairesponses.CodexPoolDispatchIdentity{ModelID: "gpt-5.6"})
	if err != nil {
		t.Fatal(err)
	}
	mainQuota := got.Selection.Quotas[codexauth.MainAccountID]
	if got.Selection.MainPlan != "plus" || mainQuota == nil || mainQuota.ShortPercent == nil || *mainQuota.ShortPercent != 100 || mainQuota.WeeklyPercent == nil || *mainQuota.WeeklyPercent != 10 {
		t.Fatalf("selection=%#v", got.Selection)
	}
}

func TestBuildCodexPoolRuntimeMainShortWindowExhaustionPrefersManaged(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	benesHome := t.TempDir()
	codexHome := t.TempDir()
	writePrivateJSON(t, filepath.Join(benesHome, "codex-accounts.json"), map[string]any{
		"managed": map[string]any{"generation": 1, "credential": map[string]any{
			"accessToken": "managed-access", "refreshToken": "managed-refresh",
			"expiresAt": now.Add(time.Hour).UnixMilli(), "chatgptAccountId": "managed-chat",
		}},
	})
	writePrivateJSON(t, filepath.Join(codexHome, "auth.json"), map[string]any{
		"tokens": map[string]any{"access_token": "main-access", "account_id": "main-chat"},
	})
	disk := poolDiskForTest(t, `{"codexAccounts":[{"id":"managed","plan":"plus"}]}`)
	managedFetcher := bootstrapManagedQuotaFetcherFunc(func(context.Context, codexauth.ManagedToken, string) (codexauth.WHAMFetchResult, error) {
		usage := 40.0
		return codexauth.WHAMFetchResult{StatusCode: http.StatusOK, Quota: codexauth.WHAMQuotaResult{Plan: "plus", Quota: &codexauth.QuotaReading{WeeklyPercent: &usage}}}, nil
	})
	mainFetcher := bootstrapMainQuotaFetcherFunc(func(_ context.Context, token codexauth.ManagedToken, plan string) (codexauth.WHAMFetchResult, error) {
		if token.AccessToken != "main-access" || token.ChatGPTAccountID != "main-chat" {
			t.Fatalf("token=%#v plan=%q", token, plan)
		}
		weekly, monthly, short := 10.0, 8.0, 100.0
		return codexauth.WHAMFetchResult{StatusCode: http.StatusOK, Quota: codexauth.WHAMQuotaResult{
			Plan: "plus", Quota: &codexauth.QuotaReading{WeeklyPercent: &weekly, MonthlyPercent: &monthly, ShortPercent: &short},
		}}, nil
	})

	runtime, err := buildCodexPoolRuntime(context.Background(), disk, []providerregistry.Spec{poolSpecForTest()}, CodexPoolOptions{
		BenesHome: benesHome, CodexHome: codexHome, Now: func() time.Time { return now },
		ManagedQuotaFetcher: managedFetcher, MainQuotaFetcher: mainFetcher,
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime.prime(context.Background())
	credential, err := runtime.authorities["openai"].Resolve(context.Background(), providers.DispatchRequest{
		Parsed: protocolRequestForPoolTest("gpt-5.6"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if credential.Authorization != "Bearer managed-access" || credential.ChatGPTAccountID != "managed-chat" {
		t.Fatalf("short-exhausted Main stayed selected: %#v", credential)
	}
}

func TestCodexPoolSnapshotExposesMainQuotaOnlyForCurrentPhysicalIdentity(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	benesHome := t.TempDir()
	codexHome := t.TempDir()
	writePrivateJSON(t, filepath.Join(codexHome, "auth.json"), map[string]any{
		"tokens": map[string]any{"access_token": "first-access", "account_id": "first-account"},
	})
	main, err := codexauth.NewMainCredentialSource(codexHome)
	if err != nil {
		t.Fatal(err)
	}
	store, err := codexauth.NewManagedCredentialStore(benesHome)
	if err != nil {
		t.Fatal(err)
	}
	mainQuotas := codexauth.NewMainQuotaState()
	weekly := 12.0
	mainQuotas.Publish("first-account", "pro", &codexauth.QuotaReading{WeeklyPercent: &weekly}, now)
	source := &codexPoolSnapshotSource{
		store: store, main: main, mainQuotas: mainQuotas, quotas: codexauth.NewQuotaState(),
		now: func() time.Time { return now },
	}
	first, err := source.Snapshot(context.Background(), openairesponses.CodexPoolDispatchIdentity{ModelID: "gpt-5.6"})
	if err != nil {
		t.Fatal(err)
	}
	mainQuota := first.Selection.Quotas[codexauth.MainAccountID]
	if first.Selection.MainPlan != "pro" || mainQuota == nil || mainQuota.WeeklyPercent == nil || *mainQuota.WeeklyPercent != 12 {
		t.Fatalf("first selection=%#v", first.Selection)
	}

	writePrivateJSON(t, filepath.Join(codexHome, "auth.json"), map[string]any{
		"tokens": map[string]any{"access_token": "second-access", "account_id": "second-account"},
	})
	second, err := source.Snapshot(context.Background(), openairesponses.CodexPoolDispatchIdentity{ModelID: "gpt-5.6"})
	if err != nil {
		t.Fatal(err)
	}
	if second.Selection.Quotas[codexauth.MainAccountID] != nil || second.Selection.MainPlan != "" {
		t.Fatalf("stale main quota/plan escaped identity fence: %#v", second.Selection)
	}
}

func TestBuildCodexPoolAuthoritiesLeavesPhysicalMainPrimeInactive(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	benesHome := t.TempDir()
	codexHome := t.TempDir()
	writePrivateJSON(t, filepath.Join(codexHome, "auth.json"), map[string]any{
		"tokens": map[string]any{"access_token": "main-access", "account_id": "main-account"},
	})
	started := make(chan struct{}, 1)
	fetcher := bootstrapMainQuotaFetcherFunc(func(ctx context.Context, _ codexauth.ManagedToken, _ string) (codexauth.WHAMFetchResult, error) {
		started <- struct{}{}
		<-ctx.Done()
		return codexauth.WHAMFetchResult{}, ctx.Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	authorities, err := buildCodexPoolAuthorities(ctx, poolDiskForTest(t, `{}`), []providerregistry.Spec{poolSpecForTest()}, CodexPoolOptions{
		BenesHome: benesHome, CodexHome: codexHome, Now: func() time.Time { return now }, MainQuotaFetcher: fetcher,
	})
	if err != nil {
		t.Fatal(err)
	}
	if authorities["openai"] == nil {
		t.Fatal("openai Pool authority missing")
	}
	select {
	case <-started:
		t.Fatal("authority construction started physical-main quota I/O")
	case <-time.After(50 * time.Millisecond):
	}
}
