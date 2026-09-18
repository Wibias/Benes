package codexauth

import (
	"math"
	"testing"
)

func TestNormalizePoolStrategyAndStickyLimit(t *testing.T) {
	for raw, want := range map[string]PoolStrategy{
		"quota":        PoolStrategyQuota,
		"round-robin":  PoolStrategyRoundRobin,
		"fill-first":   PoolStrategyFillFirst,
		"reset-window": PoolStrategyResetWindow,
		"":             PoolStrategyQuota,
		"unknown":      PoolStrategyQuota,
	} {
		if got := NormalizePoolStrategy(raw); got != want {
			t.Fatalf("strategy %q=%q want=%q", raw, got, want)
		}
	}
	for raw, want := range map[int]int{0: 1, 1: 1, 2: 2, 100: 100, 101: 1, -1: 1} {
		if got := NormalizeStickyLimit(raw); got != want {
			t.Fatalf("sticky %d=%d want=%d", raw, got, want)
		}
	}
}

func TestComputeQuotaUsageScoreMatchesPlanWindows(t *testing.T) {
	if got := ComputeQuotaUsageScore(nil, "plus"); got != UnknownUsageScore {
		t.Fatalf("nil score=%v", got)
	}
	quota := &QuotaSnapshot{WeeklyPercent: float64Ptr(40), MonthlyPercent: float64Ptr(70)}
	if got := ComputeQuotaUsageScore(quota, "plus"); got != 70 {
		t.Fatalf("plus score=%v", got)
	}
	if got := ComputeQuotaUsageScore(quota, "free"); got != 70 {
		t.Fatalf("free score=%v", got)
	}
	if got := ComputeQuotaUsageScore(&QuotaSnapshot{WeeklyPercent: float64Ptr(40)}, "go"); got != UnknownUsageScore {
		t.Fatalf("go missing monthly score=%v", got)
	}
	if got := ComputeQuotaUsageScore(&QuotaSnapshot{WeeklyPercent: float64Ptr(math.NaN()), MonthlyPercent: float64Ptr(20)}, "team"); got != 20 {
		t.Fatalf("finite filter score=%v", got)
	}
}

func TestQuotaHeadroomTreatsUnknownAsUsableAndThresholdAsExclusive(t *testing.T) {
	if !QuotaHasHeadroom(nil, "plus", 80) {
		t.Fatal("unknown quota was treated as drained")
	}
	if !QuotaHasHeadroom(&QuotaSnapshot{WeeklyPercent: float64Ptr(100)}, "plus", 0) {
		t.Fatal("disabled threshold did not allow quota")
	}
	if QuotaHasHeadroom(&QuotaSnapshot{WeeklyPercent: float64Ptr(80)}, "plus", 80) {
		t.Fatal("usage equal to threshold had headroom")
	}
	if !QuotaHasHeadroom(&QuotaSnapshot{WeeklyPercent: float64Ptr(79.9)}, "plus", 80) {
		t.Fatal("usage below threshold lacked headroom")
	}
}

func TestPickLowestUsagePreservesFirstTie(t *testing.T) {
	quotas := map[string]*QuotaSnapshot{
		"main": {WeeklyPercent: float64Ptr(30)},
		"a":    {WeeklyPercent: float64Ptr(10)},
		"b":    {WeeklyPercent: float64Ptr(10)},
		"c":    nil,
	}
	picked := PickLowestUsage(
		[]string{"main", "a", "b", "c"},
		func(id string) *QuotaSnapshot { return quotas[id] },
		func(string) string { return "plus" },
	)
	if picked != "a" {
		t.Fatalf("picked=%q", picked)
	}
	if got := PickLowestUsage(nil, func(string) *QuotaSnapshot { return nil }, func(string) string { return "" }); got != "" {
		t.Fatalf("empty picked=%q", got)
	}
}

func TestPickFillFirstKeepsHealthyActiveThenAdvancesStableOrder(t *testing.T) {
	headroom := map[string]bool{"a": false, "b": true, "c": true}
	has := func(id string) bool { return headroom[id] }
	eligible := []string{"c", "a", "b"}
	allConfigured := []string{"c", "a", "b"}

	if got := PickFillFirst(eligible, allConfigured, "b", has); got != "b" {
		t.Fatalf("healthy active=%q", got)
	}
	if got := PickFillFirst(eligible, allConfigured, "a", has); got != "b" {
		t.Fatalf("advance from drained a=%q", got)
	}
	if got := PickFillFirst(eligible, allConfigured, "missing", has); got != "b" {
		t.Fatalf("missing active=%q", got)
	}

	allDrained := func(string) bool { return false }
	if got := PickFillFirst([]string{"c", "a"}, []string{"a", "b", "c"}, "a", allDrained); got != "c" {
		t.Fatalf("all-drained fallback=%q", got)
	}
}

func TestRotationStateRoundRobinAndStickyBehavior(t *testing.T) {
	state := NewRotationState()
	ids := []string{"a", "b", "c"}
	sequence := make([]string, 0, 6)
	for i := 0; i < 6; i++ {
		picked := state.PickRoundRobin("codex", ids, 1)
		sequence = append(sequence, picked)
		state.NoteSuccess("codex", picked, 1)
	}
	assertStringsEqual(t, sequence, []string{"a", "b", "c", "a", "b", "c"})

	state.Clear("codex")
	sequence = sequence[:0]
	for i := 0; i < 6; i++ {
		picked := state.PickRoundRobin("codex", ids, 2)
		sequence = append(sequence, picked)
		state.NoteSuccess("codex", picked, 2)
	}
	assertStringsEqual(t, sequence, []string{"a", "a", "b", "b", "c", "c"})
}

func TestRotationStatePeekSeedFailureAndReconcile(t *testing.T) {
	state := NewRotationState()
	ids := []string{"a", "b", "c"}
	if got := state.PeekRoundRobin("codex", ids, 1); got != "a" {
		t.Fatalf("first peek=%q", got)
	}
	if got := state.PeekRoundRobin("codex", ids, 1); got != "a" {
		t.Fatalf("second peek mutated state=%q", got)
	}
	if got := state.PickRoundRobin("codex", ids, 1); got != "a" {
		t.Fatalf("pick after peek=%q", got)
	}
	state.NoteSuccess("codex", "a", 1)

	state.Seed("codex", "c")
	if got := state.PickRoundRobin("codex", ids, 3); got != "c" {
		t.Fatalf("seeded pick=%q", got)
	}
	state.NoteFailure("codex", "c")
	if got := state.PickRoundRobin("codex", ids, 3); got == "c" {
		t.Fatalf("failure left c sticky: %q", got)
	}

	state.Seed("codex", "a")
	state.PickRoundRobin("codex:spark", []string{"b", "c"}, 2)
	removed := state.Reconcile(5, map[string]struct{}{"b": {}, "c": {}})
	if removed == 0 {
		t.Fatal("reconcile removed nothing")
	}
	if removed := state.Reconcile(4, map[string]struct{}{}); removed != 0 {
		t.Fatalf("older reconcile removed=%d", removed)
	}
}

func float64Ptr(value float64) *float64 { return &value }
