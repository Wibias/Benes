package kiro

import (
	"encoding/json"
	"fmt"
)

var ErrUnknownUsageLimits = fmt.Errorf("Kiro usage limits payload is unrecognised")

type UsageLimits struct {
	MonthlyPercent float64
	MonthlyResetAt int64
	TrialPercent   float64
	TrialResetAt   int64
	HasTrial       bool
	OverageEnabled bool
}

func ParseUsageLimits(raw []byte) (UsageLimits, error) {
	var payload struct {
		UsageBreakdownList   []map[string]any `json:"usageBreakdownList"`
		NextDateReset        any              `json:"nextDateReset"`
		OverageConfiguration struct {
			OverageStatus string `json:"overageStatus"`
		} `json:"overageConfiguration"`
	}
	if json.Unmarshal(raw, &payload) != nil || len(payload.UsageBreakdownList) == 0 {
		return UsageLimits{}, ErrUnknownUsageLimits
	}
	row, ok := selectUsageBreakdown(payload.UsageBreakdownList)
	if !ok {
		return UsageLimits{}, ErrUnknownUsageLimits
	}
	used, limit, ok := usageAmounts(row)
	if !ok {
		return UsageLimits{}, ErrUnknownUsageLimits
	}
	out := UsageLimits{
		MonthlyPercent: used / limit * 100,
		OverageEnabled: payload.OverageConfiguration.OverageStatus == "ENABLED",
	}
	if reset, ok := epochMillis(row["nextDateReset"]); ok {
		out.MonthlyResetAt = reset
	} else if reset, ok := epochMillis(payload.NextDateReset); ok {
		out.MonthlyResetAt = reset
	}
	if trial, ok := trialAmounts(row["freeTrialInfo"]); ok {
		out.HasTrial = true
		out.TrialPercent = trial.percent
		out.TrialResetAt = trial.resetAt
	}
	return out, nil
}

func selectUsageBreakdown(list []map[string]any) (map[string]any, bool) {
	var credit map[string]any
	for _, row := range list {
		switch stringsTrim(row["resourceType"]) {
		case "AGENTIC_REQUEST":
			return row, true
		case "CREDIT":
			if credit == nil {
				credit = row
			}
		}
	}
	if credit != nil {
		return credit, true
	}
	return nil, false
}

func usageAmounts(row map[string]any) (used, limit float64, ok bool) {
	used, uok := numberPreferPrecision(row, "currentUsageWithPrecision", "currentUsage")
	limit, lok := numberPreferPrecision(row, "usageLimitWithPrecision", "usageLimit")
	if !uok || !lok || used < 0 || limit <= 0 {
		return 0, 0, false
	}
	return used, limit, true
}

type trialAmountsResult struct {
	percent float64
	resetAt int64
}

func trialAmounts(raw any) (trialAmountsResult, bool) {
	row, _ := raw.(map[string]any)
	if row == nil {
		return trialAmountsResult{}, false
	}
	used, limit, ok := usageAmounts(row)
	if !ok {
		return trialAmountsResult{}, false
	}
	out := trialAmountsResult{percent: used / limit * 100}
	if reset, ok := epochMillis(row["nextDateReset"]); ok {
		out.resetAt = reset
	}
	return out, true
}

func numberPreferPrecision(row map[string]any, precise, fallback string) (float64, bool) {
	if n, ok := asFinite(row[precise]); ok {
		return n, true
	}
	return asFinite(row[fallback])
}

func asFinite(v any) (float64, bool) {
	n, ok := v.(float64)
	if !ok || n != n {
		return 0, false
	}
	return n, true
}

func epochMillis(v any) (int64, bool) {
	n, ok := asFinite(v)
	if !ok || n <= 0 {
		return 0, false
	}
	return int64(n), true
}

func stringsTrim(v any) string {
	s, _ := v.(string)
	return s
}
