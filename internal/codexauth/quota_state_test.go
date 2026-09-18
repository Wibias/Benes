package codexauth

import (
	"testing"
	"time"
)

func TestQuotaStateMergesLongWindowEvidenceWithoutCachingBurst(t *testing.T) {
	state := NewQuotaState()
	now := time.Unix(1_700_000_000, 0)
	weekly, monthly, short, weeklyReset, monthlyReset, credits := 20.0, 40.0, 70.0, 1700000100.0, 1700000200.0, 2.0
	state.SetParsed("a", QuotaReading{
		WeeklyPercent: &weekly, WeeklyResetAt: &weeklyReset,
		MonthlyPercent: &monthly, MonthlyResetAt: &monthlyReset, MonthlyIsPrimaryWindow: true,
		ShortPercent: &short, ResetCredits: &credits,
	}, 1, now)

	nextWeekly := 25.0
	state.SetParsed("a", QuotaReading{WeeklyPercent: &nextWeekly}, 1, now.Add(time.Minute))
	got := state.Get("a")
	if got == nil || got.WeeklyPercent == nil || *got.WeeklyPercent != 25 {
		t.Fatalf("quota=%#v", got)
	}
	if got.MonthlyPercent == nil || *got.MonthlyPercent != 40 || !got.MonthlyIsPrimaryWindow {
		t.Fatalf("monthly evidence lost: %#v", got)
	}
	if got.ResetCredits == nil || *got.ResetCredits != 2 {
		t.Fatalf("credits lost: %#v", got)
	}
	if got.ShortPercent == nil || *got.ShortPercent != 70 {
		t.Fatalf("short window lost: %#v", got)
	}
}

func TestQuotaStateMonthlyOnlyClearsStaleWeeklyAndReplacesMonthlyProvenance(t *testing.T) {
	state := NewQuotaState()
	now := time.Unix(1_700_000_000, 0)
	weekly, monthly := 20.0, 40.0
	state.SetParsed("a", QuotaReading{WeeklyPercent: &weekly, MonthlyPercent: &monthly, MonthlyIsPrimaryWindow: true}, 1, now)

	tertiary := 55.0
	state.SetParsed("a", QuotaReading{MonthlyPercent: &tertiary}, 1, now.Add(time.Minute))
	got := state.Get("a")
	if got == nil || got.WeeklyPercent != nil || got.MonthlyPercent == nil || *got.MonthlyPercent != 55 {
		t.Fatalf("quota=%#v", got)
	}
	if got.MonthlyIsPrimaryWindow {
		t.Fatalf("stale primary provenance retained: %#v", got)
	}
}

func TestQuotaStateCreditsOnlyPreservesLongWindowUsage(t *testing.T) {
	state := NewQuotaState()
	now := time.Unix(1_700_000_000, 0)
	weekly, monthly, short := 20.0, 40.0, 65.0
	state.SetParsed("a", QuotaReading{WeeklyPercent: &weekly, MonthlyPercent: &monthly, ShortPercent: &short}, 1, now)
	credits := 3.0
	state.SetParsed("a", QuotaReading{ResetCredits: &credits}, 1, now.Add(time.Minute))
	got := state.Get("a")
	if got == nil || got.WeeklyPercent == nil || *got.WeeklyPercent != 20 || got.MonthlyPercent == nil || *got.MonthlyPercent != 40 {
		t.Fatalf("quota=%#v", got)
	}
	if got.ShortPercent == nil || *got.ShortPercent != 65 || got.ResetCredits == nil || *got.ResetCredits != 3 {
		t.Fatalf("quota=%#v", got)
	}
}

func TestQuotaStateSelectionSnapshotsKeepLegacyLongWindowUsageScore(t *testing.T) {
	state := NewQuotaState()
	now := time.Unix(1_700_000_000, 0)
	weekly, monthly, short := 20.0, 30.0, 100.0
	reading := QuotaReading{WeeklyPercent: &weekly, MonthlyPercent: &monthly, ShortPercent: &short}
	state.SetParsed("a", reading, 1, now)

	snapshots := state.SelectionSnapshots()
	q := snapshots["a"]
	if q == nil || q.WeeklyPercent == nil || *q.WeeklyPercent != 20 || q.MonthlyPercent == nil || *q.MonthlyPercent != 30 {
		t.Fatalf("snapshot=%#v", q)
	}
	if q.ShortPercent == nil || *q.ShortPercent != 100 {
		t.Fatalf("snapshot short=%#v", q)
	}
	if score := ComputeQuotaUsageScore(q, "plus"); score != 100 {
		t.Fatalf("score=%v", score)
	}
	if !IsQuotaReadingExhausted(&reading, "plus") {
		t.Fatal("100% burst window must remain immediate exhaustion evidence")
	}
	if got := state.Get("a"); got == nil || got.ShortPercent == nil || *got.ShortPercent != 100 {
		t.Fatalf("shared cache dropped short window: %#v", got)
	}
}

func TestQuotaStateReconcileFencesRemovedAccountsButAllowsLiveOlderWriter(t *testing.T) {
	state := NewQuotaState()
	now := time.Unix(1_700_000_000, 0)
	v := 10.0
	state.SetParsed("removed", QuotaReading{WeeklyPercent: &v}, 1, now)
	state.SetParsed("live", QuotaReading{WeeklyPercent: &v}, 1, now)
	if removed := state.Reconcile(2, map[string]struct{}{"live": {}}); removed != 1 {
		t.Fatalf("removed=%d", removed)
	}

	staleRemoved := 90.0
	state.SetParsed("removed", QuotaReading{WeeklyPercent: &staleRemoved}, 1, now.Add(time.Minute))
	if state.Get("removed") != nil {
		t.Fatalf("removed account resurrected: %#v", state.Get("removed"))
	}
	staleLive := 25.0
	state.SetParsed("live", QuotaReading{WeeklyPercent: &staleLive}, 1, now.Add(time.Minute))
	if got := state.Get("live"); got == nil || got.WeeklyPercent == nil || *got.WeeklyPercent != 25 {
		t.Fatalf("live older writer rejected: %#v", got)
	}
}

func TestQuotaStateGetAndSelectionSnapshotsAreDefensiveCopies(t *testing.T) {
	state := NewQuotaState()
	now := time.Unix(1_700_000_000, 0)
	v := 15.0
	state.SetParsed("a", QuotaReading{WeeklyPercent: &v}, 1, now)

	got := state.Get("a")
	*got.WeeklyPercent = 99
	snapshots := state.SelectionSnapshots()
	*snapshots["a"].WeeklyPercent = 88
	again := state.Get("a")
	if again == nil || again.WeeklyPercent == nil || *again.WeeklyPercent != 15 {
		t.Fatalf("state mutated through copy: %#v", again)
	}
}
