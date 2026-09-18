package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDailyWeeklySurviveBerlinDST(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skip(err)
	}
	pct := 25
	policy := Policy{Enabled: true, Schedule: ScheduleDaily, Mode: ModeQuarantine, Target: PolicyTarget{RemoveOldestPercent: &pct}}

	spring := time.Date(2026, 3, 28, 12, 0, 0, 0, loc)
	policy.Job = &PolicyJob{FinishedAt: spring.UnixMilli()}
	next := NextRunAt(policy, spring, loc)
	if next == nil {
		t.Fatal("daily next")
	}
	got := time.UnixMilli(*next).In(loc)
	if got.Hour() != 0 || got.Minute() != 0 || got.Day() != 29 {
		t.Fatalf("spring daily=%s", got)
	}

	afterJump := time.Date(2026, 3, 29, 12, 0, 0, 0, loc)
	policy.Job.FinishedAt = afterJump.UnixMilli()
	next = NextRunAt(policy, afterJump, loc)
	got = time.UnixMilli(*next).In(loc)
	if got.Year() != 2026 || got.Month() != 3 || got.Day() != 30 || got.Hour() != 0 || got.Minute() != 0 {
		t.Fatalf("spring-forward daily=%s", got)
	}

	fall := time.Date(2026, 10, 24, 12, 0, 0, 0, loc)
	policy.Job.FinishedAt = fall.UnixMilli()
	next = NextRunAt(policy, fall, loc)
	got = time.UnixMilli(*next).In(loc)
	if got.Hour() != 0 || got.Minute() != 0 || got.Day() != 25 {
		t.Fatalf("fall daily=%s", got)
	}

	afterBack := time.Date(2026, 10, 25, 12, 0, 0, 0, loc)
	policy.Job.FinishedAt = afterBack.UnixMilli()
	next = NextRunAt(policy, afterBack, loc)
	got = time.UnixMilli(*next).In(loc)
	if got.Year() != 2026 || got.Month() != 10 || got.Day() != 26 || got.Hour() != 0 || got.Minute() != 0 {
		t.Fatalf("fall-back daily=%s", got)
	}

	policy.Schedule = ScheduleWeekly
	policy.Job.FinishedAt = spring.UnixMilli()
	next = NextRunAt(policy, spring, loc)
	got = time.UnixMilli(*next).In(loc)
	if got.Hour() != 0 || got.Minute() != 0 {
		t.Fatalf("spring weekly not midnight: %s", got)
	}
	wantWeek := time.Date(2026, 4, 4, 0, 0, 0, 0, loc)
	if got.Year() != wantWeek.Year() || got.Month() != wantWeek.Month() || got.Day() != wantWeek.Day() {
		t.Fatalf("spring weekly=%s want=%s", got, wantWeek)
	}
	policy.Job.FinishedAt = fall.UnixMilli()
	next = NextRunAt(policy, fall, loc)
	got = time.UnixMilli(*next).In(loc)
	if got.Hour() != 0 || got.Minute() != 0 {
		t.Fatalf("fall weekly not midnight: %s", got)
	}
	wantFallWeek := time.Date(2026, 10, 31, 0, 0, 0, 0, loc)
	if got.Year() != wantFallWeek.Year() || got.Month() != wantFallWeek.Month() || got.Day() != wantFallWeek.Day() {
		t.Fatalf("fall weekly=%s want=%s", got, wantFallWeek)
	}
}

func TestHardlinkAcrossBucketsCountedOncePhysically(t *testing.T) {
	home := t.TempDir()
	arch := writeArchived(t, home, "a.jsonl", "shared", time.Now())
	if err := osMkdir(home+"/sessions", 0o700); err != nil {
		t.Fatal(err)
	}
	live := home + "/sessions/a.jsonl"
	if err := osLink(arch, live); err != nil {
		t.Skip("hardlinks not available")
	}
	report, err := Scan(home)
	if err != nil {
		t.Fatal(err)
	}
	if report.Total.PhysicalBytes != 6 {
		t.Fatalf("physicalBytes=%d total=%+v", report.Total.PhysicalBytes, report.Total)
	}
	if report.Total.Bytes != 12 {
		t.Fatalf("logical bytes=%d", report.Total.Bytes)
	}
}

func TestHardlinkWithinBucketCountedOncePhysically(t *testing.T) {
	home := t.TempDir()
	a := writeArchived(t, home, "a.jsonl", "shared", time.Now())
	b := filepath.Join(home, "archived_sessions", "b.jsonl")
	if err := osLink(a, b); err != nil {
		t.Skip("hardlinks not available")
	}
	report, err := Scan(home)
	if err != nil {
		t.Fatal(err)
	}
	if report.Total.Bytes != 12 {
		t.Fatalf("logical bytes=%d", report.Total.Bytes)
	}
	if report.Total.PhysicalBytes != 6 {
		t.Fatalf("physicalBytes=%d", report.Total.PhysicalBytes)
	}
	for _, bucket := range report.Buckets {
		if bucket.Key == BucketArchivedSessions && bucket.PhysicalBytes != 6 {
			t.Fatalf("archived physicalBytes=%d", bucket.PhysicalBytes)
		}
	}
}

func TestPermanentDeleteLastLinkFreesBytes(t *testing.T) {
	home := t.TempDir()
	a := writeArchived(t, home, "a.jsonl", "shared", time.Unix(100, 0))
	b := filepath.Join(home, "archived_sessions", "b.jsonl")
	if err := osLink(a, b); err != nil {
		t.Skip("hardlinks not available")
	}
	preview, err := PreviewPercent(home, 100)
	if err != nil {
		t.Fatal(err)
	}
	result, err := ExecuteCleanup(home, CleanupRequest{Percent: 100, Mode: ModePermanent, Digest: preview.Digest})
	if err != nil || !result.OK {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if result.FreedBytes != 6 {
		t.Fatalf("last-link freedBytes=%d", result.FreedBytes)
	}
	if result.RemovedArchivedBytes != 6 {
		t.Fatalf("removedArchivedBytes=%d", result.RemovedArchivedBytes)
	}
}

func TestPermanentDeleteSurvivingLinkDoesNotFreeBytes(t *testing.T) {
	home := t.TempDir()
	arch := writeArchived(t, home, "a.jsonl", "shared", time.Unix(100, 0))
	if err := osMkdir(home+"/sessions", 0o700); err != nil {
		t.Fatal(err)
	}
	live := home + "/sessions/a.jsonl"
	if err := osLink(arch, live); err != nil {
		t.Skip("hardlinks not available")
	}
	preview, err := PreviewPercent(home, 100)
	if err != nil {
		t.Fatal(err)
	}
	result, err := ExecuteCleanup(home, CleanupRequest{Percent: 100, Mode: ModePermanent, Digest: preview.Digest})
	if err != nil || !result.OK {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if result.FreedBytes != 0 {
		t.Fatalf("surviving link claimed freedBytes=%d", result.FreedBytes)
	}
	if result.RemovedArchivedBytes != 6 {
		t.Fatalf("removedArchivedBytes=%d", result.RemovedArchivedBytes)
	}
	if _, err := os.Lstat(live); err != nil {
		t.Fatal("surviving hardlink was removed")
	}
}

func osMkdir(path string, mode uint32) error {
	return os.MkdirAll(path, os.FileMode(mode))
}

func osLink(old, new string) error {
	return os.Link(old, new)
}
