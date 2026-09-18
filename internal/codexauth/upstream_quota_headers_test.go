package codexauth

import "testing"

func TestParseUpstreamQuotaHeadersUsesPrimaryWeeklyAndTertiaryMonthly(t *testing.T) {
	quota := ParseUpstreamQuotaHeaders(UpstreamQuotaHeaders{
		PrimaryUsedPercent: "150", SecondaryUsedPercent: "41", TertiaryUsedPercent: "12",
		PrimaryResetAt: "1700000100", SecondaryResetAt: "1700000200", TertiaryResetAt: "1700000300",
	})
	if quota == nil || quota.WeeklyPercent == nil || *quota.WeeklyPercent != 100 || quota.MonthlyPercent == nil || *quota.MonthlyPercent != 12 {
		t.Fatalf("quota=%#v", quota)
	}
	if quota.WeeklyResetAt == nil || *quota.WeeklyResetAt != 1700000100 || quota.MonthlyResetAt == nil || *quota.MonthlyResetAt != 1700000300 || quota.MonthlyIsPrimaryWindow {
		t.Fatalf("quota=%#v", quota)
	}
}

func TestParseUpstreamQuotaHeadersFallsBackToSecondaryWeekly(t *testing.T) {
	quota := ParseUpstreamQuotaHeaders(UpstreamQuotaHeaders{
		PrimaryUsedPercent: "not-a-number", PrimaryResetAt: "1700000100",
		SecondaryUsedPercent: "42", SecondaryResetAt: "1700000200",
	})
	if quota == nil || quota.WeeklyPercent == nil || *quota.WeeklyPercent != 42 || quota.WeeklyResetAt == nil || *quota.WeeklyResetAt != 1700000200 {
		t.Fatalf("quota=%#v", quota)
	}
}

func TestParseUpstreamQuotaHeadersRecognizesExplicitMonthlyPrimary(t *testing.T) {
	quota := ParseUpstreamQuotaHeaders(UpstreamQuotaHeaders{
		PrimaryUsedPercent: "33", PrimaryResetAt: "1700000100", PrimaryWindowMinutes: "40320",
		SecondaryUsedPercent: "15", SecondaryResetAt: "1700000200", SecondaryWindowMinutes: "5",
		TertiaryUsedPercent: "77", TertiaryResetAt: "1700000300",
	})
	if quota == nil || quota.MonthlyPercent == nil || *quota.MonthlyPercent != 33 || !quota.MonthlyIsPrimaryWindow || quota.MonthlyResetAt == nil || *quota.MonthlyResetAt != 1700000100 || quota.WeeklyPercent == nil || *quota.WeeklyPercent != 15 || quota.WeeklyResetAt == nil || *quota.WeeklyResetAt != 1700000200 {
		t.Fatalf("quota=%#v", quota)
	}
}

func TestParseUpstreamQuotaHeadersDoesNotInventBurstSemantics(t *testing.T) {
	quota := ParseUpstreamQuotaHeaders(UpstreamQuotaHeaders{PrimaryUsedPercent: "88", PrimaryWindowMinutes: "300", SecondaryUsedPercent: "42"})
	if quota == nil || quota.WeeklyPercent == nil || *quota.WeeklyPercent != 88 || quota.ShortPercent != nil || quota.ShortWindowSeconds != nil {
		t.Fatalf("quota=%#v", quota)
	}
}

func TestParseUpstreamQuotaHeadersRejectsResetOnlyAndMalformedValues(t *testing.T) {
	if quota := ParseUpstreamQuotaHeaders(UpstreamQuotaHeaders{PrimaryResetAt: "1700000100"}); quota != nil {
		t.Fatalf("reset-only quota=%#v", quota)
	}
	if quota := ParseUpstreamQuotaHeaders(UpstreamQuotaHeaders{PrimaryUsedPercent: "NaN", SecondaryUsedPercent: "Infinity", TertiaryUsedPercent: "12x"}); quota != nil {
		t.Fatalf("malformed quota=%#v", quota)
	}
}

func TestParseUpstreamQuotaHeadersNormalizesNegativeUsageAndReset(t *testing.T) {
	quota := ParseUpstreamQuotaHeaders(UpstreamQuotaHeaders{PrimaryUsedPercent: "-3", PrimaryResetAt: "-1"})
	if quota == nil || quota.WeeklyPercent == nil || *quota.WeeklyPercent != 0 || quota.WeeklyResetAt != nil {
		t.Fatalf("quota=%#v", quota)
	}
}
