package quota

import (
	"net/http"
	"strconv"
	"strings"
)

func ptr(n float64) *float64 { return &n }

func httpProbe(status int, err error) probeResult {
	if err != nil || status == 0 {
		return probeResult{kind: probeTransient}
	}
	if status < 200 || status >= 300 {
		return probeResult{kind: classifyHTTP(status)}
	}
	return probeResult{kind: probeFresh}
}

func canonicalMoonshot(base string) bool {
	n := normalizeBaseURL(base)
	return n == MoonshotAI || n == MoonshotCN
}

func canonicalVenice(base string) bool { return normalizeBaseURL(base) == VeniceBase }

func canonicalCline(base string) bool {
	n := normalizeBaseURL(base)
	return n == ClineBase || n == ClineBase+"/api/v1"
}

func canonicalMinimax(base string) bool {
	n := normalizeBaseURL(base)
	return n == MinimaxIO || n == MinimaxCN
}

func canonicalDeepInfra(base string) bool {
	n := normalizeBaseURL(base)
	return n == DeepInfraBase || n == DeepInfraBase+"/v1/openai"
}

func canonicalNeuralwatt(base string) bool { return normalizeBaseURL(base) == NeuralwattBase }

func canonicalSynthetic(base string) bool {
	n := normalizeBaseURL(base)
	return n == SyntheticBase || n == "https://api.synthetic.new/openai/v1"
}

func canonicalOpenCodeGo(base string) bool { return normalizeBaseURL(base) == OpenCodeGoBase }

func probeMoonshot(provider Provider) probeResult {
	apiKey := resolveEnvValue(provider.APIKey)
	if apiKey == "" {
		return probeResult{kind: probeNone}
	}
	host := MoonshotAI
	if strings.HasPrefix(normalizeBaseURL(provider.BaseURL), "https://api.moonshot.cn") {
		host = MoonshotCN
	}
	status, body, err := bearerGet(host+"/users/me/balance", apiKey)
	if res := httpProbe(status, err); res.kind != probeFresh {
		return res
	}
	data := asRecord(body["data"])
	if data == nil {
		data = body
	}
	available, ok := asFloat(data["available_balance"])
	if !ok || available < 0 {
		return probeResult{kind: probeTransient}
	}
	china := host == MoonshotCN
	money := func(n float64) string {
		if china {
			return "¥" + formatMoney(n)
		}
		return "$" + formatMoney(n)
	}
	unit := "USD"
	if china {
		unit = "CNY"
	}
	label := "Balance (" + money(available) + " " + unit + " available)"
	if voucher, vok := asFloat(data["voucher_balance"]); vok {
		if _, cok := asFloat(data["cash_balance"]); cok {
			label = "Balance (" + money(available) + " " + unit + " available, " + money(voucher) + " voucher)"
		}
	}
	now := Now().UnixMilli()
	return probeResult{kind: probeFresh, report: makeReport(provider.Name, "moonshot:balance", Quota{
		CustomWindows: []Window{{Label: label, Percent: 0}},
		UpdatedAt:     now,
	})}
}

func probeVenice(provider Provider) probeResult {
	apiKey := resolveEnvValue(provider.APIKey)
	if apiKey == "" {
		return probeResult{kind: probeNone}
	}
	status, body, err := bearerGet(VeniceBase+"/billing/balance", apiKey)
	if res := httpProbe(status, err); res.kind != probeFresh {
		return res
	}
	data := asRecord(body["data"])
	if data == nil {
		data = body
	}
	if data == nil {
		return probeResult{kind: probeTransient}
	}
	diem, diemOK := asFloat(data["balance"])
	usd, usdOK := asFloat(data["balance_usd"])
	if !diemOK && !usdOK {
		return probeResult{kind: probeTransient}
	}
	label := ""
	if diemOK {
		label = "DIEM balance (" + formatInt(diem) + ")"
	} else {
		label = "USD balance ($" + formatMoney(usd) + ")"
	}
	now := Now().UnixMilli()
	percent := 0.0
	if allocated, aok := asFloat(data["diem_epoch_allocated"]); aok && allocated > 0 {
		if used, uok := asFloat(data["diem_epoch_used"]); uok {
			percent = (used / allocated) * 100
		}
	}
	return probeResult{kind: probeFresh, report: makeReport(provider.Name, "venice:billing-balance", Quota{
		CustomWindows: []Window{{Label: label, Percent: percent}},
		UpdatedAt:     now,
	})}
}

func probeCline(provider Provider) probeResult {
	apiKey := resolveEnvValue(provider.APIKey)
	if apiKey == "" {
		return probeResult{kind: probeNone}
	}
	status, body, err := bearerGet(ClineBase+"/api/v1/users/me/plan/usage-limits", apiKey)
	if status == http.StatusNotFound {
		return probeResult{kind: probeNone}
	}
	if res := httpProbe(status, err); res.kind != probeFresh {
		return res
	}
	data := asRecord(body["data"])
	if data == nil {
		data = body
	}
	rawLimits, _ := data["limits"].([]any)
	quota := Quota{UpdatedAt: Now().UnixMilli()}
	windows := 0
	for _, raw := range rawLimits {
		row := asRecord(raw)
		if row == nil {
			continue
		}
		percent, ok := asFloat(row["percentUsed"])
		if !ok {
			continue
		}
		switch stringValue(row["type"]) {
		case "five_hour":
			quota.FiveHourPercent = ptr(percent)
			windows++
		case "weekly":
			quota.WeeklyPercent = ptr(percent)
			windows++
		case "monthly":
			quota.MonthlyPercent = ptr(percent)
			windows++
		}
	}
	if windows == 0 {
		return probeResult{kind: probeNone}
	}
	return probeResult{kind: probeFresh, report: makeReport(provider.Name, "cline:plan-usage-limits", quota)}
}

func probeMinimax(provider Provider) probeResult {
	apiKey := resolveEnvValue(provider.APIKey)
	if apiKey == "" {
		return probeResult{kind: probeNone}
	}
	url := "https://www.minimax.io/v1/token_plan/remains"
	if strings.HasPrefix(normalizeBaseURL(provider.BaseURL), "https://api.minimaxi.com") {
		url = "https://api.minimaxi.com/v1/token_plan/remains"
	}
	status, body, err := bearerGet(url, apiKey)
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
	remains, ok := asFloat(data["remains_time"])
	if !ok {
		remains, ok = asFloat(data["remainsTime"])
	}
	if !ok || remains < 0 {
		return probeResult{kind: probeTransient}
	}
	total, tok := asFloat(data["total_time"])
	if !tok {
		total, tok = asFloat(data["plan_duration_ms"])
	}
	if !tok {
		total, tok = asFloat(data["total_duration_ms"])
	}
	if !tok || total <= 0 {
		return probeResult{kind: probeTerminal}
	}
	hours := int(remains / 3600000)
	label := "Token Plan remaining (" + formatHours(hours) + "h)"
	consumed := total - remains
	if consumed < 0 {
		consumed = 0
	}
	now := Now().UnixMilli()
	return probeResult{kind: probeFresh, report: makeReport(provider.Name, "minimax:token-plan-remains", Quota{
		CustomWindows: []Window{{Label: label, Percent: (consumed / total) * 100}},
		UpdatedAt:     now,
	})}
}

func probeDeepInfra(provider Provider) probeResult {
	apiKey := resolveEnvValue(provider.APIKey)
	if apiKey == "" {
		return probeResult{kind: probeNone}
	}
	status, body, err := bearerGet(DeepInfraBase+"/payment/checklist?compute_owed=true", apiKey)
	if res := httpProbe(status, err); res.kind != probeFresh {
		return res
	}
	data := asRecord(body["data"])
	if data == nil {
		data = body
	}
	now := Now().UnixMilli()
	if stripe, ok := asFloat(data["stripe_balance"]); ok {
		available := -stripe
		if available < 0 {
			available = 0
		}
		label := "Prepaid balance ($" + formatMoney(available) + ")"
		return probeResult{kind: probeFresh, report: makeReport(provider.Name, "deepinfra:billing-checklist", Quota{
			CustomWindows: []Window{{Label: label, Percent: 0}},
			UpdatedAt:     now,
		})}
	}
	return probeResult{kind: probeTransient}
}

func probeNeuralwatt(provider Provider) probeResult {
	apiKey := resolveEnvValue(provider.APIKey)
	if apiKey == "" {
		return probeResult{kind: probeNone}
	}
	status, body, err := bearerGet(NeuralwattBase+"/quota", apiKey)
	if res := httpProbe(status, err); res.kind != probeFresh {
		return res
	}
	data := asRecord(body["data"])
	if data == nil {
		data = body
	}
	quota := Quota{UpdatedAt: Now().UnixMilli()}
	windows := 0
	if sub := asRecord(data["subscription"]); sub != nil {
		used, uok := asFloat(sub["kwh_used"])
		included, iok := asFloat(sub["kwh_included"])
		if uok && iok && included > 0 {
			quota.FiveHourPercent = ptr((used / included) * 100)
			windows++
		}
	}
	if bal := asRecord(data["balance"]); bal != nil {
		total, tok := asFloat(bal["total_credits_usd"])
		remain, rok := asFloat(bal["credits_remaining_usd"])
		if tok && rok && total > 0 {
			used := total - remain
			if used < 0 {
				used = 0
			}
			quota.CustomWindows = append(quota.CustomWindows, Window{Label: "Prepaid credits", Percent: (used / total) * 100})
			windows++
		}
	}
	if windows == 0 {
		return probeResult{kind: probeNone}
	}
	return probeResult{kind: probeFresh, report: makeReport(provider.Name, "neuralwatt:quota", quota)}
}

func probeSynthetic(provider Provider) probeResult {
	apiKey := resolveEnvValue(provider.APIKey)
	if apiKey == "" {
		return probeResult{kind: probeNone}
	}
	status, body, err := bearerGet(SyntheticBase+"/quotas", apiKey)
	if res := httpProbe(status, err); res.kind != probeFresh {
		return res
	}
	data := asRecord(body["data"])
	if data == nil {
		data = body
	}
	quota := Quota{UpdatedAt: Now().UnixMilli()}
	windows := 0
	percentAt := func(key string) (float64, bool) {
		if p, ok := asFloat(data[key]); ok {
			return p, true
		}
		nested := asRecord(data["quota"])
		if nested == nil {
			nested = asRecord(data["quotas"])
		}
		if nested == nil {
			return 0, false
		}
		return asFloat(nested[key])
	}
	if p, ok := percentAt("rollingFiveHourLimit"); ok {
		quota.FiveHourPercent = ptr(p)
		windows++
	}
	if p, ok := percentAt("weeklyTokenLimit"); ok {
		quota.WeeklyPercent = ptr(p)
		windows++
	}
	if search := asRecord(data["search"]); search != nil {
		if p, ok := asFloat(search["hourly"]); ok {
			quota.CustomWindows = append(quota.CustomWindows, Window{Label: "Search hourly", Percent: p})
			windows++
		}
	}
	if windows == 0 {
		return probeResult{kind: probeNone}
	}
	return probeResult{kind: probeFresh, report: makeReport(provider.Name, "synthetic:quotas", quota)}
}

func probeOpenCodeGo(provider Provider) probeResult {
	apiKey := resolveEnvValue(provider.APIKey)
	if apiKey == "" {
		return probeResult{kind: probeNone}
	}
	status, body, err := bearerGet(OpenCodeGoBase+"/usage", apiKey)
	if res := httpProbe(status, err); res.kind != probeFresh {
		return res
	}
	usage := asRecord(body["usage"])
	if usage == nil {
		return probeResult{kind: probeTransient}
	}
	quota := Quota{UpdatedAt: Now().UnixMilli()}
	windows := 0
	parse := func(raw any) (float64, bool) {
		rec := asRecord(raw)
		if rec == nil {
			return asFloat(raw)
		}
		if p, ok := asFloat(rec["percent"]); ok {
			return p, true
		}
		used, uok := asFloat(rec["used"])
		limit, lok := asFloat(rec["limit"])
		if uok && lok && limit > 0 {
			return (used / limit) * 100, true
		}
		return 0, false
	}
	if p, ok := parse(usage["rolling"]); ok {
		quota.FiveHourPercent = ptr(p)
		windows++
	}
	if p, ok := parse(usage["weekly"]); ok {
		quota.WeeklyPercent = ptr(p)
		windows++
	}
	if p, ok := parse(usage["monthly"]); ok {
		quota.MonthlyPercent = ptr(p)
		windows++
	}
	if windows == 0 {
		return probeResult{kind: probeNone}
	}
	return probeResult{kind: probeFresh, report: makeReport(provider.Name, "opencode-go:usage", quota)}
}

func formatHours(n int) string { return strconv.Itoa(n) }

func formatInt(n float64) string {
	s := formatMoney(n)
	if len(s) > 3 && s[len(s)-3:] == ".00" {
		return s[:len(s)-3]
	}
	return s
}
