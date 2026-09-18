package quota

import (
	"math"
	"strconv"
	"strings"
	"sync"
)

func canonicalA6api(base string) bool {
	n := normalizeBaseURL(base)
	return n == A6APIBase || n == A6APIBase+"/v1"
}

func canonicalZai(base string) bool {
	n := normalizeBaseURL(base)
	return n == ZaiBase || n == ZaiBase+"/api/coding/paas/v4"
}

func a6apiPayload(body map[string]any) map[string]any {
	if nested := asRecord(body["data"]); nested != nil {
		return nested
	}
	return body
}

func firstFinite(record map[string]any, names ...string) (float64, bool) {
	if record == nil {
		return 0, false
	}
	for _, name := range names {
		if n, ok := asFloat(record[name]); ok {
			return n, true
		}
	}
	return 0, false
}

func worstKind(a, b probeKind) probeKind {
	if a == probeTerminal || b == probeTerminal {
		return probeTerminal
	}
	if a == probeTransient || b == probeTransient {
		return probeTransient
	}
	return probeNone
}

func probeA6api(provider Provider) probeResult {
	apiKey := resolveEnvValue(provider.APIKey)
	if apiKey == "" {
		return probeResult{kind: probeNone}
	}
	var subStatus, tokStatus int
	var subBody, tokBody map[string]any
	var subErr, tokErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		subStatus, subBody, subErr = bearerGet(A6APIBase+"/dashboard/billing/subscription", apiKey)
	}()
	go func() {
		defer wg.Done()
		tokStatus, tokBody, tokErr = bearerGet(A6APIBase+"/api/usage/token/", apiKey)
	}()
	wg.Wait()
	if subErr != nil || tokErr != nil || subStatus == 0 || tokStatus == 0 {
		return probeResult{kind: probeTransient}
	}
	if subStatus < 200 || subStatus >= 300 || tokStatus < 200 || tokStatus >= 300 {
		return probeResult{kind: worstKind(classifyHTTP(subStatus), classifyHTTP(tokStatus))}
	}
	subscription := a6apiPayload(subBody)
	token := a6apiPayload(tokBody)
	unlimited := token["unlimited_quota"] == true || token["unlimited_quota"] == float64(1) || strings.EqualFold(stringValue(token["unlimited_quota"]), "true")
	now := Now().UnixMilli()
	ent := a6Entitlement(subscription)
	if unlimited {
		credits := &CreditsUsd{Unlimited: true}
		return probeResult{kind: probeFresh, report: boundReport(provider, "a6api:billing", Quota{
			CreditsUsd:    credits,
			CustomWindows: []Window{{Label: "Unlimited API credits", Percent: 0}},
			UpdatedAt:     now,
		}, ent)}
	}
	limitUsd, lok := firstFinite(subscription, "hard_limit_usd")
	granted, gok := firstFinite(token, "total_granted")
	used, uok := firstFinite(token, "total_used")
	available, aok := firstFinite(token, "total_available")
	if !lok || !gok || !uok || !aok || limitUsd <= 0 || granted <= 0 || used < 0 || available < 0 {
		return probeResult{kind: probeTerminal}
	}
	reconciled := used + available
	tol := math.Abs(granted) * 1e-9
	if math.Abs(reconciled-granted) > tol {
		return probeResult{kind: probeTerminal}
	}
	usdPerUnit := limitUsd / granted
	usedUsd := used * usdPerUnit
	remainingUsd := math.Max(0, available*usdPerUnit)
	percent := (usedUsd / limitUsd) * 100
	if percent != percent {
		return probeResult{kind: probeTerminal}
	}
	label := "API credits ($" + formatMoney(remainingUsd) + " of $" + formatMoney(limitUsd) + " remaining)"
	return probeResult{kind: probeFresh, report: boundReport(provider, "a6api:billing", Quota{
		CreditsUsd: &CreditsUsd{
			Used:      usedUsd,
			Limit:     limitUsd,
			Remaining: remainingUsd,
			Percent:   percent,
		},
		CustomWindows: []Window{{Label: label, Percent: percent}},
		UpdatedAt:     now,
	}, ent)}
}

func epochMillis(v any) (int64, bool) {
	switch n := v.(type) {
	case float64:
		if n != n {
			return 0, false
		}
		if n > 1e12 {
			return int64(n), true
		}
		if n > 1e9 {
			return int64(n * 1000), true
		}
		return 0, false
	case string:
		s := strings.TrimSpace(n)
		if s == "" {
			return 0, false
		}
		if ms, ok := parseTimeMillis(s); ok {
			return ms, true
		}
		return 0, false
	default:
		return 0, false
	}
}

func parseFloat(s string) (float64, error) {
	return strconv.ParseFloat(s, 64)
}

func probeZai(provider Provider) probeResult {
	apiKey := resolveEnvValue(provider.APIKey)
	if apiKey == "" {
		return probeResult{kind: probeNone}
	}
	status, body, err := bearerGet(ZaiBase+"/api/monitor/usage/quota/limit", apiKey)
	if res := httpProbe(status, err); res.kind != probeFresh {
		return res
	}
	if success, ok := body["success"].(bool); ok && !success {
		return probeResult{kind: probeTransient}
	}
	data := asRecord(body["data"])
	if data == nil {
		data = body
	}
	quota := Quota{UpdatedAt: Now().UnixMilli()}
	windows := 0
	percentAt := func(keys ...string) (float64, bool) {
		for _, key := range keys {
			if p, ok := asFloat(data[key]); ok {
				return p, true
			}
			nested := asRecord(data["quota"])
			if nested != nil {
				if p, ok := asFloat(nested[key]); ok {
					return p, true
				}
			}
		}
		return 0, false
	}
	if p, ok := percentAt("fiveHourPercent", "fiveHourUsage", "fiveHourUsed"); ok {
		quota.FiveHourPercent = ptr(p)
		windows++
	}
	if p, ok := percentAt("weeklyPercent", "weeklyUsage", "weeklyUsed"); ok {
		quota.WeeklyPercent = ptr(p)
		windows++
	}
	if p, ok := percentAt("monthlyPercent", "mcpPercent", "monthlyMCPUsage"); ok {
		quota.MonthlyPercent = ptr(p)
		windows++
	}
	if windows == 0 {
		return probeResult{kind: probeNone}
	}
	return probeResult{kind: probeFresh, report: makeReport(provider.Name, "zai:quota-limit", quota)}
}
