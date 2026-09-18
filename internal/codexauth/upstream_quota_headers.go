package codexauth

import (
	"math"
	"strconv"
	"strings"
)

const upstreamMonthlyWindowMinMinutes = 28 * 24 * 60

type UpstreamQuotaHeaders struct {
	PrimaryUsedPercent     string
	SecondaryUsedPercent   string
	TertiaryUsedPercent    string
	PrimaryResetAt         string
	SecondaryResetAt       string
	TertiaryResetAt        string
	PrimaryWindowMinutes   string
	SecondaryWindowMinutes string
}

func ParseUpstreamQuotaHeaders(headers UpstreamQuotaHeaders) *QuotaReading {
	primaryPercent := normalizeQuotaHeaderPercent(headers.PrimaryUsedPercent)
	secondaryPercent := normalizeQuotaHeaderPercent(headers.SecondaryUsedPercent)
	tertiaryPercent := normalizeQuotaHeaderPercent(headers.TertiaryUsedPercent)
	primaryReset := normalizeQuotaHeaderReset(headers.PrimaryResetAt)
	secondaryReset := normalizeQuotaHeaderReset(headers.SecondaryResetAt)
	tertiaryReset := normalizeQuotaHeaderReset(headers.TertiaryResetAt)
	primaryMonthly := primaryPercent != nil && explicitMonthlyWindowMinutes(headers.PrimaryWindowMinutes)

	quota := QuotaReading{}
	if primaryMonthly {
		quota.MonthlyPercent = cloneFloat64(primaryPercent)
		quota.MonthlyResetAt = cloneFloat64(primaryReset)
		quota.MonthlyIsPrimaryWindow = true
		if secondaryPercent != nil {
			quota.WeeklyPercent = cloneFloat64(secondaryPercent)
			quota.WeeklyResetAt = cloneFloat64(secondaryReset)
		}
	} else {
		weeklyPercent := primaryPercent
		weeklyReset := primaryReset
		if weeklyPercent == nil {
			weeklyPercent = secondaryPercent
			weeklyReset = secondaryReset
		}
		if weeklyPercent != nil {
			quota.WeeklyPercent = cloneFloat64(weeklyPercent)
			quota.WeeklyResetAt = cloneFloat64(weeklyReset)
		}
	}

	if tertiaryPercent != nil && quota.MonthlyPercent == nil {
		quota.MonthlyPercent = cloneFloat64(tertiaryPercent)
		quota.MonthlyResetAt = cloneFloat64(tertiaryReset)
	}
	if quota.WeeklyPercent == nil && quota.MonthlyPercent == nil {
		return nil
	}
	return &quota
}

func normalizeQuotaHeaderPercent(value string) *float64 {
	parsed := parseFiniteQuotaHeaderNumber(value)
	if parsed == nil {
		return nil
	}
	if *parsed < 0 {
		*parsed = 0
	} else if *parsed > 100 {
		*parsed = 100
	}
	return parsed
}

func normalizeQuotaHeaderReset(value string) *float64 {
	parsed := parseFiniteQuotaHeaderNumber(value)
	if parsed == nil || *parsed < 0 {
		return nil
	}
	return parsed
}

func explicitMonthlyWindowMinutes(value string) bool {
	parsed := parseFiniteQuotaHeaderNumber(value)
	return parsed != nil && *parsed >= upstreamMonthlyWindowMinMinutes
}

func parseFiniteQuotaHeaderNumber(value string) *float64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return nil
	}
	return &parsed
}
