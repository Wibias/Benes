package storage

import (
	"encoding/json"
	"testing"
	"time"
)

func TestNormalizePolicyRejectsBadValues(t *testing.T) {
	both := int64(1)
	pct := 10
	_, err := NormalizePolicy(Policy{Schedule: "hourly", Mode: ModeQuarantine, Target: PolicyTarget{ReduceToBytes: &both, RemoveOldestPercent: &pct}})
	if err == nil {
		t.Fatal("expected rejection")
	}
	_, err = NormalizePolicy(Policy{Schedule: ScheduleDaily, Mode: "wipe", Target: PolicyTarget{RemoveOldestPercent: &pct}})
	if err == nil {
		t.Fatal("expected mode rejection")
	}
	bad := 0
	_, err = NormalizePolicy(Policy{Schedule: ScheduleDaily, Mode: ModeQuarantine, Target: PolicyTarget{RemoveOldestPercent: &bad}})
	if err == nil {
		t.Fatal("expected percent rejection")
	}
}

func TestDisabledAndManualNeverSchedule(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	pct := 25
	disabled := Policy{Enabled: false, Schedule: ScheduleDaily, Mode: ModeQuarantine, Target: PolicyTarget{RemoveOldestPercent: &pct}}
	if ShouldRunScheduled(disabled, now, time.UTC, true) {
		t.Fatal("disabled ran")
	}
	manual := Policy{Enabled: true, Schedule: ScheduleManual, Mode: ModeQuarantine, Target: PolicyTarget{RemoveOldestPercent: &pct}}
	if ShouldRunScheduled(manual, now, time.UTC, true) {
		t.Fatal("manual ran")
	}
}

func TestDailyAndWeeklyNextRun(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	pct := 25
	daily := Policy{Enabled: true, Schedule: ScheduleDaily, Mode: ModeQuarantine, Target: PolicyTarget{RemoveOldestPercent: &pct}, Job: &PolicyJob{FinishedAt: now.UnixMilli()}}
	next := NextRunAt(daily, now, time.UTC)
	if next == nil {
		t.Fatal("daily next")
	}
	got := time.UnixMilli(*next).UTC()
	want := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("daily next=%s want=%s", got, want)
	}
	weekly := daily
	weekly.Schedule = ScheduleWeekly
	next = NextRunAt(weekly, now, time.UTC)
	got = time.UnixMilli(*next).UTC()
	want = time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("weekly next=%s want=%s", got, want)
	}
}

func TestStartupRunsOncePerCallNotWhenDisabled(t *testing.T) {
	now := time.Now()
	pct := 25
	policy := Policy{Enabled: true, Schedule: ScheduleStartup, Mode: ModeQuarantine, Target: PolicyTarget{RemoveOldestPercent: &pct}}
	if !ShouldRunScheduled(policy, now, time.UTC, true) {
		t.Fatal("startup should run")
	}
	if ShouldRunScheduled(policy, now, time.UTC, false) {
		t.Fatal("ticker should not treat as startup")
	}
}

func TestClearStaleRunning(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	policy := DefaultPolicy()
	policy.Job = &PolicyJob{Status: JobRunning, StartedAt: now.Add(-time.Hour).UnixMilli()}
	cleared := ClearStaleRunning(policy, now)
	if cleared.Job.Status != JobIdle || cleared.Job.LastError != CodeRestoreWorkerAborted {
		t.Fatalf("cleared=%+v", cleared.Job)
	}
}

func TestPolicyRunDisabledAndUnderThreshold(t *testing.T) {
	home := t.TempDir()
	engine := NewEngine()
	engine.Now = func() time.Time { return time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC) }
	engine.Location = time.UTC
	policy := DefaultPolicy()
	_, outcome, err := engine.RunPolicy(home, policy)
	if err != nil || outcome.Skipped != SkipDisabled {
		t.Fatalf("disabled=%+v err=%v", outcome, err)
	}
	policy.Enabled = true
	policy.Trigger.ArchivedBytesOver = 100
	writeArchived(t, home, "a.jsonl", "aa", time.Now())
	_, outcome, err = engine.RunPolicy(home, policy)
	if err != nil || outcome.Skipped != SkipUnderThreshold {
		t.Fatalf("under=%+v err=%v", outcome, err)
	}
}

func TestPolicyRunRemovesOldestPercent(t *testing.T) {
	home := t.TempDir()
	engine := NewEngine()
	writeArchived(t, home, "a.jsonl", "aaaa", time.Unix(100, 0))
	writeArchived(t, home, "b.jsonl", "bbbb", time.Unix(200, 0))
	pct := 50
	policy := Policy{
		Enabled:  true,
		Trigger:  PolicyTrigger{ArchivedBytesOver: 1},
		Target:   PolicyTarget{RemoveOldestPercent: &pct},
		Schedule: ScheduleManual,
		Mode:     ModeQuarantine,
	}
	_, outcome, err := engine.RunPolicy(home, policy)
	if err != nil || !outcome.OK || outcome.Removed != 1 {
		t.Fatalf("outcome=%+v err=%v", outcome, err)
	}
}

func TestParsePolicyRoundTrip(t *testing.T) {
	raw := json.RawMessage(`{"enabled":true,"trigger":{"archivedBytesOver":10},"target":{"removeOldestPercent":25},"schedule":"daily","mode":"quarantine"}`)
	policy, err := ParsePolicy(raw)
	if err != nil || !policy.Enabled || policy.Schedule != ScheduleDaily {
		t.Fatalf("policy=%+v err=%v", policy, err)
	}
}

func TestFirstDailyRunDueTimeIsStable(t *testing.T) {
	loc := time.UTC
	pct := 25
	enabledAt := time.Date(2026, 9, 4, 12, 0, 0, 0, loc)
	policy := Policy{Enabled: true, Schedule: ScheduleDaily, Mode: ModeQuarantine, Target: PolicyTarget{RemoveOldestPercent: &pct}}
	policy.NextRun = NextRunAt(policy, enabledAt, loc)
	if policy.NextRun == nil {
		t.Fatal("daily next")
	}
	want := time.Date(2026, 9, 5, 0, 0, 0, 0, loc)
	if !time.UnixMilli(*policy.NextRun).UTC().Equal(want) {
		t.Fatalf("first daily=%s", time.UnixMilli(*policy.NextRun).UTC())
	}
	for _, later := range []time.Time{
		time.Date(2026, 9, 4, 13, 0, 0, 0, loc),
		time.Date(2026, 9, 4, 18, 0, 0, 0, loc),
		time.Date(2026, 9, 4, 23, 59, 0, 0, loc),
	} {
		if ShouldRunScheduled(policy, later, loc, false) {
			t.Fatalf("ran early at %s", later)
		}
		kept := PreserveNextRun(policy, policy, later, loc)
		if kept == nil || *kept != *policy.NextRun {
			t.Fatalf("read moved nextRun at %s", later)
		}
	}
	due := time.Date(2026, 9, 5, 0, 1, 0, 0, loc)
	if !ShouldRunScheduled(policy, due, loc, false) {
		t.Fatal("not due after midnight")
	}
	restarted := policy
	if !ShouldRunScheduled(restarted, due, loc, false) {
		t.Fatal("restart lost due time")
	}
	stamped := stampJob(policy, due, loc, JobOutcome{OK: true, Mode: ModeQuarantine})
	if stamped.NextRun == nil || *stamped.NextRun == *policy.NextRun {
		t.Fatalf("completion did not advance nextRun: %+v", stamped.NextRun)
	}
	next := time.UnixMilli(*stamped.NextRun).UTC()
	if !next.Equal(time.Date(2026, 9, 6, 0, 0, 0, 0, loc)) {
		t.Fatalf("next after completion=%s", next)
	}
	if ShouldRunScheduled(stamped, due.Add(time.Minute), loc, false) {
		t.Fatal("tight retry after completion")
	}
}

func TestFirstWeeklyRunDueTimeIsStable(t *testing.T) {
	loc := time.UTC
	pct := 25
	enabledAt := time.Date(2026, 9, 4, 12, 0, 0, 0, loc)
	policy := Policy{Enabled: true, Schedule: ScheduleWeekly, Mode: ModeQuarantine, Target: PolicyTarget{RemoveOldestPercent: &pct}}
	policy.NextRun = NextRunAt(policy, enabledAt, loc)
	want := time.Date(2026, 9, 11, 0, 0, 0, 0, loc)
	if policy.NextRun == nil || !time.UnixMilli(*policy.NextRun).UTC().Equal(want) {
		t.Fatalf("first weekly=%v", policy.NextRun)
	}
	later := time.Date(2026, 9, 10, 23, 59, 0, 0, loc)
	if ShouldRunScheduled(policy, later, loc, false) {
		t.Fatal("weekly ran early")
	}
	kept := PreserveNextRun(policy, policy, later, loc)
	if kept == nil || *kept != *policy.NextRun {
		t.Fatal("weekly nextRun moved")
	}
	due := time.Date(2026, 9, 11, 0, 1, 0, 0, loc)
	if !ShouldRunScheduled(policy, due, loc, false) {
		t.Fatal("weekly not due")
	}
}

func TestNoTightRetryAfterCompletedDaily(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	pct := 25
	policy := Policy{
		Enabled:  true,
		Schedule: ScheduleDaily,
		Mode:     ModeQuarantine,
		Target:   PolicyTarget{RemoveOldestPercent: &pct},
		Job:      &PolicyJob{FinishedAt: now.UnixMilli()},
	}
	if ShouldRunScheduled(policy, now, time.UTC, false) {
		t.Fatal("daily retried immediately")
	}
}
