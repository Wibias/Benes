package codexauth

import (
	"testing"
	"time"
)

func TestPickResetWindowSoonestAndLatest(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	quotas := map[string]*QuotaSnapshot{
		"soon":  resetSnapshot(now, 40, 1_700_000_100),
		"later": resetSnapshot(now, 40, 1_700_000_500),
	}
	ids := []string{"later", "soon"}
	quotaOf := func(id string) *QuotaSnapshot { return quotas[id] }
	planOf := func(string) string { return "plus" }
	head := func(string) bool { return true }
	if got := PickResetWindow(ids, ResetOrderSoonest, now, quotaOf, planOf, head); got != "soon" {
		t.Fatalf("soonest=%q", got)
	}
	if got := PickResetWindow(ids, ResetOrderLatest, now, quotaOf, planOf, head); got != "later" {
		t.Fatalf("latest=%q", got)
	}
}

func TestPickResetWindowEqualResetsAreDeterministic(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	quotas := map[string]*QuotaSnapshot{
		"b": resetSnapshot(now, 20, 1_700_000_200),
		"a": resetSnapshot(now, 20, 1_700_000_200),
	}
	ids := []string{"b", "a"}
	quotaOf := func(id string) *QuotaSnapshot { return quotas[id] }
	if got := PickResetWindow(ids, ResetOrderSoonest, now, quotaOf, func(string) string { return "plus" }, func(string) bool { return true }); got != "a" {
		t.Fatalf("equal resets=%q", got)
	}
}

func TestPickResetWindowExhaustedDoesNotBeatHeadroom(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	quotas := map[string]*QuotaSnapshot{
		"drained": resetSnapshot(now, 95, 1_700_000_050),
		"ok":      resetSnapshot(now, 20, 1_700_000_400),
	}
	head := func(id string) bool { return id == "ok" }
	got := PickResetWindow([]string{"drained", "ok"}, ResetOrderSoonest, now, func(id string) *QuotaSnapshot { return quotas[id] }, func(string) string { return "plus" }, head)
	if got != "ok" {
		t.Fatalf("exhausted won=%q", got)
	}
}

func TestPickResetWindowMissingOrStaleFallsBackToQuota(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	quotas := map[string]*QuotaSnapshot{
		"fresh-high": resetSnapshot(now, 70, 1_700_000_100),
		"stale-low":  resetSnapshot(now.Add(-ResetEvidenceMaxAge-time.Second), 10, 1_700_000_050),
	}
	got := PickResetWindow([]string{"fresh-high", "stale-low"}, ResetOrderSoonest, now, func(id string) *QuotaSnapshot { return quotas[id] }, func(string) string { return "plus" }, func(string) bool { return true })
	if got != "stale-low" {
		t.Fatalf("stale mixed set should fall back to lowest usage, got=%q", got)
	}
	quotas["missing"] = &QuotaSnapshot{WeeklyPercent: float64Ptr(5), UpdatedAt: now}
	got = PickResetWindow([]string{"fresh-high", "missing"}, ResetOrderSoonest, now, func(id string) *QuotaSnapshot { return quotas[id] }, func(string) string { return "plus" }, func(string) bool { return true })
	if got != "missing" {
		t.Fatalf("missing reset should fall back to lowest usage, got=%q", got)
	}
}

func TestGoverningResetUsesMonthlyWindowForThirtyDayPlans(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	q := &QuotaSnapshot{
		WeeklyPercent:  float64Ptr(10),
		WeeklyResetAt:  float64Ptr(1_700_000_100),
		MonthlyPercent: float64Ptr(40),
		MonthlyResetAt: float64Ptr(1_700_000_900),
		UpdatedAt:      now,
	}
	reset, ok := GoverningReset(q, "go", now)
	if !ok || reset != 1_700_000_900 {
		t.Fatalf("go governing=%v ok=%v", reset, ok)
	}
	plus, ok := GoverningReset(q, "plus", now)
	if !ok || plus != 1_700_000_900 {
		t.Fatalf("plus bottleneck=%v ok=%v", plus, ok)
	}
}

func TestGoverningResetAmbiguousMatchingPercentsFallClosed(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	q := &QuotaSnapshot{
		WeeklyPercent:  float64Ptr(50),
		WeeklyResetAt:  float64Ptr(1_700_000_100),
		MonthlyPercent: float64Ptr(50),
		MonthlyResetAt: float64Ptr(1_700_000_900),
		UpdatedAt:      now,
	}
	if _, ok := GoverningReset(q, "plus", now); ok {
		t.Fatal("ambiguous dual bottleneck must not invent an ordering")
	}
}

func TestPoolSelectorResetWindowPreservesAffinityAndPriority(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	selector := NewPoolSelector(PoolSelectorDependencies{})
	input := poolSelectorFixture(now)
	input.Strategy = PoolStrategyResetWindow
	input.ResetOrder = ResetOrderSoonest
	input.ThreadID = "thread"
	input.Accounts.Accounts = []ManagedAccount{{ID: "a", Plan: "plus"}, {ID: "b", Plan: "plus"}, {ID: "low", Plan: "plus"}}
	input.Accounts.Priorities = map[string]int{"a": 10, "b": 10, "low": 0}
	input.Credentials = credentialSnapshotFor("a", "b", "low")
	input.Quotas = map[string]*QuotaSnapshot{
		"a":   resetSnapshot(now, 40, 1_700_000_400),
		"b":   resetSnapshot(now, 40, 1_700_000_100),
		"low": resetSnapshot(now, 1, 1_700_000_050),
	}
	got := selector.Resolve(input)
	if got.AccountID != "b" || got.Reason != PoolSelectionResetWindow {
		t.Fatalf("initial reset-window=%#v", got)
	}
	if !selector.Affinity.Bind("thread", "a", AffinityScopeLegacy, now, input.Credentials) {
		t.Fatal("bind")
	}
	got = selector.Resolve(input)
	if got.AccountID != "a" || got.Reason != PoolSelectionAffinity {
		t.Fatalf("affinity broken=%#v", got)
	}
}

func TestPoolSelectorResetWindowSkipsCooldownAndReauth(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	selector := NewPoolSelector(PoolSelectorDependencies{})
	input := poolSelectorFixture(now)
	input.Strategy = PoolStrategyResetWindow
	input.Accounts.Accounts = []ManagedAccount{{ID: "soon", Plan: "plus"}, {ID: "later", Plan: "plus"}}
	input.Credentials = credentialSnapshotFor("soon", "later")
	input.Quotas = map[string]*QuotaSnapshot{
		"soon":  resetSnapshot(now, 20, 1_700_000_100),
		"later": resetSnapshot(now, 20, 1_700_000_400),
	}
	selector.Health.SetHardCooldown("soon", now.Add(time.Minute), CooldownSourceDefault)
	got := selector.Resolve(input)
	if got.AccountID != "later" {
		t.Fatalf("cooldown=%#v", got)
	}
	selector.Health.ClearHardCooldown("soon")
	selector.Reauth.Mark("soon", 1)
	got = selector.Resolve(input)
	if got.AccountID != "later" {
		t.Fatalf("reauth=%#v", got)
	}
}

func resetSnapshot(updated time.Time, weekly, resetAt float64) *QuotaSnapshot {
	return &QuotaSnapshot{
		WeeklyPercent: float64Ptr(weekly),
		WeeklyResetAt: float64Ptr(resetAt),
		UpdatedAt:     updated,
	}
}
