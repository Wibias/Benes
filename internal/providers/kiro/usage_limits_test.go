package kiro

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseUsageLimitsPrefersPrecisionAndResourceTypeNotIndex(t *testing.T) {
	raw := []byte(`{
		"usageBreakdownList": [
			{"resourceType":"CREDIT","currentUsage":1,"usageLimit":1,"currentUsageWithPrecision":1,"usageLimitWithPrecision":1},
			{"resourceType":"AGENTIC_REQUEST","currentUsage":695,"usageLimit":1000,"currentUsageWithPrecision":695.17,"usageLimitWithPrecision":1000,"nextDateReset":1700000000000,
			 "freeTrialInfo":{"currentUsageWithPrecision":10,"usageLimitWithPrecision":50}}
		],
		"userInfo":{"email":"hidden@example.com","userId":"uid-secret"},
		"overageConfiguration":{"overageStatus":"DISABLED"}
	}`)
	limits, err := ParseUsageLimits(raw)
	if err != nil {
		t.Fatal(err)
	}
	want := 695.17 / 1000 * 100
	if limits.MonthlyPercent != want {
		t.Fatalf("monthly=%v want=%v (must use precision, not integer rounding)", limits.MonthlyPercent, want)
	}
	if limits.MonthlyResetAt != 1700000000000 {
		t.Fatalf("reset=%d", limits.MonthlyResetAt)
	}
	if !limits.HasTrial || limits.TrialPercent != 20 {
		t.Fatalf("trial=%#v", limits)
	}
	if limits.OverageEnabled {
		t.Fatalf("disabled overage marked enabled: %#v", limits)
	}
	encoded, err := json.Marshal(limits)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "hidden@example.com") || strings.Contains(string(encoded), "uid-secret") {
		t.Fatalf("leaked userInfo: %s", encoded)
	}
}

func TestParseUsageLimitsMissingListIsUnknownNotZero(t *testing.T) {
	limits, err := ParseUsageLimits([]byte(`{"userInfo":{"email":"a@b.c"},"overageConfiguration":{"overageStatus":"ENABLED"}}`))
	if err == nil {
		t.Fatalf("accepted unknown shape as %#v", limits)
	}
}

func TestParseUsageLimitsOverageEnabledAtCapIsUsable(t *testing.T) {
	limits, err := ParseUsageLimits([]byte(`{
		"usageBreakdownList":[{"resourceType":"AGENTIC_REQUEST","currentUsageWithPrecision":50,"usageLimitWithPrecision":50}],
		"overageConfiguration":{"overageStatus":"ENABLED"}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if limits.MonthlyPercent != 100 {
		t.Fatalf("monthly=%v", limits.MonthlyPercent)
	}
	if !limits.OverageEnabled {
		t.Fatal("overage-enabled at cap was not marked usable")
	}
}

func TestManagementURLAllowlistsRegion(t *testing.T) {
	if _, err := ManagementURL("evil"); err == nil {
		t.Fatal("accepted evil region")
	}
	got, err := ManagementURL("eu-west-1")
	if err != nil || got != "https://management.eu-west-1.kiro.dev/" {
		t.Fatalf("got=%q err=%v", got, err)
	}
}
