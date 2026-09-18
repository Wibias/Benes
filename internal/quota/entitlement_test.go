package quota

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestCommandCodeKeepsBillingPeriodOffCreditsAndCredential(t *testing.T) {
	Now = func() time.Time { return time.UnixMilli(1_700_000_000_000) }
	t.Cleanup(func() { Now = time.Now })
	HTTPClient = roundTrip(func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.Contains(req.URL.Path, "/alpha/whoami"):
			return jsonResp(200, map[string]any{"data": map[string]any{"org": map[string]any{"id": "org-1"}}}), nil
		case strings.Contains(req.URL.Path, "/alpha/billing/credits"):
			if req.Header.Get("Authorization") != "Bearer tok-a" {
				t.Fatalf("auth %q", req.Header.Get("Authorization"))
			}
			return jsonResp(200, map[string]any{"data": map[string]any{
				"credits":      map[string]any{"monthlyCredits": 80.0, "purchasedCredits": 0.0, "freeCredits": 0.0},
				"windowLimits": map[string]any{"fiveHour": map[string]any{"cap": 100.0, "used": 10.0, "resetTime": 1.700000360e12}},
			}}), nil
		case strings.Contains(req.URL.Path, "/alpha/billing/subscriptions"):
			return jsonResp(200, map[string]any{"data": map[string]any{
				"currentPeriodStart": "2023-11-01T00:00:00Z",
				"currentPeriodEnd":   "2023-12-01T00:00:00Z",
			}}), nil
		case strings.Contains(req.URL.Path, "/alpha/usage/summary"):
			return jsonResp(200, map[string]any{"data": map[string]any{"totalCost": 20.0}}), nil
		default:
			t.Fatalf("unexpected %s", req.URL)
			return nil, nil
		}
	})
	t.Cleanup(func() { HTTPClient = http.DefaultClient })
	out := FetchReports(nil, []Provider{{
		Name: "command-code", AuthMode: "oauth", AccessToken: "tok-a", AccountID: "acct-a", BaseURL: CommandCodeBase,
	}}, true)
	if len(out.Reports) != 1 {
		t.Fatalf("%+v", out)
	}
	got := out.Reports[0]
	if got.AccountID != "acct-a" {
		t.Fatalf("account %q", got.AccountID)
	}
	if got.Quota.FiveHourResetAt != 1_700_000_360_000 {
		t.Fatalf("quota reset %+v", got.Quota)
	}
	if got.Quota.CreditsUsd == nil || got.Quota.CreditsUsd.ExpiresAt != 0 {
		t.Fatalf("credits must not inherit billing end: %+v", got.Quota.CreditsUsd)
	}
	if got.Entitlement == nil || got.Entitlement.BillingPeriodEndsAt != time.Date(2023, 12, 1, 0, 0, 0, 0, time.UTC).UnixMilli() {
		t.Fatalf("billing %+v", got.Entitlement)
	}
	if got.Entitlement.CredentialExpiresAt != 0 || got.Entitlement.QuotaResetAt != 0 {
		t.Fatalf("must not copy credential/quota into plan fields: %+v", got.Entitlement)
	}
	if got.Entitlement.Confidence != entitlementAuthoritative {
		t.Fatalf("confidence %q", got.Entitlement.Confidence)
	}
}

func TestEntitlementOmitsMissingAndMalformedTimestamps(t *testing.T) {
	Now = func() time.Time { return time.UnixMilli(1_700_000_000_000) }
	t.Cleanup(func() { Now = time.Now })
	HTTPClient = roundTrip(func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Path, "/alpha/billing/credits") {
			return jsonResp(200, map[string]any{"data": map[string]any{
				"windowLimits": map[string]any{"weekly": map[string]any{"cap": 10.0, "used": 1.0}},
			}}), nil
		}
		if strings.Contains(req.URL.Path, "/alpha/billing/subscriptions") {
			return jsonResp(200, map[string]any{"data": map[string]any{
				"currentPeriodEnd": "not-a-date",
			}}), nil
		}
		return jsonResp(404, map[string]any{"error": "no"}), nil
	})
	t.Cleanup(func() { HTTPClient = http.DefaultClient })
	out := FetchReports(nil, []Provider{{Name: "command-code", AuthMode: "oauth", AccessToken: "tok", BaseURL: CommandCodeBase}}, true)
	if len(out.Reports) != 1 || out.Reports[0].Entitlement != nil {
		t.Fatalf("missing/malformed must omit entitlement, got %+v", out)
	}
}

func TestEntitlementIsolatesAccountsAndInvalidatesOnReauth(t *testing.T) {
	Now = func() time.Time { return time.UnixMilli(1_700_000_000_000) }
	t.Cleanup(func() { Now = time.Now })
	HTTPClient = roundTrip(func(req *http.Request) (*http.Response, error) {
		token := strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer ")
		end := "2023-12-01T00:00:00Z"
		if token == "tok-b" {
			end = "2024-01-01T00:00:00Z"
		}
		if strings.Contains(req.URL.Path, "/alpha/billing/credits") {
			return jsonResp(200, map[string]any{"data": map[string]any{
				"windowLimits": map[string]any{"weekly": map[string]any{"cap": 10.0, "used": 2.0, "resetTime": 1.700000100e12}},
			}}), nil
		}
		if strings.Contains(req.URL.Path, "/alpha/billing/subscriptions") {
			return jsonResp(200, map[string]any{"data": map[string]any{"currentPeriodEnd": end}}), nil
		}
		return jsonResp(404, map[string]any{}), nil
	})
	t.Cleanup(func() { HTTPClient = http.DefaultClient })
	store := &Store{}
	first := FetchReports(store, []Provider{
		{Name: "command-code", AuthMode: "oauth", AccessToken: "tok-a", AccountID: "acct-a", BaseURL: CommandCodeBase},
		{Name: "command-code", AuthMode: "oauth", AccessToken: "tok-b", AccountID: "acct-b", BaseURL: CommandCodeBase},
	}, true)
	if len(first.Reports) != 2 {
		t.Fatalf("want 2 accounts, got %+v", first)
	}
	byAccount := map[string]Report{}
	for _, item := range first.Reports {
		byAccount[item.AccountID] = item
	}
	wantA := time.Date(2023, 12, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	wantB := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	if byAccount["acct-a"].Entitlement == nil || byAccount["acct-a"].Entitlement.BillingPeriodEndsAt != wantA {
		t.Fatalf("a %+v", byAccount["acct-a"].Entitlement)
	}
	if byAccount["acct-b"].Entitlement == nil || byAccount["acct-b"].Entitlement.BillingPeriodEndsAt != wantB {
		t.Fatalf("b %+v", byAccount["acct-b"].Entitlement)
	}
	replaced := FetchReports(store, []Provider{
		{Name: "command-code", AuthMode: "oauth", AccessToken: "tok-b", AccountID: "acct-a", BaseURL: CommandCodeBase},
	}, true)
	if len(replaced.Reports) != 1 || replaced.Reports[0].Entitlement == nil || replaced.Reports[0].Entitlement.BillingPeriodEndsAt != wantB {
		t.Fatalf("reauth must rebind, got %+v", replaced)
	}
	if replaced.Reports[0].AccountID != "acct-a" {
		t.Fatalf("account %q", replaced.Reports[0].AccountID)
	}
}

func TestA6AndCursorProjectPlanDatesWithoutRawPayloads(t *testing.T) {
	Now = func() time.Time { return time.UnixMilli(1_700_000_000_000) }
	t.Cleanup(func() { Now = time.Now })
	HTTPClient = roundTrip(func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.Contains(req.URL.Path, "/dashboard/billing/subscription"):
			return jsonResp(200, map[string]any{"data": map[string]any{
				"hard_limit_usd":     10.0,
				"current_period_end": 1.700009e12,
				"renews_at":          1.700010e12,
			}}), nil
		case strings.Contains(req.URL.Path, "/api/usage/token"):
			return jsonResp(200, map[string]any{"data": map[string]any{
				"total_granted": 100.0, "total_used": 25.0, "total_available": 75.0, "expires_at": 1.8e12,
			}}), nil
		case strings.Contains(req.URL.Path, "GetCurrentPeriodUsage"):
			return jsonResp(200, map[string]any{
				"billingCycleEnd": 1.700020e12,
				"planUsage":       map[string]any{"limit": 100.0, "remaining": 40.0, "totalPercentUsed": 60.0},
			}), nil
		default:
			t.Fatalf("unexpected %s", req.URL)
			return nil, nil
		}
	})
	t.Cleanup(func() { HTTPClient = http.DefaultClient })
	a6 := FetchReports(nil, []Provider{{Name: "a6api", APIKey: "sk-a6", BaseURL: A6APIBase, AccountID: "a6-1"}}, true)
	if len(a6.Reports) != 1 || a6.Reports[0].Entitlement == nil {
		t.Fatalf("a6 %+v", a6)
	}
	if a6.Reports[0].Quota.CreditsUsd != nil && a6.Reports[0].Quota.CreditsUsd.ExpiresAt != 0 {
		t.Fatalf("token expiry must not become credit expiry %+v", a6.Reports[0].Quota.CreditsUsd)
	}
	if a6.Reports[0].Entitlement.BillingPeriodEndsAt != 1_700_009_000_000 || a6.Reports[0].Entitlement.PlanRenewsAt != 1_700_010_000_000 {
		t.Fatalf("a6 entitlement %+v", a6.Reports[0].Entitlement)
	}
	if a6.Reports[0].Entitlement.CredentialExpiresAt != 0 {
		t.Fatalf("credential %+v", a6.Reports[0].Entitlement)
	}
	cursor := FetchReports(nil, []Provider{{Name: "cursor", AuthMode: "oauth", AccessToken: "tok-cursor", AccountID: "cur-1"}}, true)
	if len(cursor.Reports) != 1 || cursor.Reports[0].Entitlement == nil || cursor.Reports[0].Entitlement.BillingPeriodEndsAt != 1_700_020_000_000 {
		t.Fatalf("cursor %+v", cursor)
	}
	if cursor.Reports[0].Quota.MonthlyResetAt != 0 {
		t.Fatalf("billing cycle must not be relabeled quota reset %+v", cursor.Reports[0].Quota)
	}
	raw, err := json.Marshal(a6.Reports[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "sk-a6") || strings.Contains(string(raw), "hard_limit_usd") {
		t.Fatalf("leaked raw payload: %s", raw)
	}
}

func TestSafeEntitlementJSONOmitsZeroDates(t *testing.T) {
	raw, err := json.Marshal(Report{Provider: "demo", Quota: Quota{UpdatedAt: 1}, Entitlement: &Entitlement{BillingPeriodEndsAt: 2, Confidence: "authoritative"}})
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	ent := payload["entitlement"].(map[string]any)
	if _, ok := ent["credentialExpiresAt"]; ok {
		t.Fatalf("zero credential date must omit: %s", raw)
	}
	if ent["billingPeriodEndsAt"] != float64(2) {
		t.Fatalf("%s", raw)
	}
}
