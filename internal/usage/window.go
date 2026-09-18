package usage

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

const maxCustomDays = 366

var (
	ErrInvalidRange  = errors.New("invalid usage range")
	ErrReversedRange = errors.New("usage range end must be after start")
	ErrRangeTooLong  = errors.New("usage range exceeds 366 days")
)

type Query struct {
	Range    Range
	Surface  Surface
	Provider string
	Model    string
	Account  string
	Now      int64
	Location *time.Location
	Date     string
	Start    string
	End      string
}

type timeWindow struct {
	since *int64
	until *int64
}

func ParseLocation(name, offsetMinutes string) *time.Location {
	name = strings.TrimSpace(name)
	if name != "" {
		if loc, err := time.LoadLocation(name); err == nil {
			return loc
		}
	}
	offsetMinutes = strings.TrimSpace(offsetMinutes)
	if offsetMinutes != "" {
		minutes, err := strconv.Atoi(offsetMinutes)
		if err == nil && minutes >= -14*60 && minutes <= 14*60 {
			return time.FixedZone("offset", minutes*60)
		}
	}
	return time.Local
}

func (q Query) location() *time.Location {
	if q.Location != nil {
		return q.Location
	}
	return time.Local
}

func (q Query) instant() time.Time {
	now := q.Now
	if now <= 0 {
		now = time.Now().UnixMilli()
	}
	return time.UnixMilli(now).In(q.location())
}

func dayStart(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

func isDateOnly(raw string) bool {
	raw = strings.TrimSpace(raw)
	_, err := time.Parse("2006-01-02", raw)
	return err == nil && len(raw) == 10
}

func parseInstant(raw string, loc *time.Location) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, ErrInvalidRange
	}
	if loc == nil {
		loc = time.Local
	}
	for _, layout := range []string{
		"2006-01-02",
		"2006-01-02T15:04",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04",
		"2006-01-02 15:04:05",
	} {
		if t, err := time.ParseInLocation(layout, raw, loc); err == nil {
			return t, nil
		}
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t, nil
	}
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return t, nil
	}
	return time.Time{}, ErrInvalidRange
}

func resolveWindow(q Query) (timeWindow, error) {
	rng := q.Range
	if strings.TrimSpace(q.Date) != "" {
		rng = RangeDate
	} else if strings.TrimSpace(q.Start) != "" || strings.TrimSpace(q.End) != "" {
		rng = RangeCustom
	}
	loc := q.location()
	now := q.instant()
	today := dayStart(now)
	switch rng {
	case RangeAll:
		return timeWindow{}, nil
	case RangeToday:
		until := today.AddDate(0, 0, 1).UnixMilli()
		since := today.UnixMilli()
		return timeWindow{since: &since, until: &until}, nil
	case RangeYesterday:
		start := today.AddDate(0, 0, -1)
		until := today.UnixMilli()
		since := start.UnixMilli()
		return timeWindow{since: &since, until: &until}, nil
	case RangeDate:
		if !isDateOnly(q.Date) {
			return timeWindow{}, fmt.Errorf("%w: date", ErrInvalidRange)
		}
		day, err := parseInstant(q.Date, loc)
		if err != nil {
			return timeWindow{}, fmt.Errorf("%w: date", ErrInvalidRange)
		}
		until := day.AddDate(0, 0, 1).UnixMilli()
		since := day.UnixMilli()
		return timeWindow{since: &since, until: &until}, nil
	case RangeCustom:
		if strings.TrimSpace(q.Start) == "" || strings.TrimSpace(q.End) == "" {
			return timeWindow{}, ErrInvalidRange
		}
		start, err := parseInstant(q.Start, loc)
		if err != nil {
			return timeWindow{}, fmt.Errorf("%w: start", ErrInvalidRange)
		}
		end, err := parseInstant(q.End, loc)
		if err != nil {
			return timeWindow{}, fmt.Errorf("%w: end", ErrInvalidRange)
		}
		if !end.After(start) {
			return timeWindow{}, ErrReversedRange
		}
		if end.Sub(start) > time.Duration(maxCustomDays)*24*time.Hour {
			return timeWindow{}, ErrRangeTooLong
		}
		since := start.UnixMilli()
		until := end.UnixMilli()
		return timeWindow{since: &since, until: &until}, nil
	default:
		days := 29
		if rng == Range7d {
			days = 6
		}
		since := today.AddDate(0, 0, -days).UnixMilli()
		return timeWindow{since: &since}, nil
	}
}

func queryWindow(q Query) timeWindow {
	win, err := resolveWindow(q)
	if err != nil {
		return timeWindow{since: int64ptr(0), until: int64ptr(0)}
	}
	return win
}

func ResolveWindow(q Query) error {
	_, err := resolveWindow(q)
	return err
}

func (w timeWindow) contains(ts int64) bool {
	if w.since != nil && ts < *w.since {
		return false
	}
	if w.until != nil && ts >= *w.until {
		return false
	}
	return true
}

func int64ptr(v int64) *int64 { return &v }

func finishDays(entries []entry, q Query, table *Table) []dayRow {
	if q.Range == RangeAll && strings.TrimSpace(q.Date) == "" && strings.TrimSpace(q.Start) == "" && strings.TrimSpace(q.End) == "" {
		return []dayRow{}
	}
	win := queryWindow(q)
	dates := calendarDates(q, win)
	if len(dates) == 0 {
		return []dayRow{}
	}
	if table == nil {
		table = DefaultTable()
	}
	loc := q.location()
	type acc struct {
		row    dayRow
		models map[string]*dayModelRow
	}
	grouped := map[string]*acc{}
	for _, date := range dates {
		grouped[date] = &acc{row: dayRow{Date: date, Models: []dayModelRow{}}, models: map[string]*dayModelRow{}}
	}
	for _, item := range entries {
		date := time.UnixMilli(item.Timestamp).In(loc).Format("2006-01-02")
		bucket := grouped[date]
		if bucket == nil {
			continue
		}
		status := item.UsageStatus
		if status == "" {
			status = "unreported"
		}
		bucket.row.Requests++
		if status == "reported" || status == "estimated" {
			bucket.row.MeasuredRequests++
		}
		if status == "reported" {
			bucket.row.ReportedRequests++
		}
		tokens := displayTokens(item)
		bucket.row.TotalTokens += tokens
		key := item.Provider + "/" + item.Model
		model := bucket.models[key]
		if model == nil {
			model = &dayModelRow{Model: item.Model, Provider: item.Provider}
			bucket.models[key] = model
		}
		model.Requests++
		model.TotalTokens += tokens
		if item.Usage == nil {
			addUnmetered(&bucket.row.Cost)
			continue
		}
		priced, ok := table.Price(item, q.Now)
		if !ok {
			bucket.row.UnpricedRequests++
			addUnpriced(&bucket.row.Cost)
			continue
		}
		addPriced(priced, &bucket.row.Cost)
		addPricedDayLegacy(&bucket.row, priced)
	}
	out := make([]dayRow, 0, len(dates))
	for _, date := range dates {
		bucket := grouped[date]
		bucket.row.Models = finishDayModels(bucket.models)
		bucket.row.Cost = finishCost(bucket.row.Cost)
		out = append(out, bucket.row)
	}
	return out
}

func calendarDates(q Query, win timeWindow) []string {
	if win.since == nil {
		return nil
	}
	if win.until != nil && *win.until <= *win.since {
		return nil
	}
	loc := q.location()
	start := dayStart(time.UnixMilli(*win.since).In(loc))
	endExclusive := dayStart(q.instant()).AddDate(0, 0, 1)
	if win.until != nil {
		endExclusive = time.UnixMilli(*win.until).In(loc)
	}
	out := make([]string, 0, 32)
	for d := start; d.Before(endExclusive); d = d.AddDate(0, 0, 1) {
		out = append(out, d.Format("2006-01-02"))
		if len(out) > maxCustomDays {
			return nil
		}
	}
	return out
}

func finishDayModels(in map[string]*dayModelRow) []dayModelRow {
	out := make([]dayModelRow, 0, len(in))
	for _, row := range in {
		out = append(out, *row)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TotalTokens != out[j].TotalTokens {
			return out[i].TotalTokens > out[j].TotalTokens
		}
		if out[i].Provider != out[j].Provider {
			return out[i].Provider < out[j].Provider
		}
		return out[i].Model < out[j].Model
	})
	return out
}
