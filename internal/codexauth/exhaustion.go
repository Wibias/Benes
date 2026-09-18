package codexauth

import (
	"math"
	"strings"
	"time"
)

const ExhaustedUsagePercent = 100

// Unix timestamps at or above this magnitude are milliseconds.
// 1e12 ms is 2001-09-09; current second-based WHAM resets sit near 1.7e9.
const resetAtMillisecondsCutoff = 1_000_000_000_000

func IsThirtyDayOnlyPlan(plan string) bool {
	switch strings.ToLower(strings.TrimSpace(plan)) {
	case "go", "free":
		return true
	default:
		return false
	}
}

func QuotaExhausted(quota *QuotaReading, plan string) bool {
	return QuotaExhaustedAt(quota, plan, time.Now())
}

func QuotaExhaustedAt(quota *QuotaReading, plan string, now time.Time) bool {
	if quota == nil {
		return false
	}
	if now.IsZero() {
		now = time.Now()
	}
	windows := []exhaustedQuotaWindow{{quota.ShortPercent, quota.ShortResetAt}}
	if IsThirtyDayOnlyPlan(plan) {
		windows = append(windows, exhaustedQuotaWindow{quota.MonthlyPercent, quota.MonthlyResetAt})
	} else {
		windows = append(windows,
			exhaustedQuotaWindow{quota.WeeklyPercent, quota.WeeklyResetAt},
			exhaustedQuotaWindow{quota.MonthlyPercent, quota.MonthlyResetAt},
		)
	}
	for _, window := range windows {
		if windowCurrentlyExhausted(window.percent, window.resetAt, now) {
			return true
		}
	}
	return false
}

type exhaustedQuotaWindow struct {
	percent *float64
	resetAt *float64
}

func windowCurrentlyExhausted(percent *float64, resetAt *float64, now time.Time) bool {
	if percent == nil || *percent < ExhaustedUsagePercent {
		return false
	}
	reset, ok := quotaResetTime(resetAt)
	if !ok {
		return false
	}
	return now.Before(reset)
}

func quotaResetTime(resetAt *float64) (time.Time, bool) {
	if resetAt == nil || *resetAt <= 0 || math.IsNaN(*resetAt) || math.IsInf(*resetAt, 0) {
		return time.Time{}, false
	}
	unixSeconds := *resetAt
	if unixSeconds >= resetAtMillisecondsCutoff {
		unixSeconds = unixSeconds / 1000
	}
	seconds := int64(unixSeconds)
	nanos := int64(math.Round((unixSeconds - float64(seconds)) * float64(time.Second)))
	return time.Unix(seconds, nanos), true
}
