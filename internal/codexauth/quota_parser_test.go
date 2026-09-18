package codexauth

import "testing"

func TestParseWHAMUsageSeparatesBurstWeeklyAndMonthlyWindows(t *testing.T) {
	result, err := ParseWHAMUsage([]byte(`{
		"plan_type":"k12",
		"rate_limit":{
			"primary_window":{"used_percent":88,"reset_at":1700000100,"limit_window_seconds":18000},
			"secondary_window":{"used_percent":42,"reset_at":1700000200,"limit_window_seconds":604800},
			"tertiary_window":{"used_percent":12,"reset_at":1700000300,"limit_window_seconds":2592000}
		}
	}`), "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Plan != "k12" || result.Quota == nil {
		t.Fatalf("result=%#v", result)
	}
	q := result.Quota
	if q.ShortPercent == nil || *q.ShortPercent != 88 || q.ShortWindowSeconds == nil || *q.ShortWindowSeconds != 18000 {
		t.Fatalf("short=%#v", q)
	}
	if q.WeeklyPercent == nil || *q.WeeklyPercent != 42 || q.MonthlyPercent == nil || *q.MonthlyPercent != 12 {
		t.Fatalf("long windows=%#v", q)
	}
	if q.ShortResetAt == nil || *q.ShortResetAt != 1700000100 || q.WeeklyResetAt == nil || *q.WeeklyResetAt != 1700000200 || q.MonthlyResetAt == nil || *q.MonthlyResetAt != 1700000300 {
		t.Fatalf("resets=%#v", q)
	}
	if q.MonthlyIsPrimaryWindow {
		t.Fatal("tertiary monthly window must not be primary evidence")
	}
}

func TestParseWHAMUsageRecognizesExplicitMonthlyPrimary(t *testing.T) {
	result, err := ParseWHAMUsage([]byte(`{
		"plan_type":"team",
		"rate_limit":{
			"primary_window":{"used_percent":33,"reset_at":1700000100,"limit_window_seconds":2419200},
			"secondary_window":{"used_percent":15,"reset_at":1700000200,"limit_window_seconds":604800}
		}
	}`), "")
	if err != nil {
		t.Fatal(err)
	}
	q := result.Quota
	if q == nil || q.MonthlyPercent == nil || *q.MonthlyPercent != 33 || !q.MonthlyIsPrimaryWindow {
		t.Fatalf("quota=%#v", q)
	}
	if q.WeeklyPercent == nil || *q.WeeklyPercent != 15 {
		t.Fatalf("quota=%#v", q)
	}
}

func TestParseWHAMUsageThirtyDayPlanUsesMonthlyWindow(t *testing.T) {
	result, err := ParseWHAMUsage([]byte(`{
		"plan_type":" free ",
		"rate_limit":{
			"primary_window":{"used_percent":55,"limit_window_seconds":2592000},
			"secondary_window":{"used_percent":9,"limit_window_seconds":604800}
		}
	}`), "plus")
	if err != nil {
		t.Fatal(err)
	}
	if result.Plan != " free " {
		t.Fatalf("plan=%q", result.Plan)
	}
	q := result.Quota
	if q == nil || q.MonthlyPercent == nil || *q.MonthlyPercent != 55 || q.WeeklyPercent != nil {
		t.Fatalf("quota=%#v", q)
	}
}

func TestParseWHAMUsageFallsBackToConfiguredPlanAndNormalizesNumbers(t *testing.T) {
	result, err := ParseWHAMUsage([]byte(`{
		"rate_limit":{
			"primary_window":{"used_percent":"150","reset_at":"-1"},
			"tertiary_window":{"used_percent":"-2","reset_at":"1700000300"}
		}
	}`), "plus")
	if err != nil {
		t.Fatal(err)
	}
	if result.Plan != "plus" {
		t.Fatalf("plan=%q", result.Plan)
	}
	q := result.Quota
	if q == nil || q.WeeklyPercent == nil || *q.WeeklyPercent != 100 || q.MonthlyPercent == nil || *q.MonthlyPercent != 0 {
		t.Fatalf("quota=%#v", q)
	}
	if q.WeeklyResetAt != nil || q.MonthlyResetAt == nil || *q.MonthlyResetAt != 1700000300 {
		t.Fatalf("quota=%#v", q)
	}
}

func TestParseWHAMUsageReturnsCreditsOnlyAndKeepsShortOnlyAsKnownQuota(t *testing.T) {
	credits, err := ParseWHAMUsage([]byte(`{"rate_limit_reset_credits":{"available_count":2}}`), "plus")
	if err != nil {
		t.Fatal(err)
	}
	if credits.Quota == nil || credits.Quota.ResetCredits == nil || *credits.Quota.ResetCredits != 2 {
		t.Fatalf("credits=%#v", credits)
	}

	shortOnly, err := ParseWHAMUsage([]byte(`{"rate_limit":{"primary_window":{"used_percent":100,"limit_window_seconds":18000}}}`), "plus")
	if err != nil {
		t.Fatal(err)
	}
	if shortOnly.Quota == nil || shortOnly.Quota.ShortPercent == nil || *shortOnly.Quota.ShortPercent != 100 {
		t.Fatalf("short-only quota=%#v", shortOnly.Quota)
	}
	if shortOnly.Quota.WeeklyPercent != nil || shortOnly.Quota.MonthlyPercent != nil {
		t.Fatalf("short-only leaked long windows=%#v", shortOnly.Quota)
	}

	zero, err := ParseWHAMUsage([]byte(`{"rate_limit":{"primary_window":{"used_percent":0,"limit_window_seconds":18000}}}`), "plus")
	if err != nil {
		t.Fatal(err)
	}
	if zero.Quota == nil || zero.Quota.ShortPercent == nil || *zero.Quota.ShortPercent != 0 {
		t.Fatalf("zero short-only quota=%#v", zero.Quota)
	}
}

func TestParseWHAMUsageRejectsMalformedJSON(t *testing.T) {
	if _, err := ParseWHAMUsage([]byte(`{"rate_limit":`), "plus"); err == nil {
		t.Fatal("expected malformed JSON error")
	}
}
