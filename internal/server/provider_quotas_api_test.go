package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/quota"
)

func TestProviderQuotasAPIIsLoopbackAndOmitsSecrets(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"providers":{"openrouter":{"adapter":"openai","baseUrl":"https://openrouter.ai/api/v1","apiKey":"sk-or-secret","authMode":"key"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	quota.HTTPClient = quotaDo(func(*http.Request) (*http.Response, error) {
		raw, _ := json.Marshal(map[string]any{"data": map[string]any{"limit": 4.0, "limit_remaining": 1.0}})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(raw))), Header: make(http.Header)}, nil
	})
	t.Cleanup(func() { quota.HTTPClient = http.DefaultClient })
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openrouter": providerFunc(nil)},
		ConfigPath:     configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	blocked := httptest.NewRequest(http.MethodGet, "/api/provider-quotas", nil)
	blocked.Header.Set("Authorization", "Bearer local-secret")
	blockedRR := httptest.NewRecorder()
	h.ServeHTTP(blockedRR, blocked)
	if blockedRR.Code != http.StatusNotFound {
		t.Fatalf("data-plane status=%d body=%s", blockedRR.Code, blockedRR.Body.String())
	}
	req := httptest.NewRequest(http.MethodGet, "/api/provider-quotas?refresh=1", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "sk-or-secret") {
		t.Fatalf("leaked key: %s", rr.Body.String())
	}
	var payload quota.Response
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Reports) != 1 || payload.Reports[0].Provider != "openrouter" {
		t.Fatalf("%+v", payload)
	}
}

func TestProviderQuotasAPIUsesAuthStoreTokenWithoutLeaking(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	authPath := filepath.Join(home, "auth.json")
	if err := os.WriteFile(configPath, []byte(`{"providers":{"command-code":{"adapter":"openai","baseUrl":"https://api.commandcode.ai","authMode":"oauth"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(authPath, []byte(`{"command-code":{"accounts":[{"id":"acc-1","credential":{"access":"tok-secret","refresh":"rt"}}],"activeAccountId":"acc-1"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	quota.HTTPClient = quotaDo(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("Authorization") != "Bearer tok-secret" {
			t.Fatalf("auth %q", req.Header.Get("Authorization"))
		}
		if strings.Contains(req.URL.Path, "/alpha/billing/credits") {
			raw, _ := json.Marshal(map[string]any{"data": map[string]any{"windowLimits": map[string]any{"weekly": map[string]any{"cap": 10.0, "used": 2.0}}}})
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(raw))), Header: make(http.Header)}, nil
		}
		raw, _ := json.Marshal(map[string]any{"data": map[string]any{}})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(raw))), Header: make(http.Header)}, nil
	})
	t.Cleanup(func() { quota.HTTPClient = http.DefaultClient })
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"command-code": providerFunc(nil)},
		ConfigPath:     configPath,
		AuthStorePath:  authPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodGet, "/api/provider-quotas?refresh=1", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "tok-secret") {
		t.Fatalf("leaked token: %s", rr.Body.String())
	}
	var payload quota.Response
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Reports) != 1 || payload.Reports[0].Source != "command-code:credits" {
		t.Fatalf("%+v", payload)
	}
}

func TestProviderQuotasAPIUsesAuthStoreWhenConfigAuthModeIsKey(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	authPath := filepath.Join(home, "auth.json")
	if err := os.WriteFile(configPath, []byte(`{"providers":{"command-code":{"adapter":"command-code","baseUrl":"https://api.commandcode.ai","authMode":"key"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(authPath, []byte(`{"command-code":{"accounts":[{"id":"acc-1","credential":{"access":"tok-secret","refresh":"rt"}}],"activeAccountId":"acc-1"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	quota.HTTPClient = quotaDo(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("Authorization") != "Bearer tok-secret" {
			t.Fatalf("auth %q", req.Header.Get("Authorization"))
		}
		if strings.Contains(req.URL.Path, "/alpha/billing/credits") {
			raw, _ := json.Marshal(map[string]any{"data": map[string]any{"windowLimits": map[string]any{"weekly": map[string]any{"cap": 10.0, "used": 2.0}}}})
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(raw))), Header: make(http.Header)}, nil
		}
		raw, _ := json.Marshal(map[string]any{"data": map[string]any{}})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(raw))), Header: make(http.Header)}, nil
	})
	t.Cleanup(func() { quota.HTTPClient = http.DefaultClient })
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"command-code": providerFunc(nil)},
		ConfigPath:     configPath,
		AuthStorePath:  authPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodGet, "/api/provider-quotas?refresh=1", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "tok-secret") {
		t.Fatalf("leaked token: %s", rr.Body.String())
	}
	var payload quota.Response
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Reports) != 1 || payload.Reports[0].Source != "command-code:credits" {
		t.Fatalf("%+v", payload)
	}
}

func TestProviderQuotasAPIUsesKiroStoredRegionAndDropsEmail(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	authPath := filepath.Join(home, "auth.json")
	if err := os.WriteFile(configPath, []byte(`{"providers":{"kiro":{"adapter":"kiro","baseUrl":"https://runtime.us-east-1.kiro.dev","authMode":"oauth"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(authPath, []byte(`{"kiro":{"activeAccountId":"arn:aws:codewhisperer:eu-west-1:123456789012:profile/b","accounts":[{"id":"arn:aws:codewhisperer:eu-west-1:123456789012:profile/b","credential":{"access":"tok-secret","refresh":"rt-b","profileArn":"arn:aws:codewhisperer:eu-west-1:123456789012:profile/b","apiRegion":"eu-west-1","authType":"kiro_desktop"}}]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var gotHost string
	quota.HTTPClient = quotaDo(func(req *http.Request) (*http.Response, error) {
		gotHost = req.URL.Host
		raw, _ := json.Marshal(map[string]any{
			"usageBreakdownList": []any{map[string]any{"resourceType": "AGENTIC_REQUEST", "currentUsageWithPrecision": 10.0, "usageLimitWithPrecision": 50.0}},
			"userInfo":           map[string]any{"email": "hidden@example.com"},
		})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(raw))), Header: make(http.Header)}, nil
	})
	t.Cleanup(func() { quota.HTTPClient = http.DefaultClient })
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"kiro": providerFunc(nil)},
		ConfigPath:     configPath,
		AuthStorePath:  authPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodGet, "/api/provider-quotas?refresh=1", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if gotHost != "management.eu-west-1.kiro.dev" {
		t.Fatalf("host=%q", gotHost)
	}
	if strings.Contains(rr.Body.String(), "hidden@example.com") || strings.Contains(rr.Body.String(), "tok-secret") {
		t.Fatalf("leaked: %s", rr.Body.String())
	}
	var payload quota.Response
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Reports) != 1 || payload.Reports[0].Source != "kiro:get-usage-limits" || payload.Reports[0].Quota.MonthlyPercent == nil || *payload.Reports[0].Quota.MonthlyPercent != 20 {
		t.Fatalf("%+v", payload)
	}
}

func TestProviderQuotasAPIProjectsEntitlementWithoutSecrets(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	authPath := filepath.Join(home, "auth.json")
	if err := os.WriteFile(configPath, []byte(`{"providers":{"command-code":{"adapter":"openai","baseUrl":"https://api.commandcode.ai","authMode":"oauth"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(authPath, []byte(`{"command-code":{"accounts":[{"id":"acc-1","credential":{"access":"tok-secret","refresh":"rt"}}],"activeAccountId":"acc-1"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	quota.HTTPClient = quotaDo(func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Path, "/alpha/billing/credits") {
			raw, _ := json.Marshal(map[string]any{"data": map[string]any{"windowLimits": map[string]any{"weekly": map[string]any{"cap": 10.0, "used": 2.0, "resetTime": 1.7000001e12}}}})
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(raw))), Header: make(http.Header)}, nil
		}
		if strings.Contains(req.URL.Path, "/alpha/billing/subscriptions") {
			raw, _ := json.Marshal(map[string]any{"data": map[string]any{"currentPeriodEnd": "2023-12-01T00:00:00Z"}})
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(raw))), Header: make(http.Header)}, nil
		}
		raw, _ := json.Marshal(map[string]any{"data": map[string]any{}})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(raw))), Header: make(http.Header)}, nil
	})
	t.Cleanup(func() { quota.HTTPClient = http.DefaultClient })
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"command-code": providerFunc(nil)},
		ConfigPath:     configPath,
		AuthStorePath:  authPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodGet, "/api/provider-quotas?refresh=1", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if strings.Contains(body, "tok-secret") || strings.Contains(body, "currentPeriodEnd") {
		t.Fatalf("leaked secret or raw payload: %s", body)
	}
	var payload quota.Response
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Reports) != 1 || payload.Reports[0].Entitlement == nil || payload.Reports[0].Entitlement.BillingPeriodEndsAt == 0 {
		t.Fatalf("%+v", payload)
	}
	if payload.Reports[0].Entitlement.CredentialExpiresAt != 0 {
		t.Fatalf("credential must stay omitted %+v", payload.Reports[0].Entitlement)
	}
}

type quotaDo func(*http.Request) (*http.Response, error)

func (f quotaDo) Do(req *http.Request) (*http.Response, error) { return f(req) }

func TestProviderQuotasMergesCodexSnapshotAndDefaultsAuthStore(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"providers":{"openai":{"adapter":"openai-responses","authMode":"forward"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	weekly := 44.0
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai": providerFunc(nil)},
		ConfigPath:     configPath,
		CodexQuota: func() []quota.Report {
			return []quota.Report{{Provider: "openai", Label: "openai", Source: "chatgpt:wham", Quota: quota.Quota{WeeklyPercent: &weekly, UpdatedAt: 9}, UpdatedAt: 9}}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodGet, "/api/provider-quotas", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var payload quota.Response
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Reports) != 1 || payload.Reports[0].Source != "chatgpt:wham" || payload.Reports[0].Quota.WeeklyPercent == nil || *payload.Reports[0].Quota.WeeklyPercent != 44 {
		t.Fatalf("%+v", payload)
	}
}
