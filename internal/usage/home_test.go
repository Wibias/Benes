package usage

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/usageledger"
)

func TestSummarizeHomeQuerySpansSegmentsAndKeepsNewestWindow(t *testing.T) {
	now := time.Date(2026, 9, 4, 15, 0, 0, 0, time.UTC).UnixMilli()
	home := t.TempDir()
	row := func(ts int64, model string) string {
		return usageLine(ts, "p", model)
	}
	l, err := usageledger.OpenWithTarget(home, int64(len(row(now, "m"))+1))
	if err != nil {
		t.Fatal(err)
	}
	for i, model := range []string{"old", "mid", "keep", "today"} {
		raw := row(now-int64(3-i)*60_000, model)
		if err := l.Append([]byte(raw)); err != nil {
			t.Fatal(err)
		}
	}
	st, err := l.Status()
	if err != nil {
		t.Fatal(err)
	}
	if st.SealedCount < 2 {
		t.Fatalf("need segmented fixture: %+v", st)
	}
	limit := st.LogicalBytes / 2
	got, err := summarizeHomeQueryLimited(home, Query{Range: RangeAll, Now: now}, NewTable(), ReadLimits{MaxBytes: limit, MaxLine: 1 << 10})
	if err != nil {
		t.Fatal(err)
	}
	if !got.HistoryTruncated || got.TruncatedPrefixBytes <= 0 {
		t.Fatalf("completeness=%+v", got)
	}
	if modelPresent(got, "old") {
		t.Fatal("oldest prefix leaked")
	}
	if !modelPresent(got, "today") {
		t.Fatal("newest row missing")
	}
	if got.SnapshotWindowEnd == nil || *got.SnapshotWindowEnd != now {
		t.Fatalf("end=%v", got.SnapshotWindowEnd)
	}
}

func TestSummarizeHomeQueryCompleteBelowBound(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC).UnixMilli()
	home := t.TempDir()
	l, err := usageledger.OpenWithTarget(home, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Append([]byte(usageLine(now-1000, "old", "m1"))); err != nil {
		t.Fatal(err)
	}
	if err := l.Append([]byte(usageLine(now, "new", "m2"))); err != nil {
		t.Fatal(err)
	}
	got, err := summarizeHomeQueryLimited(home, Query{Range: RangeAll, Now: now}, NewTable(), defaultReadLimits())
	if err != nil {
		t.Fatal(err)
	}
	if got.HistoryTruncated || got.TruncatedPrefixBytes != 0 {
		t.Fatalf("%+v", got)
	}
	if got.Summary.Requests != 2 {
		t.Fatalf("requests=%d", got.Summary.Requests)
	}
}

func TestSummarizeHomeQueryIgnoresIncompleteActiveTail(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC).UnixMilli()
	home := t.TempDir()
	l, err := usageledger.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Append([]byte(usageLine(now-1, "a", "complete"))); err != nil {
		t.Fatal(err)
	}
	active := filepath.Join(home, usageledger.DirName, usageledger.ActiveFile)
	file, err := os.OpenFile(active, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(usageLine(now, "b", "pending")); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := summarizeHomeQueryLimited(home, Query{Range: RangeAll, Now: now}, NewTable(), defaultReadLimits())
	if err != nil {
		t.Fatal(err)
	}
	if got.Summary.Requests != 1 || !modelPresent(got, "complete") || modelPresent(got, "pending") {
		t.Fatalf("%+v", got.Summary)
	}
}

func TestSummarizeHomeQueryFiltersAfterRetainedWindow(t *testing.T) {
	now := time.Date(2026, 9, 4, 16, 0, 0, 0, time.UTC)
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, usageledger.LegacyFile), []byte(usageLine(now.AddDate(0, 0, -40).UnixMilli(), "old", "ancient")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	l, err := usageledger.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Append([]byte(usageLine(now.UnixMilli(), "new", "current"))); err != nil {
		t.Fatal(err)
	}
	got, err := summarizeHomeQueryLimited(home, Query{Range: RangeToday, Now: now.UnixMilli(), Location: time.UTC}, NewTable(), ReadLimits{MaxBytes: 1 << 20, MaxLine: 1 << 10})
	if err != nil {
		t.Fatal(err)
	}
	if got.Summary.Requests != 1 || !modelPresent(got, "current") {
		t.Fatalf("%+v", got.Summary)
	}
}

func TestSummarizeHomeQueryLegacyPlusSegmentedDoesNotDoubleCount(t *testing.T) {
	now := time.Now().UnixMilli()
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, usageledger.LegacyFile), []byte(usageLine(now-2, "legacy", "old")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	l, err := usageledger.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Append([]byte(usageLine(now, "seg", "new"))); err != nil {
		t.Fatal(err)
	}
	got, err := summarizeHomeQueryLimited(home, Query{Range: RangeAll, Now: now}, NewTable(), defaultReadLimits())
	if err != nil {
		t.Fatal(err)
	}
	if got.Summary.Requests != 2 {
		t.Fatalf("requests=%d", got.Summary.Requests)
	}
}

func TestSummarizeHomeQueryEmptyWindowDoesNotFabricateTimestamps(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, usageledger.LegacyFile), []byte("{}\nnot-json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := summarizeHomeQueryLimited(home, Query{Range: RangeAll, Now: 1}, NewTable(), defaultReadLimits())
	if err != nil {
		t.Fatal(err)
	}
	if got.SnapshotWindowStart != nil || got.SnapshotWindowEnd != nil {
		t.Fatalf("fabricated window start=%v end=%v", got.SnapshotWindowStart, got.SnapshotWindowEnd)
	}
}
