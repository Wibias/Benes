package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/catalog"
	"github.com/Wibias/Benes/internal/codexauth"
	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/harnessboard"
	"github.com/Wibias/Benes/internal/provideractivity"
	"github.com/Wibias/Benes/internal/quota"
)

func TestClassifyProviderWorkspaceUsesEvidenceAndOneLifecycle(t *testing.T) {
	tests := []struct {
		name      string
		provider  config.LogicalProvider
		evidence  providerWorkspaceEvidence
		wantState string
		wantCode  string
	}{
		{name: "healthy", provider: config.LogicalProvider{ID: "healthy"}, wantState: "healthy"},
		{name: "disabled", provider: config.LogicalProvider{ID: "disabled", Disabled: true}, evidence: providerWorkspaceEvidence{QuotaExhausted: true}, wantState: "disabled"},
		{name: "missing credential", provider: config.LogicalProvider{ID: "missing", NeedsCredential: true}, wantState: "attention", wantCode: "credential_missing"},
		{name: "reauth required", provider: config.LogicalProvider{ID: "reauth"}, evidence: providerWorkspaceEvidence{ReauthRequired: true, ReauthAt: 101}, wantState: "attention", wantCode: "credential_reauth_required"},
		{name: "quota exhausted", provider: config.LogicalProvider{ID: "quota"}, evidence: providerWorkspaceEvidence{QuotaExhausted: true, QuotaAt: 102}, wantState: "attention", wantCode: "quota_exhausted"},
		{name: "health failure", provider: config.LogicalProvider{ID: "health"}, evidence: providerWorkspaceEvidence{HealthFailed: true, HealthAt: 103}, wantState: "attention", wantCode: "provider_health_failure"},
		{name: "stale catalogue", provider: config.LogicalProvider{ID: "catalog"}, evidence: providerWorkspaceEvidence{CatalogueStale: true, CatalogueAt: 104}, wantState: "attention", wantCode: "catalogue_stale"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyProviderWorkspace(tt.provider, tt.evidence)
			if got.Lifecycle != tt.wantState {
				t.Fatalf("lifecycle=%q want=%q", got.Lifecycle, tt.wantState)
			}
			if tt.wantState == "disabled" && len(got.Issues) != 0 {
				t.Fatalf("disabled provider leaked attention issues: %#v", got.Issues)
			}
			if tt.wantCode == "" {
				if len(got.Issues) != 0 {
					t.Fatalf("unexpected issues=%#v", got.Issues)
				}
				return
			}
			found := false
			for _, issue := range got.Issues {
				if issue.Code == tt.wantCode {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("issues=%#v missing code %q", got.Issues, tt.wantCode)
			}
		})
	}
}

func TestLogicalModelCountsUsesCallabilityNotVisibility(t *testing.T) {
	logical := []config.LogicalProvider{{ID: "provider", ConnectionIDs: []string{"provider"}}}
	models := []catalog.Model{
		{ID: "provider/ready", Availability: catalog.Availability{Selectable: true}},
		{ID: "provider/exhausted", Availability: catalog.Availability{Selectable: false, Reason: "no_credit"}},
		{ID: "provider/hidden", Availability: catalog.Availability{Selectable: true}},
		{ID: "provider/discovered", Discovered: true},
	}

	counts, available, unavailable := logicalModelCounts(logical, nil, models)
	if counts["provider"] != 4 {
		t.Fatalf("model count=%d want=4", counts["provider"])
	}
	if available != 3 || unavailable != 1 {
		t.Fatalf("available=%d unavailable=%d want=3/1", available, unavailable)
	}
}

func TestProvidersWorkspaceOpenAIPoolRequiresWholePoolExhaustion(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{
		"providers": {
			"openai": {
				"adapter": "openai-responses",
				"baseUrl": "https://chatgpt.com/backend-api/codex",
				"authMode": "forward",
				"codexAccountMode": "pool"
			}
		},
		"codexAccounts": [
			{"id":"pool-1","email":"pool@example.com","plan":"plus"}
		]
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	store, err := codexauth.NewManagedCredentialStore(home)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	validatedAt := now.UnixMilli()
	if err := store.Put(context.Background(), "pool-1", codexauth.ManagedCredential{
		AccessToken: "pool-at", RefreshToken: "pool-rt", ExpiresAtMS: validatedAt + 3600000, ChatGPTAccountID: "pool-chat",
	}, &validatedAt); err != nil {
		t.Fatal(err)
	}
	managedQuotas := codexauth.NewQuotaState()
	healthy := 20.0
	managedQuotas.SetParsedForCredential("pool-1", 1, codexauth.QuotaReading{WeeklyPercent: &healthy}, 1, now)

	codexHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(codexHome, "auth.json"), []byte(`{"tokens":{"access_token":"main-at","account_id":"main-chat"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	main, err := codexauth.NewMainCredentialSource(codexHome)
	if err != nil {
		t.Fatal(err)
	}
	mainResult := main.Read(now)
	if mainResult.Status != codexauth.MainCredentialOK {
		t.Fatalf("main credential status=%s", mainResult.Status)
	}
	exhausted := 100.0
	mainQuotas := codexauth.NewMainQuotaState()
	if !mainQuotas.Publish(mainResult.Identity, "plus", &codexauth.QuotaReading{WeeklyPercent: &exhausted}, now) {
		t.Fatal("main quota publish failed")
	}

	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai": providerFunc(nil)},
		ConfigPath:     configPath,
		CodexAccounts: &CodexAccountRuntime{
			Store: store, Quotas: managedQuotas, Main: main, MainQuotas: mainQuotas,
			Now: func() time.Time { return now },
		},
		CodexQuota: func() []quota.Report {
			return []quota.Report{{
				Provider:  "openai",
				Quota:     quota.Quota{WeeklyPercent: &exhausted, UpdatedAt: now.UnixMilli()},
				UpdatedAt: now.UnixMilli(),
			}}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)

	read := func() (string, []string) {
		req := httptest.NewRequest(http.MethodGet, "/api/providers/workspace", nil)
		req.Host = "127.0.0.1"
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
		}
		var body struct {
			Providers []struct {
				ID        string `json:"id"`
				Lifecycle string `json:"lifecycle"`
			} `json:"providers"`
			Attention []struct {
				Provider string `json:"provider"`
				Code     string `json:"code"`
			} `json:"attention"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		lifecycle := ""
		for _, provider := range body.Providers {
			if provider.ID == "openai" {
				lifecycle = provider.Lifecycle
				break
			}
		}
		codes := make([]string, 0, len(body.Attention))
		for _, issue := range body.Attention {
			if issue.Provider == "openai" {
				codes = append(codes, issue.Code)
			}
		}
		return lifecycle, codes
	}

	lifecycle, codes := read()
	if lifecycle != "healthy" || len(codes) != 0 {
		t.Fatalf("one healthy pool credential must keep OpenAI healthy: lifecycle=%q issues=%v", lifecycle, codes)
	}

	later := now.Add(time.Second)
	managedQuotas.SetParsedForCredential("pool-1", 1, codexauth.QuotaReading{WeeklyPercent: &exhausted}, 1, later)
	lifecycle, codes = read()
	if lifecycle != "attention" || !containsWorkspaceIssue(codes, "quota_exhausted") {
		t.Fatalf("fully exhausted pool must need attention: lifecycle=%q issues=%v", lifecycle, codes)
	}
}

func containsWorkspaceIssue(codes []string, want string) bool {
	for _, code := range codes {
		if code == want {
			return true
		}
	}
	return false
}

func TestProvidersWorkspaceReportsUnknownAvailabilityAndRealTimestamps(t *testing.T) {
	harnessHome := t.TempDir()
	harnessboard.LookPath = func(string) (string, error) { return "", errors.New("not found") }
	harnessboard.ListProcesses = func() ([]harnessboard.Process, error) { return nil, nil }
	harnessboard.UserHome = func() (string, error) { return harnessHome, nil }
	t.Cleanup(harnessboard.ResetHooks)
	for _, key := range []string{"APPDATA", "LOCALAPPDATA", "HOME", "USERPROFILE", "XDG_CONFIG_HOME", "XDG_DATA_HOME"} {
		t.Setenv(key, harnessHome)
	}

	h, _ := newProvidersMutateHandler(t, `{
		"providers": {
			"local": {"adapter":"local","baseUrl":"http://127.0.0.1:11434","authMode":"local"}
		}
	}`, "local")
	inner := h.(*handler)

	read := func() struct {
		Summary struct {
			TotalProviders int `json:"totalProviders"`
			Healthy        int `json:"healthy"`
			Attention      int `json:"attention"`
			Disabled       int `json:"disabled"`
		} `json:"summary"`
		Providers []struct {
			ID            string `json:"id"`
			LastValidated *int64 `json:"lastValidated"`
		} `json:"providers"`
		Availability struct {
			StaleProviderCatalogues *int   `json:"staleProviderCatalogues"`
			LastModelSync           *int64 `json:"lastModelSync"`
		} `json:"availability"`
		Downstream struct {
			HarnessCount       *int `json:"harnessCount"`
			SubAgentModelCount int  `json:"subAgentModelCount"`
		} `json:"downstream"`
	} {
		req := httptest.NewRequest(http.MethodGet, "/api/providers/workspace", nil)
		req.Host = "127.0.0.1"
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
		}
		if strings.Contains(rr.Body.String(), "subAgentProfileCount") {
			t.Fatalf("workspace still claims sub-agent profiles: %s", rr.Body.String())
		}
		var body struct {
			Summary struct {
				TotalProviders int `json:"totalProviders"`
				Healthy        int `json:"healthy"`
				Attention      int `json:"attention"`
				Disabled       int `json:"disabled"`
			} `json:"summary"`
			Providers []struct {
				ID            string `json:"id"`
				LastValidated *int64 `json:"lastValidated"`
			} `json:"providers"`
			Availability struct {
				StaleProviderCatalogues *int   `json:"staleProviderCatalogues"`
				LastModelSync           *int64 `json:"lastModelSync"`
			} `json:"availability"`
			Downstream struct {
				HarnessCount       *int `json:"harnessCount"`
				SubAgentModelCount int  `json:"subAgentModelCount"`
			} `json:"downstream"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return body
	}

	initial := read()
	if initial.Summary.TotalProviders != initial.Summary.Healthy+initial.Summary.Attention+initial.Summary.Disabled {
		t.Fatalf("summary invariant violated: %#v", initial.Summary)
	}
	if initial.Availability.StaleProviderCatalogues != nil || initial.Availability.LastModelSync != nil {
		t.Fatalf("unknown availability was fabricated: %#v", initial.Availability)
	}
	if len(initial.Providers) != 1 || initial.Providers[0].LastValidated != nil {
		t.Fatalf("validation timestamp was fabricated: %#v", initial.Providers)
	}
	if initial.Downstream.HarnessCount != nil {
		t.Fatalf("unknown harness count was fabricated: %v", *initial.Downstream.HarnessCount)
	}

	// A quota observation is not a credential/connection validation.
	inner.activity.Record(provideractivity.Event{Provider: "local", Type: "quota_observed", Timestamp: 1000, Severity: "info"})
	if got := read().Providers[0].LastValidated; got != nil {
		t.Fatalf("quota observation became validation timestamp: %v", *got)
	}

	inner.activity.Record(provideractivity.Event{Provider: "local", Type: "credentials_validated", Timestamp: 2000, Severity: "info"})
	inner.activity.Record(provideractivity.Event{Provider: "local", Type: "model_catalogue_synchronized", Timestamp: 3000, Severity: "info"})
	after := read()
	if after.Providers[0].LastValidated == nil || *after.Providers[0].LastValidated != 2000 {
		t.Fatalf("lastValidated=%v", after.Providers[0].LastValidated)
	}
	if after.Availability.LastModelSync == nil || *after.Availability.LastModelSync != 3000 {
		t.Fatalf("lastModelSync=%v", after.Availability.LastModelSync)
	}
}

func TestProvidersWorkspaceStaleEventCreatesAttentionAndKnownStaleCount(t *testing.T) {
	h, _ := newProvidersMutateHandler(t, `{
		"providers": {
			"local": {"adapter":"local","baseUrl":"http://127.0.0.1:11434","authMode":"local"}
		}
	}`, "local")
	inner := h.(*handler)
	inner.activity.Record(provideractivity.Event{Provider: "local", Type: "model_catalogue_stale", Detail: "catalogue refresh failed", Timestamp: 4000, Severity: "warn"})

	req := httptest.NewRequest(http.MethodGet, "/api/providers/workspace", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var body struct {
		Summary struct {
			Attention int `json:"attention"`
		} `json:"summary"`
		Providers []struct {
			Lifecycle string `json:"lifecycle"`
		} `json:"providers"`
		Attention []struct {
			Provider  string `json:"provider"`
			Code      string `json:"code"`
			Timestamp int64  `json:"timestamp"`
		} `json:"attention"`
		Availability struct {
			StaleProviderCatalogues *int `json:"staleProviderCatalogues"`
		} `json:"availability"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Summary.Attention != 1 || len(body.Providers) != 1 || body.Providers[0].Lifecycle != "attention" {
		t.Fatalf("lifecycle=%#v summary=%#v", body.Providers, body.Summary)
	}
	if len(body.Attention) != 1 || body.Attention[0].Code != "catalogue_stale" || body.Attention[0].Timestamp != 4000 {
		t.Fatalf("attention=%#v", body.Attention)
	}
	if body.Availability.StaleProviderCatalogues == nil || *body.Availability.StaleProviderCatalogues != 1 {
		t.Fatalf("staleProviderCatalogues=%v", body.Availability.StaleProviderCatalogues)
	}
}
