package quota

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func canonicalCommandCode(base string) bool {
	n := normalizeBaseURL(base)
	return n == CommandCodeBase || n == CommandCodeBase+"/provider/v1"
}

func canonicalAntigravity(base string) bool {
	n := normalizeBaseURL(base)
	return n == "" || n == AntigravityAPIBase
}

func commandCodeBearer(provider Provider) string {
	if oauthMode(provider) {
		return strings.TrimSpace(provider.AccessToken)
	}
	return resolveEnvValue(provider.APIKey)
}

func unwrapData(body map[string]any) map[string]any {
	if nested := asRecord(body["data"]); nested != nil {
		return nested
	}
	return body
}

func nilSafe(record map[string]any, key string) any {
	if record == nil {
		return nil
	}
	return record[key]
}

type windowPercent struct {
	percent float64
	resetAt int64
}

func probeCommandCode(provider Provider) probeResult {
	if !canonicalCommandCode(provider.BaseURL) {
		return probeResult{kind: probeNone}
	}
	bearer := commandCodeBearer(provider)
	if bearer == "" {
		return probeResult{kind: probeNone}
	}
	orgQuery := ""
	if status, whoami, err := bearerGet(CommandCodeBase+"/alpha/whoami", bearer); err == nil && status >= 200 && status < 300 {
		org := asRecord(unwrapData(whoami)["org"])
		if id := strings.TrimSpace(stringValue(org["id"])); id != "" {
			orgQuery = "?orgId=" + url.QueryEscape(id)
		}
	}
	status, body, err := bearerGet(CommandCodeBase+"/alpha/billing/credits"+orgQuery, bearer)
	if res := httpProbe(status, err); res.kind != probeFresh {
		return res
	}
	payload := unwrapData(body)
	credits := asRecord(payload["credits"])
	limits := asRecord(payload["windowLimits"])
	if credits == nil && limits == nil {
		return probeResult{kind: probeNone}
	}
	quota := Quota{UpdatedAt: Now().UnixMilli()}
	windows := 0
	if five := parseCommandCodeWindow(asRecord(nilSafe(limits, "fiveHour"))); five != nil {
		quota.FiveHourPercent = ptr(five.percent)
		quota.FiveHourResetAt = five.resetAt
		windows++
	}
	if weekly := parseCommandCodeWindow(asRecord(nilSafe(limits, "weekly"))); weekly != nil {
		quota.WeeklyPercent = ptr(weekly.percent)
		quota.WeeklyResetAt = weekly.resetAt
		windows++
	}
	sub := commandCodeSubscription(bearer, orgQuery)
	if spend := commandCodeCredits(credits, sub, bearer, orgQuery); spend != nil {
		quota.CreditsUsd = spend
		windows++
	}
	ent := commandCodeEntitlement(sub)
	if windows == 0 && ent == nil {
		return probeResult{kind: probeNone}
	}
	return probeResult{kind: probeFresh, report: boundReport(provider, "command-code:credits", quota, ent)}
}

func parseCommandCodeWindow(row map[string]any) *windowPercent {
	if row == nil {
		return nil
	}
	cap, cok := asFloat(row["cap"])
	used, uok := asFloat(row["used"])
	if !cok || !uok || cap <= 0 || used < 0 {
		return nil
	}
	out := &windowPercent{percent: (used / cap) * 100}
	if ms, ok := quotaResetAt(row); ok {
		out.resetAt = ms
	}
	return out
}

func quotaResetAt(row map[string]any) (int64, bool) {
	for _, key := range []string{"resetTime", "resetAt", "reset_time", "reset_at"} {
		if ms, ok := epochMillis(row[key]); ok && ms > 0 {
			return ms, true
		}
	}
	return 0, false
}

func commandCodeSubscription(bearer, orgQuery string) map[string]any {
	status, subBody, err := bearerGet(CommandCodeBase+"/alpha/billing/subscriptions"+orgQuery, bearer)
	if err != nil || status < 200 || status >= 300 {
		return nil
	}
	return unwrapData(subBody)
}

func commandCodeEntitlement(subscription map[string]any) *Entitlement {
	end, ok := firstEpoch(subscription, "currentPeriodEnd", "current_period_end")
	if !ok {
		return nil
	}
	return authoritativeEntitlement("command-code:subscription", end, 0, 0, 0)
}

func commandCodeCredits(credits, subscription map[string]any, bearer, orgQuery string) *CreditsUsd {
	if credits == nil || subscription == nil {
		return nil
	}
	periodStart := strings.TrimSpace(stringValue(subscription["currentPeriodStart"]))
	if periodStart == "" {
		return nil
	}
	sep := "?"
	if orgQuery != "" {
		sep = "&"
	}
	summaryStatus, summaryBody, summaryErr := bearerGet(CommandCodeBase+"/alpha/usage/summary"+orgQuery+sep+"since="+url.QueryEscape(periodStart), bearer)
	if summaryErr != nil || summaryStatus < 200 || summaryStatus >= 300 {
		return nil
	}
	summary := unwrapData(summaryBody)
	used, uok := asFloat(summary["totalCost"])
	if !uok {
		used, uok = asFloat(summary["totalMonthlyCredits"])
	}
	if !uok || used < 0 {
		return nil
	}
	var remaining float64
	pools := 0
	for _, key := range []string{"monthlyCredits", "purchasedCredits", "freeCredits"} {
		if n, ok := asFloat(credits[key]); ok {
			remaining += max(0, n)
			pools++
		}
	}
	if pools == 0 {
		return nil
	}
	limit := used + remaining
	percent := 0.0
	if limit > 0 {
		percent = (used / limit) * 100
	}
	return &CreditsUsd{Used: used, Limit: limit, Remaining: remaining, Percent: percent}
}

func probeCursor(provider Provider) probeResult {
	token := strings.TrimSpace(provider.AccessToken)
	if token == "" {
		return probeResult{kind: probeNone}
	}
	if report, ok := cursorPeriodUsage(token, provider.Name); ok {
		return probeResult{kind: probeFresh, report: bindReport(report, provider, report.Entitlement)}
	}
	if report, ok := cursorUsageSummary(token, provider.Name); ok {
		return probeResult{kind: probeFresh, report: bindReport(report, provider, report.Entitlement)}
	}
	result := cursorAuthUsage(token, provider.Name)
	if result.kind == probeFresh {
		result.report = bindReport(result.report, provider, result.report.Entitlement)
	}
	return result
}

func cursorHeaders() http.Header {
	h := make(http.Header)
	h.Set("User-Agent", "benes-quota")
	return h
}

func cursorPeriodUsage(token, name string) (Report, bool) {
	extra := cursorHeaders()
	extra.Set("Content-Type", "application/json")
	extra.Set("Connect-Protocol-Version", "1")
	status, body, err := bearerJSON(http.MethodPost, CursorAPIBase+"/aiserver.v1.DashboardService/GetCurrentPeriodUsage", token, []byte("{}"), extra)
	if err != nil || status < 200 || status >= 300 {
		return Report{}, false
	}
	plan := asRecord(body["planUsage"])
	if plan == nil {
		return Report{}, false
	}
	billingEnd := cursorBillingEnd(body, plan)
	limit, lok := firstFinite(plan, "limit", "limitCents", "totalLimitCents")
	remaining, rok := firstFinite(plan, "remaining", "remainingCents")
	included, iok := firstFinite(plan, "includedSpend", "usedCents", "used")
	totalSpend, tok := asFloat(plan["totalSpend"])
	var used float64
	var hasUsed bool
	if iok {
		used, hasUsed = included, true
	} else if lok && rok {
		used, hasUsed = max(0, limit-remaining), true
	} else if tok {
		used, hasUsed = totalSpend, true
	}
	var totalPercent float64
	var hasPercent bool
	if p, ok := firstFinite(plan, "totalPercentUsed", "percentUsed"); ok {
		totalPercent, hasPercent = p, true
	} else if hasUsed && lok && limit > 0 {
		totalPercent, hasPercent = (used/limit)*100, true
	}
	var windows []Window
	if p, ok := asFloat(plan["autoPercentUsed"]); ok {
		windows = append(windows, Window{Label: "First-party models", Percent: p})
	}
	if p, ok := asFloat(plan["apiPercentUsed"]); ok {
		windows = append(windows, Window{Label: "API usage", Percent: p})
	}
	if !hasPercent && len(windows) == 0 {
		return Report{}, false
	}
	quota := Quota{CustomWindows: windows, UpdatedAt: Now().UnixMilli()}
	if hasPercent {
		quota.MonthlyPercent = ptr(totalPercent)
	}
	out := makeReport(name, "cursor:period-usage", quota)
	out.ReverseEngineered = true
	out.Entitlement = authoritativeEntitlement("cursor:billingCycleEnd", billingEnd, 0, 0, 0)
	return out, true
}

func cursorUsageSummary(token, name string) (Report, bool) {
	status, body, err := bearerJSON(http.MethodGet, CursorAPIBase+"/api/usage/summary", token, nil, cursorHeaders())
	if err != nil || status < 200 || status >= 300 {
		return Report{}, false
	}
	individual := asRecord(body["individualUsage"])
	plan := asRecord(nilSafe(individual, "plan"))
	if plan == nil {
		return Report{}, false
	}
	percent, ok := asFloat(plan["totalPercentUsed"])
	if !ok {
		used, uok := asFloat(plan["used"])
		limit, lok := asFloat(plan["limit"])
		if uok && lok && limit > 0 {
			percent, ok = (used/limit)*100, true
		}
	}
	if !ok {
		return Report{}, false
	}
	quota := Quota{MonthlyPercent: ptr(percent), UpdatedAt: Now().UnixMilli()}
	out := makeReport(name, "cursor:usage-summary", quota)
	out.ReverseEngineered = true
	if ms, ok := epochMillis(body["billingCycleEnd"]); ok {
		out.Entitlement = authoritativeEntitlement("cursor:billingCycleEnd", ms, 0, 0, 0)
	}
	return out, true
}

func cursorAuthUsage(token, name string) probeResult {
	status, body, err := bearerJSON(http.MethodGet, CursorAPIBase+"/auth/usage", token, nil, cursorHeaders())
	if res := httpProbe(status, err); res.kind != probeFresh {
		return res
	}
	used, limit, ok := cursorBucket(asRecord(body["gpt-4"]))
	if !ok {
		for key, raw := range body {
			if key == "startOfMonth" || key == "billingCycleStart" {
				continue
			}
			used, limit, ok = cursorBucket(asRecord(raw))
			if ok {
				break
			}
		}
	}
	if !ok || limit <= 0 {
		return probeResult{kind: probeNone}
	}
	quota := Quota{MonthlyPercent: ptr((used / limit) * 100), UpdatedAt: Now().UnixMilli()}
	if start, ok := epochMillis(body["startOfMonth"]); ok {
		quota.MonthlyResetAt = nextMonthUTC(start)
	} else if start, ok := epochMillis(body["billingCycleStart"]); ok {
		quota.MonthlyResetAt = nextMonthUTC(start)
	}
	out := makeReport(name, "cursor:auth-usage", quota)
	out.ReverseEngineered = true
	return probeResult{kind: probeFresh, report: out}
}

func cursorBillingEnd(body, plan map[string]any) int64 {
	for _, record := range []map[string]any{body, plan} {
		if ms, ok := firstEpoch(record, "billingCycleEnd", "periodEnd"); ok {
			return ms
		}
	}
	return 0
}

func cursorBucket(row map[string]any) (float64, float64, bool) {
	used, uok := firstFinite(row, "numRequests", "used")
	limit, lok := firstFinite(row, "maxRequestUsage", "limit", "maxRequests")
	return used, limit, uok && lok
}

func nextMonthUTC(ms int64) int64 {
	start := time.UnixMilli(ms).UTC()
	return time.Date(start.Year(), start.Month()+1, start.Day(), 0, 0, 0, 0, time.UTC).UnixMilli()
}

func probeAntigravity(provider Provider) probeResult {
	token := strings.TrimSpace(provider.AccessToken)
	project := strings.TrimSpace(provider.ProjectID)
	if token == "" || project == "" || !canonicalAntigravity(provider.BaseURL) {
		return probeResult{kind: probeNone}
	}
	payload, _ := json.Marshal(map[string]string{"project": project})
	extra := make(http.Header)
	extra.Set("Content-Type", "application/json")
	extra.Set("User-Agent", "antigravity/ide/1.0.0 (aidev_client; os_type=windows; arch=amd64)")
	status, body, err := bearerJSON(http.MethodPost, AntigravityAPIBase+"/v1internal:fetchAvailableModels", token, payload, extra)
	if res := httpProbe(status, err); res.kind != probeFresh {
		return res
	}
	models := asRecord(body["models"])
	if models == nil {
		return probeResult{kind: probeNone}
	}
	found := map[string]Window{}
	for modelID, raw := range models {
		info := asRecord(raw)
		for _, quotaInfo := range antigravityQuotaEntries(info) {
			label := classifyAntigravityFamily(modelID, info, quotaInfo)
			if label == "" {
				continue
			}
			if _, exists := found[label]; exists {
				continue
			}
			percent, ok := antigravityUsedPercent(quotaInfo)
			if !ok {
				continue
			}
			win := Window{Label: label, Percent: percent}
			if ms, ok := epochMillis(quotaInfo["resetTime"]); ok {
				win.ResetAt = ms
			}
			found[label] = win
		}
	}
	var windows []Window
	for _, label := range []string{"Gem", "Cla"} {
		if win, ok := found[label]; ok {
			windows = append(windows, win)
		}
	}
	if len(windows) == 0 {
		return probeResult{kind: probeNone}
	}
	return probeResult{kind: probeFresh, report: makeReport(provider.Name, "google-antigravity:fetchAvailableModels", Quota{
		CustomWindows: windows,
		UpdatedAt:     Now().UnixMilli(),
	})}
}

func antigravityQuotaEntries(modelInfo map[string]any) []map[string]any {
	var out []map[string]any
	add := func(value any, tier string) {
		rec := asRecord(value)
		if rec == nil {
			return
		}
		if tier != "" {
			copied := map[string]any{}
			for k, v := range rec {
				copied[k] = v
			}
			copied["tier"] = tier
			rec = copied
		}
		out = append(out, rec)
	}
	if rows, ok := modelInfo["quotaInfo"].([]any); ok {
		for _, row := range rows {
			add(row, "")
		}
	} else {
		add(modelInfo["quotaInfo"], "")
	}
	if rows, ok := modelInfo["quotaInfos"].([]any); ok {
		for _, row := range rows {
			add(row, "")
		}
	}
	if byTier := asRecord(modelInfo["quotaInfoByTier"]); byTier != nil {
		for tier, value := range byTier {
			if rows, ok := value.([]any); ok {
				for _, row := range rows {
					add(row, tier)
				}
				continue
			}
			add(value, tier)
		}
	}
	return out
}

func classifyAntigravityFamily(modelID string, modelInfo, quotaInfo map[string]any) string {
	haystack := strings.ToLower(strings.Join([]string{
		modelID,
		stringValue(modelInfo["displayName"]),
		stringValue(quotaInfo["tier"]),
	}, " "))
	if strings.Contains(haystack, "gemini") {
		return "Gem"
	}
	if strings.Contains(haystack, "claude") || strings.Contains(haystack, "opus") || strings.Contains(haystack, "sonnet") || strings.Contains(haystack, "gpt-oss") || strings.Contains(haystack, "gpt_oss") {
		return "Cla"
	}
	return ""
}

func antigravityUsedPercent(quotaInfo map[string]any) (float64, bool) {
	if n, ok := asFloat(quotaInfo["remainingFraction"]); ok {
		return 100 - n*100, true
	}
	if n, ok := asFloat(quotaInfo["remainingPercentage"]); ok {
		return 100 - n*100, true
	}
	return 0, false
}
