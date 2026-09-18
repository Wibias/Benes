package codexauth

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

const (
	whamWeeklyWindowMinSeconds  = 24 * 60 * 60
	whamMonthlyWindowMinSeconds = 28 * 24 * 60 * 60
)

type QuotaReading struct {
	WeeklyPercent          *float64
	MonthlyPercent         *float64
	ShortPercent           *float64
	WeeklyResetAt          *float64
	MonthlyResetAt         *float64
	ShortResetAt           *float64
	ShortWindowSeconds     *float64
	ResetCredits           *float64
	MonthlyIsPrimaryWindow bool
}

type WHAMQuotaResult struct {
	Quota *QuotaReading
	Plan  string
}

type whamUsageWindow struct {
	UsedPercent        json.RawMessage `json:"used_percent"`
	ResetAt            json.RawMessage `json:"reset_at"`
	LimitWindowSeconds json.RawMessage `json:"limit_window_seconds"`
}

type whamRateLimit struct {
	Primary   *whamUsageWindow `json:"primary_window"`
	Secondary *whamUsageWindow `json:"secondary_window"`
	Tertiary  *whamUsageWindow `json:"tertiary_window"`
}

type whamUsageEnvelope struct {
	PlanType     json.RawMessage `json:"plan_type"`
	RateLimit    *whamRateLimit  `json:"rate_limit"`
	ResetCredits *struct {
		AvailableCount json.RawMessage `json:"available_count"`
	} `json:"rate_limit_reset_credits"`
}

func ParseWHAMUsage(body []byte, configuredPlan string) (WHAMQuotaResult, error) {
	var data whamUsageEnvelope
	if err := json.Unmarshal(body, &data); err != nil {
		return WHAMQuotaResult{}, fmt.Errorf("decode Codex WHAM usage: %w", err)
	}

	plan := configuredPlan
	if parsed, ok := rawNonEmptyString(data.PlanType); ok {
		plan = parsed
	}

	var primary, secondary, tertiary *whamUsageWindow
	if data.RateLimit != nil {
		primary = data.RateLimit.Primary
		secondary = data.RateLimit.Secondary
		tertiary = data.RateLimit.Tertiary
	}
	primaryPercent := windowPercent(primary)
	secondaryPercent := windowPercent(secondary)
	tertiaryPercent := windowPercent(tertiary)
	primaryReset := windowReset(primary)
	secondaryReset := windowReset(secondary)
	tertiaryReset := windowReset(tertiary)
	primaryDuration := windowDuration(primary)
	primaryIsShort := primaryDuration != nil && *primaryDuration > 0 && *primaryDuration < whamWeeklyWindowMinSeconds
	primaryIsMonthly := primaryDuration != nil && *primaryDuration >= whamMonthlyWindowMinSeconds

	reading := QuotaReading{}
	if primaryIsShort && primaryPercent != nil {
		reading.ShortPercent = cloneFloat64(primaryPercent)
		reading.ShortResetAt = cloneFloat64(primaryReset)
		reading.ShortWindowSeconds = cloneFloat64(primaryDuration)
	}

	weeklyPercent := primaryPercent
	weeklyReset := primaryReset
	if primaryIsShort {
		weeklyPercent = nil
		weeklyReset = nil
	}
	if primaryIsMonthly {
		weeklyPercent = secondaryPercent
		weeklyReset = secondaryReset
	} else if weeklyPercent == nil {
		weeklyPercent = secondaryPercent
		weeklyReset = secondaryReset
	}

	monthlyPercent := tertiaryPercent
	monthlyReset := tertiaryReset
	if primaryIsMonthly {
		monthlyPercent = primaryPercent
		monthlyReset = primaryReset
		if monthlyPercent == nil {
			monthlyPercent = tertiaryPercent
			monthlyReset = tertiaryReset
		}
	}

	if isThirtyDayOnlyPlan(plan) {
		if monthlyPercent != nil {
			reading.MonthlyPercent = cloneFloat64(monthlyPercent)
			reading.MonthlyResetAt = cloneFloat64(monthlyReset)
		}
	} else {
		if weeklyPercent != nil {
			reading.WeeklyPercent = cloneFloat64(weeklyPercent)
			reading.WeeklyResetAt = cloneFloat64(weeklyReset)
		}
		if monthlyPercent != nil {
			reading.MonthlyPercent = cloneFloat64(monthlyPercent)
			reading.MonthlyResetAt = cloneFloat64(monthlyReset)
			reading.MonthlyIsPrimaryWindow = primaryIsMonthly && primaryPercent != nil
		}
	}

	if data.ResetCredits != nil {
		reading.ResetCredits = finiteJSONNumber(data.ResetCredits.AvailableCount, false)
	}
	if !quotaReadingHasUsage(reading) && reading.ResetCredits == nil {
		return WHAMQuotaResult{Plan: plan}, nil
	}

	return WHAMQuotaResult{Quota: &reading, Plan: plan}, nil
}

func rawNonEmptyString(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", false
	}
	var value string
	if json.Unmarshal(raw, &value) != nil || strings.TrimSpace(value) == "" {
		return "", false
	}
	return value, true
}

func windowPercent(window *whamUsageWindow) *float64 {
	if window == nil {
		return nil
	}
	value := finiteJSONNumber(window.UsedPercent, true)
	if value == nil {
		return nil
	}
	if *value < 0 {
		*value = 0
	} else if *value > 100 {
		*value = 100
	}
	return value
}

func windowReset(window *whamUsageWindow) *float64 {
	if window == nil {
		return nil
	}
	value := finiteJSONNumber(window.ResetAt, true)
	if value == nil || *value < 0 {
		return nil
	}
	return value
}

func windowDuration(window *whamUsageWindow) *float64 {
	if window == nil {
		return nil
	}
	return finiteJSONNumber(window.LimitWindowSeconds, false)
}

func finiteJSONNumber(raw json.RawMessage, allowString bool) *float64 {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var number float64
	if json.Unmarshal(raw, &number) == nil && !math.IsNaN(number) && !math.IsInf(number, 0) {
		return &number
	}
	if !allowString {
		return nil
	}
	var text string
	if json.Unmarshal(raw, &text) != nil {
		return nil
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	parsed, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return nil
	}
	return &parsed
}

func quotaReadingHasLongUsage(reading QuotaReading) bool {
	return reading.WeeklyPercent != nil || reading.WeeklyResetAt != nil || reading.MonthlyPercent != nil || reading.MonthlyResetAt != nil
}

func quotaReadingHasUsage(reading QuotaReading) bool {
	return quotaReadingHasLongUsage(reading) || reading.ShortPercent != nil || reading.ShortResetAt != nil || reading.ShortWindowSeconds != nil
}
