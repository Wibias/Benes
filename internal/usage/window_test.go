package usage

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	_ "time/tzdata"
)

func TestTodayIsLocalCalendarDayNotRolling24h(t *testing.T) {
	loc := time.FixedZone("UTC+2", 2*3600)
	now := time.Date(2026, 8, 22, 1, 0, 0, 0, loc)
	before := time.Date(2026, 8, 21, 23, 30, 0, 0, loc).UnixMilli()
	start := time.Date(2026, 8, 22, 0, 0, 0, 0, loc).UnixMilli()
	mid := time.Date(2026, 8, 22, 12, 0, 0, 0, loc).UnixMilli()
	end := time.Date(2026, 8, 23, 0, 0, 0, 0, loc).UnixMilli()
	entries := []entry{
		{Timestamp: before, Provider: "a", Model: "m", UsageStatus: "reported"},
		{Timestamp: start, Provider: "a", Model: "m", UsageStatus: "reported"},
		{Timestamp: mid, Provider: "a", Model: "m", UsageStatus: "reported"},
		{Timestamp: end, Provider: "a", Model: "m", UsageStatus: "reported"},
	}
	got := SummarizeQuery(entries, Query{Range: RangeToday, Now: now.UnixMilli(), Location: loc}, NewTable())
	if got.Summary.Requests != 2 {
		t.Fatalf("today counted rolling 24h: %+v since=%v until=%v", got.Summary, *got.Since, *got.Until)
	}
}

func TestYesterdayAndExplicitDate(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 8, 22, 15, 0, 0, 0, loc).UnixMilli()
	entries := []entry{
		{Timestamp: time.Date(2026, 8, 21, 0, 0, 0, 0, loc).UnixMilli(), Provider: "a", Model: "y", UsageStatus: "reported"},
		{Timestamp: time.Date(2026, 8, 21, 23, 59, 59, 0, loc).UnixMilli(), Provider: "a", Model: "y", UsageStatus: "reported"},
		{Timestamp: time.Date(2026, 8, 22, 0, 0, 0, 0, loc).UnixMilli(), Provider: "a", Model: "t", UsageStatus: "reported"},
		{Timestamp: time.Date(2026, 8, 20, 12, 0, 0, 0, loc).UnixMilli(), Provider: "a", Model: "d", UsageStatus: "reported"},
	}
	yesterday := SummarizeQuery(entries, Query{Range: RangeYesterday, Now: now, Location: loc}, NewTable())
	if yesterday.Summary.Requests != 2 {
		t.Fatalf("yesterday=%+v", yesterday.Summary)
	}
	day := SummarizeQuery(entries, Query{Date: "2026-08-20", Now: now, Location: loc}, NewTable())
	if day.Summary.Requests != 1 || day.Range != RangeDate {
		t.Fatalf("date=%+v range=%s", day.Summary, day.Range)
	}
}

func TestEmptyQuerySummaryPreservesResolvedCustomAndDateWindows(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 1, 15, 0, 0, 0, berlin).UnixMilli()
	custom := EmptyQuerySummary(Query{
		Start:    "2026-09-01T10:00",
		End:      "2026-09-01T12:00",
		Location: berlin,
		Now:      now,
	})
	if custom.Range != RangeCustom {
		t.Fatalf("custom range=%s", custom.Range)
	}
	wantStart := time.Date(2026, 9, 1, 10, 0, 0, 0, berlin).UnixMilli()
	wantEnd := time.Date(2026, 9, 1, 12, 0, 0, 0, berlin).UnixMilli()
	if custom.Since == nil || *custom.Since != wantStart || custom.Until == nil || *custom.Until != wantEnd {
		t.Fatalf("custom window since=%v until=%v", custom.Since, custom.Until)
	}
	if custom.Summary.Requests != 0 || custom.HistoryTruncated || custom.TruncatedPrefixBytes != 0 {
		t.Fatalf("empty custom fabricated totals: %+v", custom)
	}

	day := EmptyQuerySummary(Query{Date: "2026-09-01", Location: berlin, Now: now})
	if day.Range != RangeDate {
		t.Fatalf("date range=%s", day.Range)
	}
	wantDay := time.Date(2026, 9, 1, 0, 0, 0, 0, berlin).UnixMilli()
	wantNext := time.Date(2026, 9, 2, 0, 0, 0, 0, berlin).UnixMilli()
	if day.Since == nil || *day.Since != wantDay || day.Until == nil || *day.Until != wantNext {
		t.Fatalf("date window since=%v until=%v", day.Since, day.Until)
	}
}

func TestCustomRangeIsInclusiveStartExclusiveEnd(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, loc).UnixMilli()
	start := time.Date(2026, 8, 20, 0, 0, 0, 0, loc).UnixMilli()
	end := time.Date(2026, 8, 22, 0, 0, 0, 0, loc).UnixMilli()
	entries := []entry{
		{Timestamp: start - 1, Provider: "a", Model: "m", UsageStatus: "reported"},
		{Timestamp: start, Provider: "a", Model: "m", UsageStatus: "reported"},
		{Timestamp: end - 1, Provider: "a", Model: "m", UsageStatus: "reported"},
		{Timestamp: end, Provider: "a", Model: "m", UsageStatus: "reported"},
	}
	got := SummarizeQuery(entries, Query{Start: "2026-08-20", End: "2026-08-22", Now: now, Location: loc}, NewTable())
	if got.Summary.Requests != 2 || got.Range != RangeCustom {
		t.Fatalf("%+v range=%s", got.Summary, got.Range)
	}
}

func TestCustomRangeRejectsInvertedAndTooLongWindows(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, loc).UnixMilli()
	entries := []entry{{Timestamp: now, Provider: "a", Model: "m", UsageStatus: "reported"}}
	inverted := SummarizeQuery(entries, Query{Start: "2026-08-22", End: "2026-08-20", Now: now, Location: loc}, NewTable())
	if inverted.Summary.Requests != 0 {
		t.Fatalf("inverted=%+v", inverted.Summary)
	}
	tooLong := SummarizeQuery(entries, Query{Start: "2025-01-01", End: "2026-08-22", Now: now, Location: loc}, NewTable())
	if tooLong.Summary.Requests != 0 {
		t.Fatalf("unbounded custom range: %+v", tooLong.Summary)
	}
}

func TestTimezoneOffsetChangesCalendarDay(t *testing.T) {
	utc := time.UTC
	plus12 := time.FixedZone("UTC+12", 12*3600)
	now := time.Date(2026, 8, 22, 13, 0, 0, 0, utc)
	event := time.Date(2026, 8, 22, 1, 0, 0, 0, utc)
	entries := []entry{{Timestamp: event.UnixMilli(), Provider: "a", Model: "m", UsageStatus: "reported"}}
	inUTC := SummarizeQuery(entries, Query{Range: RangeToday, Now: now.UnixMilli(), Location: utc}, NewTable())
	inPlus12 := SummarizeQuery(entries, Query{Range: RangeToday, Now: now.UnixMilli(), Location: plus12}, NewTable())
	if inUTC.Summary.Requests != 1 {
		t.Fatalf("utc today=%+v", inUTC.Summary)
	}
	if inPlus12.Summary.Requests != 0 {
		t.Fatalf("+12 counted previous local day as today: %+v", inPlus12.Summary)
	}
}

func TestCustomRangeEmitsCalendarDayRows(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, loc).UnixMilli()
	entries := []entry{
		{Timestamp: time.Date(2026, 8, 20, 12, 0, 0, 0, loc).UnixMilli(), Provider: "a", Model: "m", UsageStatus: "reported"},
		{Timestamp: time.Date(2026, 8, 21, 12, 0, 0, 0, loc).UnixMilli(), Provider: "a", Model: "m", UsageStatus: "reported"},
	}
	got := SummarizeQuery(entries, Query{Start: "2026-08-20", End: "2026-08-22", Now: now, Location: loc}, NewTable())
	if len(got.Days) != 2 || got.Days[0].Date != "2026-08-20" || got.Days[1].Date != "2026-08-21" {
		t.Fatalf("%#v", got.Days)
	}
}

func berlin(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func TestParseLocationPrefersIANAThenBoundedOffset(t *testing.T) {
	iana := ParseLocation("Europe/Berlin", "0")
	if iana.String() != "Europe/Berlin" {
		t.Fatalf("iana=%s", iana)
	}
	offset := ParseLocation("", "120")
	if _, seconds := time.Date(2026, 8, 22, 0, 0, 0, 0, offset).Zone(); seconds != 2*3600 {
		t.Fatalf("offset seconds=%d", seconds)
	}
	if got := ParseLocation("", "9999"); got != time.Local {
		t.Fatalf("out-of-range offset=%v", got)
	}
}

func TestDSTSpringForwardCalendarDay(t *testing.T) {
	loc := berlin(t)
	before := time.Date(2026, 3, 28, 23, 59, 59, 0, loc)
	start := time.Date(2026, 3, 29, 0, 0, 0, 0, loc)
	afterGap := time.Date(2026, 3, 29, 3, 0, 0, 0, loc)
	end := time.Date(2026, 3, 30, 0, 0, 0, 0, loc)
	now := time.Date(2026, 3, 29, 12, 0, 0, 0, loc)
	entries := []entry{
		{Timestamp: before.UnixMilli(), Provider: "a", Model: "m", UsageStatus: "reported"},
		{Timestamp: start.UnixMilli(), Provider: "a", Model: "m", UsageStatus: "reported"},
		{Timestamp: afterGap.UnixMilli(), Provider: "a", Model: "m", UsageStatus: "reported"},
		{Timestamp: end.UnixMilli(), Provider: "a", Model: "m", UsageStatus: "reported"},
	}
	got := SummarizeQuery(entries, Query{Range: RangeToday, Now: now.UnixMilli(), Location: loc}, NewTable())
	if got.Summary.Requests != 2 {
		t.Fatalf("spring-forward today=%+v since=%v until=%v", got.Summary, *got.Since, *got.Until)
	}
}

func TestDSTFallBackCalendarDayIncludesRepeatedHour(t *testing.T) {
	loc := berlin(t)
	before := time.Date(2026, 10, 24, 23, 59, 59, 0, loc)
	first := time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC)  // 02:30 CEST
	second := time.Date(2026, 10, 25, 1, 30, 0, 0, time.UTC) // 02:30 CET
	end := time.Date(2026, 10, 26, 0, 0, 0, 0, loc)
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, loc)
	entries := []entry{
		{Timestamp: before.UnixMilli(), Provider: "a", Model: "m", UsageStatus: "reported"},
		{Timestamp: first.UnixMilli(), Provider: "a", Model: "m", UsageStatus: "reported"},
		{Timestamp: second.UnixMilli(), Provider: "a", Model: "m", UsageStatus: "reported"},
		{Timestamp: end.UnixMilli(), Provider: "a", Model: "m", UsageStatus: "reported"},
	}
	got := SummarizeQuery(entries, Query{Range: RangeToday, Now: now.UnixMilli(), Location: loc}, NewTable())
	if got.Summary.Requests != 2 {
		t.Fatalf("fall-back today=%+v since=%v until=%v firstLocal=%s secondLocal=%s",
			got.Summary, *got.Since, *got.Until, first.In(loc), second.In(loc))
	}
}

func TestBoundedRangeZeroFillsEmptyDays(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, loc)
	entries := []entry{
		{Timestamp: time.Date(2026, 8, 20, 12, 0, 0, 0, loc).UnixMilli(), Provider: "a", Model: "m", UsageStatus: "reported"},
	}
	got := SummarizeQuery(entries, Query{Range: Range7d, Now: now.UnixMilli(), Location: loc}, NewTable())
	if len(got.Days) != 7 {
		t.Fatalf("7d days=%d %#v", len(got.Days), got.Days)
	}
	if got.Days[0].Date != "2026-08-16" || got.Days[6].Date != "2026-08-22" {
		t.Fatalf("7d bounds %#v", got.Days)
	}
	var active int
	for _, day := range got.Days {
		if day.Requests > 0 {
			active++
			if day.Date != "2026-08-20" || len(day.Models) != 1 || day.Models[0].Model != "m" {
				t.Fatalf("active day %#v", day)
			}
		}
		if day.Models == nil {
			t.Fatalf("nil models on %s", day.Date)
		}
	}
	if active != 1 {
		t.Fatalf("active=%d", active)
	}
}

func TestCustomRangeZeroFillsGapsWithModels(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, loc).UnixMilli()
	ts := time.Date(2026, 8, 20, 12, 0, 0, 0, loc).UnixMilli()
	entries := []entry{
		{Timestamp: ts, Provider: "a", Model: "one", UsageStatus: "reported", Usage: &TokenUsage{InputTokens: 3, OutputTokens: 1}},
		{Timestamp: ts, Provider: "b", Model: "two", UsageStatus: "estimated"},
	}
	got := SummarizeQuery(entries, Query{Start: "2026-08-20", End: "2026-08-23", Now: now, Location: loc}, NewTable())
	if len(got.Days) != 3 || got.Days[0].Date != "2026-08-20" || got.Days[1].Date != "2026-08-21" || got.Days[2].Date != "2026-08-22" {
		t.Fatalf("%#v", got.Days)
	}
	day := got.Days[0]
	if day.Requests != 2 || day.MeasuredRequests != 2 || day.ReportedRequests != 1 {
		t.Fatalf("coverage %#v", day)
	}
	if len(day.Models) != 2 || day.Models[0].Model != "one" || day.Models[0].TotalTokens != 4 {
		t.Fatalf("models %#v", day.Models)
	}
	if got.Days[1].Requests != 0 || len(got.Days[1].Models) != 0 {
		t.Fatalf("gap %#v", got.Days[1])
	}
}

func TestAllRangeOmitsDays(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, loc).UnixMilli()
	got := SummarizeQuery([]entry{{Timestamp: now, Provider: "a", Model: "m", UsageStatus: "reported"}}, Query{Range: RangeAll, Now: now, Location: loc}, NewTable())
	if len(got.Days) != 0 {
		t.Fatalf("all days=%#v", got.Days)
	}
}

func TestCustomRangeAcceptsMinutePrecisionInclusiveStartExclusiveEnd(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 8, 20, 16, 0, 0, 0, loc).UnixMilli()
	start := time.Date(2026, 8, 20, 14, 30, 0, 0, loc)
	end := time.Date(2026, 8, 20, 15, 0, 0, 0, loc)
	entries := []entry{
		{Timestamp: start.UnixMilli() - 1, Provider: "a", Model: "m", UsageStatus: "reported"},
		{Timestamp: start.UnixMilli(), Provider: "a", Model: "m", UsageStatus: "reported"},
		{Timestamp: end.UnixMilli() - 1, Provider: "a", Model: "m", UsageStatus: "reported"},
		{Timestamp: end.UnixMilli(), Provider: "a", Model: "m", UsageStatus: "reported"},
	}
	got := SummarizeQuery(entries, Query{Start: "2026-08-20T14:30", End: "2026-08-20T15:00", Now: now, Location: loc}, NewTable())
	if got.Summary.Requests != 2 || got.Range != RangeCustom {
		t.Fatalf("%+v range=%s since=%v until=%v", got.Summary, got.Range, got.Since, got.Until)
	}
	if got.Since == nil || *got.Since != start.UnixMilli() || got.Until == nil || *got.Until != end.UnixMilli() {
		t.Fatalf("window since=%v until=%v", got.Since, got.Until)
	}
}

func TestCustomRangeAcceptsRFC3339AbsoluteInstants(t *testing.T) {
	loc := berlin(t)
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, loc).UnixMilli()
	first := time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC)  // 02:30 CEST
	second := time.Date(2026, 10, 25, 1, 30, 0, 0, time.UTC) // 02:30 CET
	end := time.Date(2026, 10, 25, 1, 30, 1, 0, time.UTC)    // 02:30:01 CET
	entries := []entry{
		{Timestamp: first.UnixMilli() - 1, Provider: "a", Model: "m", UsageStatus: "reported"},
		{Timestamp: first.UnixMilli(), Provider: "a", Model: "m", UsageStatus: "reported"},
		{Timestamp: second.UnixMilli(), Provider: "a", Model: "m", UsageStatus: "reported"},
		{Timestamp: end.UnixMilli(), Provider: "a", Model: "m", UsageStatus: "reported"},
	}
	got := SummarizeQuery(entries, Query{
		Start:    "2026-10-25T02:30:00+02:00",
		End:      "2026-10-25T02:30:01+01:00",
		Now:      now,
		Location: loc,
	}, NewTable())
	if got.Summary.Requests != 2 {
		t.Fatalf("dst rfc3339=%+v since=%v until=%v", got.Summary, got.Since, got.Until)
	}
}

func TestSummarizeFileQueryFailsClosedOnMalformedReversedAndTooLong(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC).UnixMilli()
	line := `{"timestamp":` + strconv.FormatInt(now, 10) + `,"provider":"a","model":"m","usageStatus":"reported"}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	table := NewTable()
	cases := []struct {
		name  string
		query Query
		want  error
	}{
		{name: "malformed", query: Query{Start: "nope", End: "2026-08-22", Now: now, Location: time.UTC}, want: ErrInvalidRange},
		{name: "reversed", query: Query{Start: "2026-08-22", End: "2026-08-20", Now: now, Location: time.UTC}, want: ErrReversedRange},
		{name: "too long", query: Query{Start: "2025-01-01", End: "2026-08-22", Now: now, Location: time.UTC}, want: ErrRangeTooLong},
		{name: "one sided", query: Query{Start: "2026-08-20", Now: now, Location: time.UTC}, want: ErrInvalidRange},
	}
	for _, tc := range cases {
		_, err := SummarizeFileQuery(path, tc.query, table)
		if !errors.Is(err, tc.want) {
			t.Fatalf("%s err=%v want %v", tc.name, err, tc.want)
		}
	}
}
