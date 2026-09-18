package quota

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) Do(req *http.Request) (*http.Response, error) { return f(req) }

func jsonResp(code int, body any) *http.Response {
	raw, _ := json.Marshal(body)
	return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(string(raw))), Header: make(http.Header)}
}

func TestFetchReportsOpenRouterAndLastGood(t *testing.T) {
	Now = func() time.Time { return time.UnixMilli(1_700_000_000_000) }
	t.Cleanup(func() { Now = time.Now })
	var hits int
	HTTPClient = roundTrip(func(req *http.Request) (*http.Response, error) {
		hits++
		if req.URL.Path != "/api/v1/key" {
			t.Fatalf("path %s", req.URL.Path)
		}
		if req.Header.Get("Authorization") != "Bearer sk-or-secret" {
			t.Fatalf("auth %q", req.Header.Get("Authorization"))
		}
		if hits == 1 {
			return jsonResp(200, map[string]any{"data": map[string]any{"limit": 10.0, "limit_remaining": 7.5}}), nil
		}
		return jsonResp(503, map[string]any{"error": "down"}), nil
	})
	t.Cleanup(func() { HTTPClient = http.DefaultClient })
	store := &Store{}
	providers := []Provider{{Name: "openrouter", APIKey: "sk-or-secret", BaseURL: OpenRouterBase, AuthMode: "key"}}
	first := FetchReports(store, providers, true)
	if len(first.Reports) != 1 || first.Reports[0].Source != "openrouter:key-info" {
		t.Fatalf("first %+v", first)
	}
	if strings.Contains(first.Reports[0].Quota.CustomWindows[0].Label, "sk-or") {
		t.Fatal("leaked key")
	}
	second := FetchReports(store, providers, true)
	if len(second.Reports) != 1 || second.Reports[0].UpdatedAt != first.Reports[0].UpdatedAt {
		t.Fatalf("last-good not preserved: %+v", second)
	}
}

func TestFetchReportsTerminalDropsLastGood(t *testing.T) {
	Now = func() time.Time { return time.UnixMilli(1_700_000_000_000) }
	t.Cleanup(func() { Now = time.Now })
	HTTPClient = roundTrip(func(req *http.Request) (*http.Response, error) {
		return jsonResp(401, map[string]any{"error": "no"}), nil
	})
	t.Cleanup(func() { HTTPClient = http.DefaultClient })
	store := &Store{
		key:   cacheKey([]Provider{{Name: "openrouter", APIKey: "sk", BaseURL: OpenRouterBase}}),
		ts:    Now(),
		value: Response{Reports: []Report{{Provider: "openrouter", UpdatedAt: Now().UnixMilli()}}},
	}
	out := FetchReports(store, []Provider{{Name: "openrouter", APIKey: "sk", BaseURL: OpenRouterBase}}, true)
	if len(out.Reports) != 0 {
		t.Fatalf("wanted drop, got %+v", out)
	}
}

func TestFetchReportsRejectsLookalikeHost(t *testing.T) {
	HTTPClient = roundTrip(func(*http.Request) (*http.Response, error) {
		t.Fatal("must not probe lookalike host")
		return nil, nil
	})
	t.Cleanup(func() { HTTPClient = http.DefaultClient })
	out := FetchReports(nil, []Provider{{Name: "openrouter", APIKey: "sk", BaseURL: "https://evil.example/openrouter"}}, true)
	if len(out.Reports) != 0 {
		t.Fatalf("%+v", out)
	}
}

func TestFetchReportsDeepSeek(t *testing.T) {
	Now = func() time.Time { return time.UnixMilli(1_700_000_000_000) }
	t.Cleanup(func() { Now = time.Now })
	HTTPClient = roundTrip(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/user/balance" {
			t.Fatalf("path %s", req.URL.Path)
		}
		return jsonResp(200, map[string]any{"balance_infos": []any{
			map[string]any{"currency": "CNY", "total_balance": 1.0},
			map[string]any{"currency": "USD", "total_balance": 12.5, "granted_balance": 10.0},
		}}), nil
	})
	t.Cleanup(func() { HTTPClient = http.DefaultClient })
	out := FetchReports(nil, []Provider{{Name: "deepseek", APIKey: "sk-ds", BaseURL: DeepSeekBase + "/v1"}}, true)
	if len(out.Reports) != 1 || out.Reports[0].Source != "deepseek:balance" {
		t.Fatalf("%+v", out)
	}
	if !strings.Contains(out.Reports[0].Quota.CustomWindows[0].Label, "12.50") {
		t.Fatalf("label %q", out.Reports[0].Quota.CustomWindows[0].Label)
	}
}

func TestFetchReportsMoonshotAndCline404(t *testing.T) {
	Now = func() time.Time { return time.UnixMilli(1_700_000_000_000) }
	t.Cleanup(func() { Now = time.Now })
	HTTPClient = roundTrip(func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Path, "users/me/balance") {
			return jsonResp(200, map[string]any{"data": map[string]any{"available_balance": 3.5, "voucher_balance": 1.0, "cash_balance": 2.5}}), nil
		}
		if strings.Contains(req.URL.Path, "usage-limits") {
			return jsonResp(404, map[string]any{"error": "no plan"}), nil
		}
		t.Fatalf("unexpected %s", req.URL)
		return nil, nil
	})
	t.Cleanup(func() { HTTPClient = http.DefaultClient })
	moon := FetchReports(nil, []Provider{{Name: "moonshot", APIKey: "sk", BaseURL: MoonshotAI}}, true)
	if len(moon.Reports) != 1 || moon.Reports[0].Source != "moonshot:balance" {
		t.Fatalf("moon %+v", moon)
	}
	if !strings.Contains(moon.Reports[0].Quota.CustomWindows[0].Label, "USD") {
		t.Fatalf("label %q", moon.Reports[0].Quota.CustomWindows[0].Label)
	}
	cline := FetchReports(nil, []Provider{{Name: "cline-pass", APIKey: "sk", BaseURL: ClineBase}}, true)
	if len(cline.Reports) != 0 {
		t.Fatalf("cline 404 should be no-report, got %+v", cline)
	}
}

func TestFetchReportsA6apiAndZai(t *testing.T) {
	Now = func() time.Time { return time.UnixMilli(1_700_000_000_000) }
	t.Cleanup(func() { Now = time.Now })
	HTTPClient = roundTrip(func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.Contains(req.URL.Path, "/dashboard/billing/subscription"):
			if req.Header.Get("Authorization") != "Bearer sk-a6" {
				t.Fatalf("auth %q", req.Header.Get("Authorization"))
			}
			return jsonResp(200, map[string]any{"data": map[string]any{"hard_limit_usd": 10.0}}), nil
		case strings.Contains(req.URL.Path, "/api/usage/token"):
			return jsonResp(200, map[string]any{"data": map[string]any{
				"total_granted": 100.0, "total_used": 25.0, "total_available": 75.0,
			}}), nil
		case strings.Contains(req.URL.Path, "/api/monitor/usage/quota/limit"):
			return jsonResp(200, map[string]any{"success": true, "data": map[string]any{
				"fiveHourPercent": 12.0, "weeklyPercent": 34.0, "mcpPercent": 56.0,
			}}), nil
		default:
			t.Fatalf("unexpected %s", req.URL)
			return nil, nil
		}
	})
	t.Cleanup(func() { HTTPClient = http.DefaultClient })
	a6 := FetchReports(nil, []Provider{{Name: "a6api", APIKey: "sk-a6", BaseURL: A6APIBase + "/v1"}}, true)
	if len(a6.Reports) != 1 || a6.Reports[0].Source != "a6api:billing" {
		t.Fatalf("a6 %+v", a6)
	}
	if a6.Reports[0].Quota.CreditsUsd == nil || a6.Reports[0].Quota.CreditsUsd.Remaining != 7.5 {
		t.Fatalf("credits %+v", a6.Reports[0].Quota.CreditsUsd)
	}
	if strings.Contains(a6.Reports[0].Quota.CustomWindows[0].Label, "sk-a6") {
		t.Fatal("leaked key")
	}
	zai := FetchReports(nil, []Provider{{Name: "z-ai", APIKey: "sk-z", BaseURL: ZaiBase + "/api/coding/paas/v4"}}, true)
	if len(zai.Reports) != 1 || zai.Reports[0].Source != "zai:quota-limit" {
		t.Fatalf("zai %+v", zai)
	}
	if zai.Reports[0].Quota.FiveHourPercent == nil || *zai.Reports[0].Quota.FiveHourPercent != 12 {
		t.Fatalf("five %+v", zai.Reports[0].Quota)
	}
}

func TestFetchReportsKiroUsageLimits(t *testing.T) {
	Now = func() time.Time { return time.UnixMilli(1_700_000_000_000) }
	t.Cleanup(func() { Now = time.Now })
	var gotURL, gotTarget, gotAuth, gotBody string
	HTTPClient = roundTrip(func(req *http.Request) (*http.Response, error) {
		gotURL = req.URL.String()
		gotTarget = req.Header.Get("x-amz-target")
		gotAuth = req.Header.Get("Authorization")
		raw, _ := io.ReadAll(req.Body)
		gotBody = string(raw)
		return jsonResp(200, map[string]any{
			"usageBreakdownList": []any{
				map[string]any{"resourceType": "CREDIT", "currentUsageWithPrecision": 1.0, "usageLimitWithPrecision": 1.0},
				map[string]any{"resourceType": "AGENTIC_REQUEST", "currentUsageWithPrecision": 20.0, "usageLimitWithPrecision": 50.0, "nextDateReset": 1.700000100e12},
			},
			"userInfo": map[string]any{"email": "hidden@example.com"},
		}), nil
	})
	t.Cleanup(func() { HTTPClient = http.DefaultClient })
	out := FetchReports(nil, []Provider{{
		Name: "kiro", AuthMode: "oauth", AccessToken: "tok-k", AccountID: "arn:aws:codewhisperer:eu-west-1:123456789012:profile/b",
		BaseURL: "https://runtime.eu-west-1.kiro.dev",
	}}, true)
	if len(out.Reports) != 1 || out.Reports[0].Source != "kiro:get-usage-limits" {
		t.Fatalf("%+v", out)
	}
	if gotURL != "https://management.eu-west-1.kiro.dev/" || gotTarget != "AmazonCodeWhispererService.GetUsageLimits" || gotAuth != "Bearer tok-k" {
		t.Fatalf("url=%s target=%s auth=%s", gotURL, gotTarget, gotAuth)
	}
	if !strings.Contains(gotBody, "profile/b") {
		t.Fatalf("body=%s", gotBody)
	}
	if out.Reports[0].Quota.MonthlyPercent == nil || *out.Reports[0].Quota.MonthlyPercent != 40 {
		t.Fatalf("quota=%+v", out.Reports[0].Quota)
	}
	encoded, _ := json.Marshal(out.Reports[0])
	if strings.Contains(string(encoded), "hidden@example.com") {
		t.Fatalf("leaked email: %s", encoded)
	}
	HTTPClient = roundTrip(func(*http.Request) (*http.Response, error) {
		t.Fatal("must not probe evil region host")
		return nil, nil
	})
	blocked := FetchReports(nil, []Provider{{
		Name: "kiro", AuthMode: "oauth", AccessToken: "tok-k", BaseURL: "https://runtime.evil.kiro.dev",
	}}, true)
	if len(blocked.Reports) != 0 {
		t.Fatalf("evil region reports=%+v", blocked)
	}
}

func TestFetchReportsA6apiLookalikeAndTerminal(t *testing.T) {
	HTTPClient = roundTrip(func(*http.Request) (*http.Response, error) {
		t.Fatal("must not probe lookalike host")
		return nil, nil
	})
	t.Cleanup(func() { HTTPClient = http.DefaultClient })
	out := FetchReports(nil, []Provider{{Name: "a6api", APIKey: "sk", BaseURL: "https://evil.example/a6api"}}, true)
	if len(out.Reports) != 0 {
		t.Fatalf("%+v", out)
	}
	HTTPClient = roundTrip(func(req *http.Request) (*http.Response, error) {
		return jsonResp(401, map[string]any{"error": "no"}), nil
	})
	store := &Store{
		key:   cacheKey([]Provider{{Name: "a6api", APIKey: "sk", BaseURL: A6APIBase}}),
		ts:    time.UnixMilli(1_700_000_000_000),
		value: Response{Reports: []Report{{Provider: "a6api", UpdatedAt: 1_700_000_000_000}}},
	}
	Now = func() time.Time { return time.UnixMilli(1_700_000_000_000) }
	t.Cleanup(func() { Now = time.Now })
	dropped := FetchReports(store, []Provider{{Name: "a6api", APIKey: "sk", BaseURL: A6APIBase}}, true)
	if len(dropped.Reports) != 0 {
		t.Fatalf("wanted drop, got %+v", dropped)
	}
}

func TestFetchReportsCommandCodeAndCursor(t *testing.T) {
	Now = func() time.Time { return time.UnixMilli(1_700_000_000_000) }
	t.Cleanup(func() { Now = time.Now })
	HTTPClient = roundTrip(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("Authorization") == "Bearer tok-cc" && strings.Contains(req.URL.Path, "/alpha/billing/credits") {
			return jsonResp(200, map[string]any{"data": map[string]any{
				"windowLimits": map[string]any{"fiveHour": map[string]any{"cap": 100.0, "used": 25.0}},
			}}), nil
		}
		if strings.Contains(req.URL.Path, "GetCurrentPeriodUsage") {
			if req.Header.Get("Authorization") != "Bearer tok-cursor" {
				t.Fatalf("cursor auth %q", req.Header.Get("Authorization"))
			}
			return jsonResp(200, map[string]any{"planUsage": map[string]any{"limit": 100.0, "remaining": 40.0, "totalPercentUsed": 60.0}}), nil
		}
		if strings.Contains(req.URL.Host, "evil") {
			t.Fatal("lookalike")
		}
		return jsonResp(404, map[string]any{"error": "no"}), nil
	})
	t.Cleanup(func() { HTTPClient = http.DefaultClient })
	cc := FetchReports(nil, []Provider{{Name: "command-code", AuthMode: "oauth", AccessToken: "tok-cc", BaseURL: CommandCodeBase}}, true)
	if len(cc.Reports) != 1 || cc.Reports[0].Source != "command-code:credits" || cc.Reports[0].Quota.FiveHourPercent == nil || *cc.Reports[0].Quota.FiveHourPercent != 25 {
		t.Fatalf("cc %+v", cc)
	}
	if strings.Contains(cc.Reports[0].Source, "tok-") || (len(cc.Reports[0].Quota.CustomWindows) > 0 && strings.Contains(cc.Reports[0].Quota.CustomWindows[0].Label, "tok-cc")) {
		t.Fatal("leaked token")
	}
	cursor := FetchReports(nil, []Provider{{Name: "cursor", AuthMode: "oauth", AccessToken: "tok-cursor"}}, true)
	if len(cursor.Reports) != 1 || cursor.Reports[0].Source != "cursor:period-usage" || !cursor.Reports[0].ReverseEngineered {
		t.Fatalf("cursor %+v", cursor)
	}
	lookalike := FetchReports(nil, []Provider{{Name: "command-code", AuthMode: "oauth", AccessToken: "tok-cc", BaseURL: "https://evil.example/commandcode"}}, true)
	if len(lookalike.Reports) != 0 {
		t.Fatalf("lookalike %+v", lookalike)
	}
}

func TestFetchReportsKimiXaiAnthropic(t *testing.T) {
	Now = func() time.Time { return time.UnixMilli(1_700_000_000_000) }
	t.Cleanup(func() { Now = time.Now })
	HTTPClient = roundTrip(func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.Contains(req.URL.Path, "/usages"):
			if req.Header.Get("Authorization") != "Bearer tok-kimi" {
				t.Fatalf("kimi auth %q", req.Header.Get("Authorization"))
			}
			return jsonResp(200, map[string]any{"data": map[string]any{
				"usage":  map[string]any{"limit": 100.0, "used": 40.0},
				"limits": []any{map[string]any{"name": "5 hour", "detail": map[string]any{"limit": 10.0, "used": 2.0}, "window": map[string]any{"duration": 5.0, "timeUnit": "HOUR"}}},
			}}), nil
		case strings.Contains(req.URL.RawQuery, "format=credits"):
			if req.Header.Get("x-userid") != "user-1" || req.Header.Get("Authorization") != "Bearer tok-xai" {
				t.Fatalf("xai headers userid=%q auth=%q", req.Header.Get("x-userid"), req.Header.Get("Authorization"))
			}
			return jsonResp(200, map[string]any{"config": map[string]any{
				"creditUsagePercent": 12.0,
				"currentPeriod":      map[string]any{"type": "USAGE_PERIOD_TYPE_WEEKLY", "end": 1.8e12},
			}}), nil
		case strings.Contains(req.URL.Path, "/api/oauth/usage"):
			if req.Header.Get("Authorization") != "Bearer tok-anth" {
				t.Fatalf("anth auth %q", req.Header.Get("Authorization"))
			}
			return jsonResp(200, map[string]any{
				"five_hour": map[string]any{"utilization": 30.0},
				"seven_day": map[string]any{"utilization": 50.0},
			}), nil
		default:
			t.Fatalf("unexpected %s", req.URL)
			return nil, nil
		}
	})
	t.Cleanup(func() { HTTPClient = http.DefaultClient })
	kimi := FetchReports(nil, []Provider{{Name: "kimi", AuthMode: "oauth", AccessToken: "tok-kimi", BaseURL: KimiCodeBase}}, true)
	if len(kimi.Reports) != 1 || kimi.Reports[0].Source != "kimi:usages" || kimi.Reports[0].Quota.WeeklyPercent == nil || *kimi.Reports[0].Quota.WeeklyPercent != 40 {
		t.Fatalf("kimi %+v", kimi)
	}
	xai := FetchReports(nil, []Provider{{Name: "xai", AuthMode: "oauth", AccessToken: "tok-xai", AccountID: "user-1"}}, true)
	if len(xai.Reports) != 1 || xai.Reports[0].Source != "xai:grok-billing-credits" {
		t.Fatalf("xai %+v", xai)
	}
	if strings.Contains(xai.Reports[0].Source, "tok-") {
		t.Fatal("leaked xai token")
	}
	anth := FetchReports(nil, []Provider{{Name: "anthropic", AuthMode: "oauth", AccessToken: "tok-anth"}}, true)
	if len(anth.Reports) != 1 || anth.Reports[0].Source != "anthropic:oauth-usage" || anth.Reports[0].Quota.FiveHourPercent == nil || *anth.Reports[0].Quota.FiveHourPercent != 30 {
		t.Fatalf("anth %+v", anth)
	}
	lookalike := FetchReports(nil, []Provider{{Name: "kimi", AuthMode: "oauth", AccessToken: "tok-kimi", BaseURL: "https://evil.example/kimi"}}, true)
	if len(lookalike.Reports) != 0 {
		t.Fatalf("lookalike %+v", lookalike)
	}
}
