package quota

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
)

func canonicalKimi(base string) bool {
	return normalizeBaseURL(base) == KimiCodeBase
}

func clampPercent(n float64) float64 {
	if n < 0 {
		return 0
	}
	if n > 100 {
		return 100
	}
	return n
}

func kimiBearer(provider Provider) string {
	if oauthMode(provider) {
		return strings.TrimSpace(provider.AccessToken)
	}
	return resolveEnvValue(provider.APIKey)
}

func unwrapKimiPayload(body map[string]any) map[string]any {
	nested := asRecord(body["data"])
	if nested == nil {
		return body
	}
	usable := func(v any) bool { return v != nil }
	outer := usable(body["usage"]) || usable(body["limits"]) || usable(body["totalQuota"])
	inner := usable(nested["usage"]) || usable(nested["limits"]) || usable(nested["totalQuota"])
	if !outer && inner {
		return nested
	}
	return body
}

func parseKimiRow(value any, resetFallback map[string]any) *windowPercent {
	row := asRecord(value)
	if row == nil {
		return nil
	}
	resetAt, _ := quotaResetAt(row)
	if resetAt == 0 && resetFallback != nil {
		resetAt, _ = quotaResetAt(resetFallback)
	}
	if limit, ok := asFloat(row["limit"]); ok && limit > 0 {
		used, uok := asFloat(row["used"])
		if !uok {
			if remaining, rok := asFloat(row["remaining"]); rok {
				used, uok = limit-remaining, true
			}
		}
		if uok {
			return &windowPercent{percent: clampPercent((used / limit) * 100), resetAt: resetAt}
		}
	}
	for _, key := range []string{"utilization", "percent", "usedPercent", "used_percent"} {
		if n, ok := asFloat(row[key]); ok {
			return &windowPercent{percent: clampPercent(n), resetAt: resetAt}
		}
	}
	return nil
}

func kimiLabel(item, detail map[string]any) string {
	parts := []string{}
	for _, rec := range []map[string]any{item, detail} {
		for _, key := range []string{"name", "title", "scope"} {
			if s := strings.TrimSpace(stringValue(rec[key])); s != "" {
				parts = append(parts, strings.ToLower(s))
			}
		}
	}
	return strings.Join(parts, " ")
}

func isKimiFiveHour(item, detail, window map[string]any) bool {
	duration, _ := asFloat(window["duration"])
	if duration == 0 {
		duration, _ = asFloat(item["duration"])
	}
	if duration == 0 {
		duration, _ = asFloat(detail["duration"])
	}
	unit := strings.ToUpper(strings.Join([]string{
		stringValue(window["timeUnit"]), stringValue(item["timeUnit"]), stringValue(detail["timeUnit"]),
	}, " "))
	if (strings.Contains(unit, "MINUTE") && duration == 300) || (strings.Contains(unit, "HOUR") && duration == 5) {
		return true
	}
	label := kimiLabel(item, detail)
	return strings.Contains(label, "5h") || strings.Contains(label, "5 h") || strings.Contains(label, "5 hour")
}

func isKimiWeekly(item, detail, window map[string]any) bool {
	duration, _ := asFloat(window["duration"])
	if duration == 0 {
		duration, _ = asFloat(item["duration"])
	}
	if duration == 0 {
		duration, _ = asFloat(detail["duration"])
	}
	unit := strings.ToUpper(strings.Join([]string{
		stringValue(window["timeUnit"]), stringValue(item["timeUnit"]), stringValue(detail["timeUnit"]),
	}, " "))
	if (strings.Contains(unit, "DAY") && duration == 7) || (strings.Contains(unit, "HOUR") && duration == 168) {
		return true
	}
	label := kimiLabel(item, detail)
	return strings.Contains(label, "weekly") || strings.Contains(label, "7d") || strings.Contains(label, "7 day")
}

func probeKimi(provider Provider) probeResult {
	if !canonicalKimi(provider.BaseURL) {
		return probeResult{kind: probeNone}
	}
	bearer := kimiBearer(provider)
	if bearer == "" {
		return probeResult{kind: probeNone}
	}
	status, body, err := bearerGet(KimiCodeBase+"/usages", bearer)
	if res := httpProbe(status, err); res.kind != probeFresh {
		return res
	}
	payload := unwrapKimiPayload(body)
	weekly := parseKimiRow(payload["usage"], nil)
	total := parseKimiRow(payload["totalQuota"], nil)
	var five *windowPercent
	if rows, ok := payload["limits"].([]any); ok {
		for _, raw := range rows {
			item := asRecord(raw)
			if item == nil {
				continue
			}
			detail := asRecord(item["detail"])
			if detail == nil {
				detail = item
			}
			window := asRecord(item["window"])
			if window == nil {
				window = map[string]any{}
			}
			if five == nil && isKimiFiveHour(item, detail, window) {
				five = parseKimiRow(detail, window)
			}
			if weekly == nil && isKimiWeekly(item, detail, window) {
				weekly = parseKimiRow(detail, window)
			}
			if five != nil && weekly != nil {
				break
			}
		}
	}
	quota := Quota{UpdatedAt: Now().UnixMilli()}
	windows := 0
	if five != nil {
		quota.FiveHourPercent = ptr(five.percent)
		quota.FiveHourResetAt = five.resetAt
		windows++
	}
	if weekly != nil {
		quota.WeeklyPercent = ptr(weekly.percent)
		quota.WeeklyResetAt = weekly.resetAt
		windows++
	}
	if total != nil {
		quota.CustomWindows = append(quota.CustomWindows, Window{Label: "Total subscription credits", Percent: total.percent, ResetAt: total.resetAt})
		windows++
	}
	if windows == 0 {
		return probeResult{kind: probeNone}
	}
	return probeResult{kind: probeFresh, report: makeReport(provider.Name, "kimi:usages", quota)}
}

func xaiUserID(provider Provider) string {
	if id := strings.TrimSpace(provider.AccountID); id != "" {
		return id
	}
	parts := strings.Split(provider.AccessToken, ".")
	if len(parts) < 2 {
		return ""
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var payload struct {
		Sub string `json:"sub"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return ""
	}
	return strings.TrimSpace(payload.Sub)
}

func xaiHeaders(userID string) http.Header {
	h := make(http.Header)
	h.Set("x-xai-token-auth", "xai-grok-cli")
	h.Set("x-authenticateresponse", "authenticate-response")
	h.Set("x-grok-client-version", "0.2.93")
	if userID != "" {
		h.Set("x-userid", userID)
	}
	return h
}

func probeXai(provider Provider) probeResult {
	token := strings.TrimSpace(provider.AccessToken)
	if token == "" {
		return probeResult{kind: probeNone}
	}
	userID := xaiUserID(provider)
	if userID != "" {
		status, body, err := bearerJSON(http.MethodGet, XAIBillingBase+"?format=credits", token, nil, xaiHeaders(userID))
		if err == nil && status >= 200 && status < 300 {
			if weekly := parseXaiCredits(body); weekly != nil {
				return probeResult{kind: probeFresh, report: makeReport(provider.Name, "xai:grok-billing-credits", *weekly)}
			}
		}
	}
	status, body, err := bearerJSON(http.MethodGet, XAIBillingBase, token, nil, xaiHeaders(userID))
	if res := httpProbe(status, err); res.kind != probeFresh {
		return res
	}
	cfg := asRecord(body["config"])
	if cfg == nil {
		return probeResult{kind: probeNone}
	}
	limit, lok := centsValue(cfg["monthlyLimit"])
	used, uok := centsValue(cfg["used"])
	if !lok || !uok || limit <= 0 {
		return probeResult{kind: probeNone}
	}
	quota := Quota{MonthlyPercent: ptr(clampPercent((used / limit) * 100)), UpdatedAt: Now().UnixMilli()}
	return probeResult{kind: probeFresh, report: boundReport(provider, "xai:grok-billing", quota, authoritativeEntitlement("xai:billing-period", xaiBillingEnd(cfg), 0, 0, 0))}
}

func parseXaiCredits(body map[string]any) *Quota {
	cfg := asRecord(body["config"])
	period := asRecord(cfg["currentPeriod"])
	if period == nil || stringValue(period["type"]) != "USAGE_PERIOD_TYPE_WEEKLY" {
		return nil
	}
	resetAt, ok := epochMillis(period["end"])
	if !ok {
		return nil
	}
	percent := 0.0
	if cfg["creditUsagePercent"] != nil {
		n, nOk := asFloat(cfg["creditUsagePercent"])
		if !nOk {
			return nil
		}
		percent = clampPercent(n)
	}
	quota := Quota{WeeklyPercent: ptr(percent), WeeklyResetAt: resetAt, UpdatedAt: Now().UnixMilli()}
	return &quota
}

func centsValue(v any) (float64, bool) {
	rec := asRecord(v)
	if rec == nil {
		return 0, false
	}
	return asFloat(rec["val"])
}

func probeAnthropic(provider Provider) probeResult {
	token := strings.TrimSpace(provider.AccessToken)
	if token == "" {
		return probeResult{kind: probeNone}
	}
	extra := make(http.Header)
	extra.Set("Content-Type", "application/json")
	extra.Set("User-Agent", "claude-cli/2.1.63 (external, cli)")
	extra.Set("anthropic-beta", "claude-code-20250219,oauth-2025-04-20,interleaved-thinking-2025-05-14,context-management-2025-06-27,prompt-caching-scope-2026-01-05")
	status, body, err := bearerJSON(http.MethodGet, AnthropicAPIBase+"/api/oauth/usage", token, nil, extra)
	if res := httpProbe(status, err); res.kind != probeFresh {
		return res
	}
	quota := Quota{UpdatedAt: Now().UnixMilli()}
	windows := 0
	if five := parseClaudeBucket(body["five_hour"]); five != nil {
		quota.FiveHourPercent = ptr(five.percent)
		quota.FiveHourResetAt = five.resetAt
		windows++
	}
	if weekly := parseClaudeBucket(body["seven_day"]); weekly != nil {
		quota.WeeklyPercent = ptr(weekly.percent)
		quota.WeeklyResetAt = weekly.resetAt
		windows++
	}
	if opus := parseClaudeBucket(body["seven_day_opus"]); opus != nil {
		quota.CustomWindows = append(quota.CustomWindows, Window{Label: "Opus", Percent: opus.percent, ResetAt: opus.resetAt})
		windows++
	}
	if sonnet := parseClaudeBucket(body["seven_day_sonnet"]); sonnet != nil {
		quota.CustomWindows = append(quota.CustomWindows, Window{Label: "Sonnet", Percent: sonnet.percent, ResetAt: sonnet.resetAt})
		windows++
	}
	if windows == 0 {
		return probeResult{kind: probeNone}
	}
	return probeResult{kind: probeFresh, report: makeReport(provider.Name, "anthropic:oauth-usage", quota)}
}

func parseClaudeBucket(value any) *windowPercent {
	rec := asRecord(value)
	if rec == nil {
		return nil
	}
	out := &windowPercent{}
	found := false
	if n, ok := asFloat(rec["utilization"]); ok {
		out.percent = clampPercent(n)
		found = true
	}
	if ms, ok := epochMillis(rec["resets_at"]); ok {
		out.resetAt = ms
		found = true
	}
	if !found {
		return nil
	}
	return out
}
