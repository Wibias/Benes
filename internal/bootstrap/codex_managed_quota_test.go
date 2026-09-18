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

type bootstrapManagedQuotaFetcherFunc func(context.Context, codexauth.ManagedToken, string) (codexauth.WHAMFetchResult, error)

func (f bootstrapManagedQuotaFetcherFunc) Fetch(ctx context.Context, token codexauth.ManagedToken, plan string) (codexauth.WHAMFetchResult, error) {
	return f(ctx, token, plan)
}

func TestCodexPoolSnapshotCarriesOnlyCurrentManagedCredentialQuota(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	benesHome := t.TempDir()
	writePrivateJSON(t, filepath.Join(benesHome, "codex-accounts.json"), map[string]any{
		"managed": map[string]any{"generation": 2, "credential": map[string]any{
			"accessToken": "access", "refreshToken": "refresh",
			"expiresAt": now.Add(time.Hour).UnixMilli(), "chatgptAccountId": "chat",
		}},
	})
	store, err := codexauth.NewManagedCredentialStore(benesHome)
	if err != nil {
		t.Fatal(err)
	}
	main, err := codexauth.NewMainCredentialSource(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	quotas := codexauth.NewQuotaState()
	weekly := 37.0
	quotas.SetParsedForCredential("managed", 2, codexauth.QuotaReading{WeeklyPercent: &weekly}, 1, now)
	source := &codexPoolSnapshotSource{
		accounts: codexauth.ManagedAccountConfig{Accounts: []codexauth.ManagedAccount{{ID: "managed", Plan: "plus"}}},
		store:    store,
		main:     main,
		quotas:   quotas,
		now:      func() time.Time { return now },
	}
	request, err := source.Snapshot(context.Background(), openairesponses.CodexPoolDispatchIdentity{ModelID: "gpt-5.6"})
	if err != nil {
		t.Fatal(err)
	}
	quota := request.Selection.Quotas["managed"]
	if quota == nil || quota.WeeklyPercent == nil || *quota.WeeklyPercent != 37 {
		t.Fatalf("quotas=%#v", request.Selection.Quotas)
	}

	writePrivateJSON(t, filepath.Join(benesHome, "codex-accounts.json"), map[string]any{
		"managed": map[string]any{"generation": 3, "credential": map[string]any{
			"accessToken": "replacement", "refreshToken": "replacement-refresh",
			"expiresAt": now.Add(time.Hour).UnixMilli(), "chatgptAccountId": "replacement-chat",
		}},
	})
	request, err = source.Snapshot(context.Background(), openairesponses.CodexPoolDispatchIdentity{ModelID: "gpt-5.6"})
	if err != nil {
		t.Fatal(err)
	}
	if stale := request.Selection.Quotas["managed"]; stale != nil {
		t.Fatalf("replacement credential inherited stale quota=%#v", stale)
	}
}

func TestBuildCodexPoolRuntimeManagedPrimeMakesSelectionQuotaAware(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	benesHome := t.TempDir()
	codexHome := t.TempDir()
	writePrivateJSON(t, filepath.Join(benesHome, "codex-accounts.json"), map[string]any{
		"a": map[string]any{"generation": 1, "credential": map[string]any{"accessToken": "access-a", "refreshToken": "refresh-a", "expiresAt": now.Add(time.Hour).UnixMilli(), "chatgptAccountId": "chat-a"}},
		"b": map[string]any{"generation": 1, "credential": map[string]any{"accessToken": "access-b", "refreshToken": "refresh-b", "expiresAt": now.Add(time.Hour).UnixMilli(), "chatgptAccountId": "chat-b"}},
	})
	disk := poolDiskForTest(t, `{
		"codexAccounts":[
			{"id":"a","plan":"plus"},
			{"id":"b","plan":"plus"}
		]
	}`)
	fetcher := bootstrapManagedQuotaFetcherFunc(func(_ context.Context, token codexauth.ManagedToken, plan string) (codexauth.WHAMFetchResult, error) {
		usage := 90.0
		if token.AccessToken == "access-b" {
			usage = 10
		}
		return codexauth.WHAMFetchResult{
			StatusCode: http.StatusOK,
			Quota:      codexauth.WHAMQuotaResult{Plan: plan, Quota: &codexauth.QuotaReading{WeeklyPercent: &usage}},
		}, nil
	})

	runtime, err := buildCodexPoolRuntime(context.Background(), disk, []providerregistry.Spec{poolSpecForTest()}, CodexPoolOptions{
		BenesHome:           benesHome,
		CodexHome:           codexHome,
		Now:                 func() time.Time { return now },
		ManagedQuotaFetcher: fetcher,
	})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.primer == nil || runtime.authorities["openai"] == nil {
		t.Fatalf("runtime=%#v", runtime)
	}
	runtime.primer.Prime(context.Background())
	credential, err := runtime.authorities["openai"].Resolve(context.Background(), providers.DispatchRequest{
		Parsed: protocolRequestForPoolTest("gpt-5.6"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if credential.Authorization != "Bearer access-b" || credential.ChatGPTAccountID != "chat-b" {
		t.Fatalf("credential=%#v", credential)
	}
}

func TestBuildCodexPoolAuthoritiesLeavesManagedPrimeInactive(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	benesHome := t.TempDir()
	codexHome := t.TempDir()
	writePrivateJSON(t, filepath.Join(benesHome, "codex-accounts.json"), map[string]any{
		"a": map[string]any{"generation": 1, "credential": map[string]any{"accessToken": "access-a", "refreshToken": "refresh-a", "expiresAt": now.Add(time.Hour).UnixMilli(), "chatgptAccountId": "chat-a"}},
	})
	disk := poolDiskForTest(t, `{"codexAccounts":[{"id":"a","plan":"plus"}]}`)
	started := make(chan struct{}, 1)
	fetcher := bootstrapManagedQuotaFetcherFunc(func(ctx context.Context, _ codexauth.ManagedToken, _ string) (codexauth.WHAMFetchResult, error) {
		started <- struct{}{}
		<-ctx.Done()
		return codexauth.WHAMFetchResult{}, ctx.Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	authorities, err := buildCodexPoolAuthorities(ctx, disk, []providerregistry.Spec{poolSpecForTest()}, CodexPoolOptions{
		BenesHome:           benesHome,
		CodexHome:           codexHome,
		Now:                 func() time.Time { return now },
		ManagedQuotaFetcher: fetcher,
	})
	if err != nil {
		t.Fatal(err)
	}
	if authorities["openai"] == nil {
		t.Fatal("openai Pool authority missing")
	}
	select {
	case <-started:
		t.Fatal("authority construction started managed quota I/O")
	case <-time.After(50 * time.Millisecond):
	}
}
