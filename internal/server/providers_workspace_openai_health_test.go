package server

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/codexauth"
	"github.com/Wibias/Benes/internal/provideractivity"
)

func TestProvidersWorkspaceOpenAIPoolAllHardCooldownNeedsAttention(t *testing.T) {
	h, health, now := newOpenAIWorkspaceHealthFixture(t)
	health.SetHardCooldownAt("pool-1", now, now.Add(10*time.Minute), codexauth.CooldownSourceRetryAfter)

	lifecycle, codes := readOpenAIWorkspaceLifecycle(t, h)
	if lifecycle != "attention" || !containsWorkspaceIssue(codes, "quota_exhausted") {
		t.Fatalf("fully rate-limited OAuth pool must need attention: lifecycle=%q issues=%v", lifecycle, codes)
	}

	health.ClearHardCooldown("pool-1")
	lifecycle, codes = readOpenAIWorkspaceLifecycle(t, h)
	if lifecycle != "healthy" || len(codes) != 0 {
		t.Fatalf("clearing the only pool cooldown must restore healthy lifecycle: lifecycle=%q issues=%v", lifecycle, codes)
	}
}

func TestProvidersWorkspaceOpenAIStaleRateLimitEventClearsWhenPoolIsUsable(t *testing.T) {
	h, _, now := newOpenAIWorkspaceHealthFixture(t)
	inner := h.(*handler)
	inner.activity.Record(provideractivity.Event{
		Provider:  "openai",
		Type:      "rate_limited",
		Severity:  "warn",
		Timestamp: now.UnixMilli(),
	})

	lifecycle, codes := readOpenAIWorkspaceLifecycle(t, h)
	if lifecycle != "healthy" || len(codes) != 0 {
		t.Fatalf("stale rate_limited activity must not keep Needs attention after the pool is usable: lifecycle=%q issues=%v", lifecycle, codes)
	}
}

func TestProvidersWorkspaceOpenAILiveCooldownKeepsRateLimitAttention(t *testing.T) {
	h, health, now := newOpenAIWorkspaceHealthFixture(t)
	inner := h.(*handler)
	inner.activity.Record(provideractivity.Event{
		Provider:  "openai",
		Type:      "rate_limited",
		Severity:  "warn",
		Timestamp: now.UnixMilli(),
	})
	health.SetHardCooldownAt("pool-1", now, now.Add(10*time.Minute), codexauth.CooldownSourceRetryAfter)

	lifecycle, codes := readOpenAIWorkspaceLifecycle(t, h)
	if lifecycle != "attention" || !containsWorkspaceIssue(codes, "quota_exhausted") {
		t.Fatalf("a still-blocked pool must keep rate-limit attention: lifecycle=%q issues=%v", lifecycle, codes)
	}

	health.ClearHardCooldown("pool-1")
	lifecycle, codes = readOpenAIWorkspaceLifecycle(t, h)
	if lifecycle != "healthy" || len(codes) != 0 {
		t.Fatalf("expiring the live cooldown must drop Needs attention even if the rate_limited event remains: lifecycle=%q issues=%v", lifecycle, codes)
	}
}

func TestProvidersWorkspaceOpenAIPoolAllSoftAvoidNeedsHealthAttention(t *testing.T) {
	h, health, now := newOpenAIWorkspaceHealthFixture(t)
	health.SetSoftAvoid("pool-1", now.Add(30*time.Second))

	lifecycle, codes := readOpenAIWorkspaceLifecycle(t, h)
	if lifecycle != "attention" || !containsWorkspaceIssue(codes, "provider_health_failure") {
		t.Fatalf("OAuth pool with no transiently usable credential must need health attention: lifecycle=%q issues=%v", lifecycle, codes)
	}

	health.ClearSoftAvoid("pool-1")
	lifecycle, codes = readOpenAIWorkspaceLifecycle(t, h)
	if lifecycle != "healthy" || len(codes) != 0 {
		t.Fatalf("clearing the only soft avoid must restore healthy lifecycle: lifecycle=%q issues=%v", lifecycle, codes)
	}
}

func TestProvidersWorkspaceOpenAIPoolBlockedMemberDoesNotPoisonHealthyAlternate(t *testing.T) {
	h, health, now := newOpenAIWorkspaceMultiHealthFixture(t)
	health.SetHardCooldownAt("pool-1", now, now.Add(10*time.Minute), codexauth.CooldownSourceRetryAfter)

	lifecycle, codes := readOpenAIWorkspaceLifecycle(t, h)
	if lifecycle != "healthy" || len(codes) != 0 {
		t.Fatalf("healthy pool alternate must absorb one blocked credential: lifecycle=%q issues=%v", lifecycle, codes)
	}
}

func TestProvidersWorkspaceOpenAIPoolScopedCooldownDoesNotPoisonWholeProvider(t *testing.T) {
	h, health, now := newOpenAIWorkspaceHealthFixture(t)
	health.SetScopedHardCooldownAt("pool-1", codexauth.QuotaScopeSpark, now, now.Add(10*time.Minute), codexauth.CooldownSourceResetDerived)

	lifecycle, codes := readOpenAIWorkspaceLifecycle(t, h)
	if lifecycle != "healthy" || len(codes) != 0 {
		t.Fatalf("model-scoped cooldown must not mark the whole provider unavailable: lifecycle=%q issues=%v", lifecycle, codes)
	}
}

func newOpenAIWorkspaceHealthFixture(t *testing.T) (http.Handler, *codexauth.HealthState, time.Time) {
	t.Helper()
	return newOpenAIWorkspaceHealthFixtureWithAccounts(t, []string{"pool-1"})
}

func newOpenAIWorkspaceMultiHealthFixture(t *testing.T) (http.Handler, *codexauth.HealthState, time.Time) {
	t.Helper()
	return newOpenAIWorkspaceHealthFixtureWithAccounts(t, []string{"pool-1", "pool-2"})
}

func newOpenAIWorkspaceHealthFixtureWithAccounts(t *testing.T, accountIDs []string) (http.Handler, *codexauth.HealthState, time.Time) {
	t.Helper()
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	accountRows := make([]map[string]string, 0, len(accountIDs))
	for _, id := range accountIDs {
		accountRows = append(accountRows, map[string]string{
			"id": id, "email": id + "@example.com", "plan": "plus",
		})
	}
	accountsJSON, err := json.Marshal(accountRows)
	if err != nil {
		t.Fatal(err)
	}
	configJSON := `{
		"providers": {
			"openai": {
				"adapter": "openai-responses",
				"baseUrl": "https://chatgpt.com/backend-api/codex",
				"authMode": "forward",
				"codexAccountMode": "pool",
				"defaultAccess": "oauth"
			}
		},
		"codexAccounts": ` + string(accountsJSON) + `
	}`
	if err := os.WriteFile(configPath, []byte(configJSON), 0o600); err != nil {
		t.Fatal(err)
	}

	store, err := codexauth.NewManagedCredentialStore(home)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	validatedAt := now.UnixMilli()
	for _, id := range accountIDs {
		if err := store.Put(context.Background(), id, codexauth.ManagedCredential{
			AccessToken: id + "-at", RefreshToken: id + "-rt", ExpiresAtMS: validatedAt + 3600000, ChatGPTAccountID: id + "-chat",
		}, &validatedAt); err != nil {
			t.Fatal(err)
		}
	}

	health := codexauth.NewHealthState()
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai": providerFunc(nil)},
		ConfigPath:     configPath,
		CodexHealth:    health,
		CodexAccounts: &CodexAccountRuntime{
			Store: store, Reauth: codexauth.NewReauthState(), Now: func() time.Time { return now },
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	return h, health, now
}
