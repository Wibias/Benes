package usage

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestReadFileBelowBoundIsComplete(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC).UnixMilli()
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	writeUsageLines(t, path,
		usageLine(now-1000, "old", "m1"),
		usageLine(now, "new", "m2"),
	)
	got := mustSummarizeLimited(t, path, Query{Range: RangeAll, Now: now}, ReadLimits{MaxBytes: 1 << 20, MaxLine: 1 << 10})
	if got.HistoryTruncated {
		t.Fatalf("complete file reported truncated: %+v", got)
	}
	if got.TruncatedPrefixBytes != 0 {
		t.Fatalf("prefix=%d", got.TruncatedPrefixBytes)
	}
	if got.Summary.Requests != 2 {
		t.Fatalf("requests=%d", got.Summary.Requests)
	}
	if got.SnapshotWindowStart == nil || *got.SnapshotWindowStart != now-1000 {
		t.Fatalf("start=%v", got.SnapshotWindowStart)
	}
	if got.SnapshotWindowEnd == nil || *got.SnapshotWindowEnd != now {
		t.Fatalf("end=%v", got.SnapshotWindowEnd)
	}
}

func TestReadFileAboveBoundKeepsNewestRows(t *testing.T) {
	now := time.Date(2026, 9, 4, 15, 0, 0, 0, time.UTC).UnixMilli()
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	old := usageLine(now-86_400_000, "old", "stale")
	mid := usageLine(now-60_000, "mid", "keep")
	newest := usageLine(now, "new", "today")
	writeUsageLines(t, path, old, mid, newest)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	limit := int64(len(mid) + 1 + len(newest) + 1)
	if info.Size() <= limit {
		t.Fatalf("fixture too small to truncate size=%d limit=%d", info.Size(), limit)
	}
	got := mustSummarizeLimited(t, path, Query{Range: RangeAll, Now: now}, ReadLimits{MaxBytes: limit, MaxLine: 1 << 10})
	if !got.HistoryTruncated {
		t.Fatal("expected historyTruncated")
	}
	wantPrefix := info.Size() - limit
	if got.TruncatedPrefixBytes != wantPrefix {
		t.Fatalf("prefix=%d want=%d size=%d", got.TruncatedPrefixBytes, wantPrefix, info.Size())
	}
	if got.Summary.Requests != 2 {
		t.Fatalf("retained=%d body=%+v", got.Summary.Requests, got.Summary)
	}
	if got.Summary.Requests > 0 && modelPresent(got, "stale") {
		t.Fatal("oldest prefix row leaked into retained window")
	}
	if !modelPresent(got, "today") {
		t.Fatal("newest row missing")
	}
	if got.SnapshotWindowStart == nil || *got.SnapshotWindowStart != now-60_000 {
		t.Fatalf("start=%v", got.SnapshotWindowStart)
	}
	if got.SnapshotWindowEnd == nil || *got.SnapshotWindowEnd != now {
		t.Fatalf("end=%v", got.SnapshotWindowEnd)
	}
}

func TestTodayRangeSeesCurrentRowsWhenHistoryExceedsBound(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 9, 4, 16, 0, 0, 0, loc)
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	old := usageLine(now.AddDate(0, 0, -40).UnixMilli(), "old", "ancient")
	today := usageLine(now.UnixMilli(), "new", "current")
	writeUsageLines(t, path, old, today)
	limit := int64(len(today) + 8)
	got := mustSummarizeLimited(t, path, Query{Range: RangeToday, Now: now.UnixMilli(), Location: loc}, ReadLimits{MaxBytes: limit, MaxLine: 1 << 10})
	if !got.HistoryTruncated {
		t.Fatal("expected truncation")
	}
	if got.Summary.Requests != 1 {
		t.Fatalf("today requests=%d", got.Summary.Requests)
	}
	if !modelPresent(got, "current") {
		t.Fatal("today row omitted after bound")
	}
}

func TestTruncatedPrefixBytesIsExactFirstRetainedRecordOffset(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC).UnixMilli()
	head := usageLine(now-3, "a", "head")
	mid := usageLine(now-2, "b", "mid")
	tail := usageLine(now, "c", "tail")
	body := head + "\n" + mid + "\n" + tail + "\n"

	t.Run("aligned after newline", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "usage.jsonl")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		start := int64(len(head) + 1)
		got := mustSnapshot(t, path, ReadLimits{MaxBytes: int64(len(body)) - start, MaxLine: 1 << 10})
		if !got.HistoryTruncated {
			t.Fatal("expected truncation")
		}
		if got.TruncatedPrefixBytes != start {
			t.Fatalf("aligned prefix=%d want=%d", got.TruncatedPrefixBytes, start)
		}
		assertPrefixEqualsFirstRetainedOffset(t, body, got)
	})

	t.Run("midway through a row", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "usage.jsonl")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		start := int64(len(head) / 2)
		got := mustSnapshot(t, path, ReadLimits{MaxBytes: int64(len(body)) - start, MaxLine: 1 << 10})
		if !got.HistoryTruncated {
			t.Fatal("expected truncation")
		}
		want := int64(len(head) + 1)
		if got.TruncatedPrefixBytes != want {
			t.Fatalf("midway prefix=%d want=%d", got.TruncatedPrefixBytes, want)
		}
		assertPrefixEqualsFirstRetainedOffset(t, body, got)
	})

	t.Run("seek exactly on newline", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "usage.jsonl")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		start := int64(len(head))
		got := mustSnapshot(t, path, ReadLimits{MaxBytes: int64(len(body)) - start, MaxLine: 1 << 10})
		if !got.HistoryTruncated {
			t.Fatal("expected truncation")
		}
		want := start + 1
		if got.TruncatedPrefixBytes != want {
			t.Fatalf("newline prefix=%d want=%d", got.TruncatedPrefixBytes, want)
		}
		assertPrefixEqualsFirstRetainedOffset(t, body, got)
	})

	t.Run("large discarded partial row", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "usage.jsonl")
		huge := usageLine(now-1, "h", strings.Repeat("x", 800))
		full := huge + "\n" + tail + "\n"
		if err := os.WriteFile(path, []byte(full), 0o600); err != nil {
			t.Fatal(err)
		}
		start := int64(40)
		got := mustSnapshot(t, path, ReadLimits{MaxBytes: int64(len(full)) - start, MaxLine: 1 << 12})
		if !got.HistoryTruncated {
			t.Fatal("expected truncation")
		}
		want := int64(len(huge) + 1)
		if got.TruncatedPrefixBytes != want {
			t.Fatalf("large partial prefix=%d want=%d", got.TruncatedPrefixBytes, want)
		}
		assertPrefixEqualsFirstRetainedOffset(t, full, got)
	})

	t.Run("no complete row after seek", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "usage.jsonl")
		partial := `{"timestamp":` + strconv.FormatInt(now, 10) + `,"provider":"z","model":"partial"`
		full := head + "\n" + partial
		if err := os.WriteFile(path, []byte(full), 0o600); err != nil {
			t.Fatal(err)
		}
		start := int64(len(head) / 2)
		got := mustSnapshot(t, path, ReadLimits{MaxBytes: int64(len(full)) - start, MaxLine: 1 << 10})
		if !got.HistoryTruncated {
			t.Fatal("expected truncation")
		}
		if len(got.Entries) != 0 {
			t.Fatalf("retained incomplete rows: %+v", got.Entries)
		}
		want := int64(len(head) + 1)
		if got.TruncatedPrefixBytes != want {
			t.Fatalf("no-complete prefix=%d want=%d", got.TruncatedPrefixBytes, want)
		}
	})
}

func TestPartialLeadingRowAfterTailSeekIsDiscarded(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC).UnixMilli()
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	head := usageLine(now-2, "a", "head")
	tail := usageLine(now, "b", "tail")
	body := head + "\n" + tail + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	seekInto := int64(len(head) / 2)
	limit := int64(len(body)) - seekInto
	got := mustSummarizeLimited(t, path, Query{Range: RangeAll, Now: now}, ReadLimits{MaxBytes: limit, MaxLine: 1 << 10})
	if !got.HistoryTruncated {
		t.Fatal("expected truncation")
	}
	if got.Summary.Requests != 1 || !modelPresent(got, "tail") || modelPresent(got, "head") {
		t.Fatalf("partial leading row not discarded: %+v", got.Summary)
	}
}

func TestMalformedRowDoesNotHideLaterRows(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC).UnixMilli()
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	writeUsageLines(t, path,
		usageLine(now-2, "a", "first"),
		"{not-json",
		"",
		usageLine(now, "b", "later"),
	)
	got := mustSummarizeLimited(t, path, Query{Range: RangeAll, Now: now}, ReadLimits{MaxBytes: 1 << 20, MaxLine: 1 << 10})
	if got.Summary.Requests != 2 {
		t.Fatalf("requests=%d", got.Summary.Requests)
	}
	if !modelPresent(got, "later") {
		t.Fatal("later valid row hidden")
	}
}

func TestOversizedRowDoesNotHideLaterRows(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC).UnixMilli()
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	huge := `{"timestamp":` + strconv.FormatInt(now-1, 10) + `,"provider":"x","model":"` + strings.Repeat("m", 200) + `","usageStatus":"reported"}`
	writeUsageLines(t, path,
		usageLine(now-2, "a", "first"),
		huge,
		usageLine(now, "b", "later"),
	)
	got := mustSummarizeLimited(t, path, Query{Range: RangeAll, Now: now}, ReadLimits{MaxBytes: 1 << 20, MaxLine: 160})
	if got.Summary.Requests != 2 {
		t.Fatalf("requests=%d", got.Summary.Requests)
	}
	if !modelPresent(got, "later") {
		t.Fatal("later valid row hidden by oversized row")
	}
	if modelPresent(got, strings.Repeat("m", 200)) {
		t.Fatal("oversized row was retained")
	}
}

func TestUnterminatedFinalJSONRecordIsExcludedUntilNewline(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC).UnixMilli()
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	complete := usageLine(now-1, "a", "complete")
	pending := usageLine(now, "b", "pending")
	if err := os.WriteFile(path, []byte(complete+"\n"+pending), 0o600); err != nil {
		t.Fatal(err)
	}
	before := mustSummarizeLimited(t, path, Query{Range: RangeAll, Now: now}, ReadLimits{MaxBytes: 1 << 20, MaxLine: 1 << 10})
	if before.Summary.Requests != 1 || !modelPresent(before, "complete") || modelPresent(before, "pending") {
		t.Fatalf("unterminated JSON accepted: %+v", before.Summary)
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("\n"); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	after := mustSummarizeLimited(t, path, Query{Range: RangeAll, Now: now}, ReadLimits{MaxBytes: 1 << 20, MaxLine: 1 << 10})
	if after.Summary.Requests != 2 || !modelPresent(after, "pending") {
		t.Fatalf("newline-committed row missing: %+v", after.Summary)
	}
}

func TestTruncatedTailObeysUnterminatedFinalRecordRule(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC).UnixMilli()
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	old := usageLine(now-3, "old", "stale")
	kept := usageLine(now-1, "a", "kept")
	pending := usageLine(now, "b", "pending")
	body := old + "\n" + kept + "\n" + pending
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	limit := int64(len(kept) + 1 + len(pending))
	got := mustSummarizeLimited(t, path, Query{Range: RangeAll, Now: now}, ReadLimits{MaxBytes: limit, MaxLine: 1 << 10})
	if !got.HistoryTruncated {
		t.Fatal("expected truncation")
	}
	if got.Summary.Requests != 1 || !modelPresent(got, "kept") || modelPresent(got, "pending") || modelPresent(got, "stale") {
		t.Fatalf("truncated unterminated tail leaked: %+v", got.Summary)
	}
}

func TestInvalidSemanticJSONLRowsAreRejected(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC).UnixMilli()
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	validEmpty := `{"timestamp":` + strconv.FormatInt(now-1, 10) + `}`
	validMissingStatus := `{"timestamp":` + strconv.FormatInt(now, 10) + `,"provider":"a","model":"kept"}`
	writeUsageLines(t, path,
		"{}",
		`{"timestamp":0,"provider":"z","model":"zero"}`,
		`{"timestamp":-1,"provider":"z","model":"neg"}`,
		validEmpty,
		validMissingStatus,
	)
	got := mustSummarizeLimited(t, path, Query{Range: RangeAll, Now: now}, ReadLimits{MaxBytes: 1 << 20, MaxLine: 1 << 10})
	if got.Summary.Requests != 2 {
		t.Fatalf("requests=%d summary=%+v", got.Summary.Requests, got.Summary)
	}
	if got.Summary.UnreportedRequests != 2 {
		t.Fatalf("unreported=%d", got.Summary.UnreportedRequests)
	}
	if got.Cost.UnmeteredRequests != 2 {
		t.Fatalf("unmetered=%d", got.Cost.UnmeteredRequests)
	}
	if got.SurfaceAttribution.Unattributed != 2 {
		t.Fatalf("unattributed=%d", got.SurfaceAttribution.Unattributed)
	}
	if !modelPresent(got, "kept") || !modelPresent(got, "") || modelPresent(got, "zero") || modelPresent(got, "neg") {
		t.Fatalf("invalid semantic rows leaked into models: %+v", got.Models)
	}
	if !providerPresent(got, "a") || !providerPresent(got, "") || providerPresent(got, "z") {
		t.Fatalf("providers=%+v", got.Providers)
	}
	if got.SnapshotWindowStart == nil || *got.SnapshotWindowStart != now-1 || got.SnapshotWindowEnd == nil || *got.SnapshotWindowEnd != now {
		t.Fatalf("window start=%v end=%v", got.SnapshotWindowStart, got.SnapshotWindowEnd)
	}
}

func TestInvalidSemanticRowDoesNotHideLaterValidRow(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC).UnixMilli()
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	writeUsageLines(t, path,
		"{}",
		usageLine(now, "b", "later"),
	)
	got := mustSummarizeLimited(t, path, Query{Range: RangeAll, Now: now}, ReadLimits{MaxBytes: 1 << 20, MaxLine: 1 << 10})
	if got.Summary.Requests != 1 || !modelPresent(got, "later") {
		t.Fatalf("valid row hidden: %+v", got.Summary)
	}
}

func TestConcurrentAppendCannotProduceHalfRecord(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC).UnixMilli()
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	complete := usageLine(now-1, "a", "complete") + "\n"
	if err := os.WriteFile(path, []byte(complete), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	partial := `{"timestamp":` + strconv.FormatInt(now, 10) + `,"provider":"b","model":"partial"`
	if _, err := file.WriteString(partial); err != nil {
		t.Fatal(err)
	}
	if err := file.Sync(); err != nil {
		t.Fatal(err)
	}

	got := mustSummarizeLimited(t, path, Query{Range: RangeAll, Now: now}, ReadLimits{MaxBytes: 1 << 20, MaxLine: 1 << 10})
	if got.Summary.Requests != 1 || !modelPresent(got, "complete") || modelPresent(got, "partial") {
		t.Fatalf("half record leaked: %+v", got.Summary)
	}

	if _, err := file.WriteString(`,"usageStatus":"reported"}` + "\n"); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	after := mustSummarizeLimited(t, path, Query{Range: RangeAll, Now: now}, ReadLimits{MaxBytes: 1 << 20, MaxLine: 1 << 10})
	if after.Summary.Requests != 2 || !modelPresent(after, "partial") {
		t.Fatalf("completed append missing: %+v", after.Summary)
	}
}

func TestEmptyRetainedWindowDoesNotFabricateTimestamps(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	if err := os.WriteFile(path, []byte("{}\nnot-json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := mustSummarizeLimited(t, path, Query{Range: RangeAll, Now: 1}, ReadLimits{MaxBytes: 1 << 20, MaxLine: 1 << 10})
	if got.SnapshotWindowStart != nil || got.SnapshotWindowEnd != nil {
		t.Fatalf("fabricated window start=%v end=%v", got.SnapshotWindowStart, got.SnapshotWindowEnd)
	}
	if got.HistoryTruncated {
		t.Fatal("small file should not be truncated")
	}
}

func mustSnapshot(t *testing.T, path string, limits ReadLimits) fileSnapshot {
	t.Helper()
	got, err := readFileSnapshot(path, limits)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func assertPrefixEqualsFirstRetainedOffset(t *testing.T, body string, snap fileSnapshot) {
	t.Helper()
	if len(snap.Entries) == 0 {
		t.Fatal("expected retained rows")
	}
	needle := usageLine(snap.Entries[0].Timestamp, snap.Entries[0].Provider, snap.Entries[0].Model)
	offset := int64(strings.Index(body, needle))
	if offset < 0 {
		t.Fatalf("retained row missing from body: %+v", snap.Entries[0])
	}
	if snap.TruncatedPrefixBytes != offset {
		t.Fatalf("prefix=%d first retained offset=%d", snap.TruncatedPrefixBytes, offset)
	}
}

func mustSummarizeLimited(t *testing.T, path string, q Query, limits ReadLimits) Summary {
	t.Helper()
	got, err := summarizeFileQueryLimited(path, q, NewTable(), limits)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func writeUsageLines(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func usageLine(ts int64, provider, model string) string {
	return `{"timestamp":` + strconv.FormatInt(ts, 10) + `,"provider":` + strconv.Quote(provider) + `,"model":` + strconv.Quote(model) + `,"usageStatus":"reported","usage":{"inputTokens":1,"outputTokens":1}}`
}

func modelPresent(s Summary, model string) bool {
	for _, row := range s.Models {
		if row.Model == model {
			return true
		}
	}
	return false
}

func providerPresent(s Summary, provider string) bool {
	for _, row := range s.Providers {
		if row.Provider == provider {
			return true
		}
	}
	return false
}
