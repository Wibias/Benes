package usage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSummarizeCountsTokensAndSurfaces(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.Local).UnixMilli()
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	ts := strconv.FormatInt(now, 10)
	body := `{"timestamp":` + ts + `,"provider":"openai-apikey","model":"gpt-5.5","usageStatus":"reported","usage":{"inputTokens":10,"outputTokens":2}}` + "\n" +
		`{"timestamp":` + ts + `,"provider":"xai","model":"grok-4","surface":"grok","usageStatus":"reported","usage":{"inputTokens":3,"outputTokens":1}}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	all, err := SummarizeFile(path, RangeAll, SurfaceAll, now)
	if err != nil {
		t.Fatal(err)
	}
	if all.Summary.Requests != 2 || all.Summary.InputTokens != 13 || all.Summary.OutputTokens != 3 || all.Summary.PricedRequests != 0 {
		t.Fatalf("%+v", all.Summary)
	}
	got, err := SummarizeFile(path, RangeAll, SurfaceGrok, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Summary.Requests != 1 || got.Summary.InputTokens != 3 {
		t.Fatalf("%+v", got.Summary)
	}
}

func TestSummarizeMissingFileIsEmpty(t *testing.T) {
	now := time.Now().UnixMilli()
	out, err := SummarizeFile(filepath.Join(t.TempDir(), "missing.jsonl"), Range30d, SurfaceAll, now)
	if err != nil {
		t.Fatal(err)
	}
	if out.Summary.Requests != 0 {
		t.Fatalf("%+v", out.Summary)
	}
}

func TestSummarizeKeepsUnknownOutOfExactAndEstimatedTotals(t *testing.T) {
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC).UnixMilli()
	table := NewTable()
	table.Add(PriceRecord{
		Provider:    "openai-apikey",
		Model:       "gpt-5.5",
		Cost4:       Cost4{Input: 5, Output: 0},
		SourceClass: SourceManufacturer,
		Status:      StatusVerified,
		Currency:    "USD",
	})
	table.Add(PriceRecord{
		Provider:    "openrouter",
		Model:       "x-ai/grok-4.6",
		Cost4:       Cost4{Input: 2, Output: 0},
		SourceClass: SourceGateway,
		Status:      StatusDerived,
		Currency:    "USD",
		LowerBound:  true,
	})
	entries := []entry{
		mustEntry(t, now, "openai-apikey", "gpt-5.5"),
		mustEntry(t, now, "openrouter", "x-ai/grok-4.6"),
		mustEntry(t, now, "together", "grok-4.6-free"),
	}
	got := SummarizeWithTable(entries, RangeAll, SurfaceAll, now, table)
	if got.Summary.ExactCostUsd != 5 || got.Summary.EstimatedCostUsd != 5 {
		t.Fatalf("exact/estimated collapsed unknown into zero: %+v", got.Summary)
	}
	if got.Summary.LowerBoundCostUsd != 2 {
		t.Fatalf("lower bound=%v", got.Summary.LowerBoundCostUsd)
	}
	if got.Summary.PricedRequests != 2 || got.Summary.UnpricedRequests != 1 {
		t.Fatalf("priced=%d unpriced=%d", got.Summary.PricedRequests, got.Summary.UnpricedRequests)
	}
	var free *modelRow
	for i := range got.Models {
		if got.Models[i].Model == "grok-4.6-free" {
			free = &got.Models[i]
		}
	}
	if free == nil || free.EstimatedCostUsd != nil || free.CostStatus != string(ConfidenceUnknown) {
		t.Fatalf("free alias billed as zero: %#v", free)
	}
}

func mustEntry(t *testing.T, now int64, provider, model string) entry {
	t.Helper()
	raw := `{"timestamp":` + strconv.FormatInt(now, 10) + `,"provider":` + strconv.Quote(provider) + `,"model":` + strconv.Quote(model) + `,"usageStatus":"reported","usage":{"inputTokens":1000000,"outputTokens":0}}`
	var item entry
	if err := json.Unmarshal([]byte(raw), &item); err != nil {
		t.Fatal(err)
	}
	return item
}

func TestCacheTotalsOmitWhenUnseenAndKeepGenuineZero(t *testing.T) {
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC).UnixMilli()
	unseenRaw := `{"timestamp":` + strconv.FormatInt(now, 10) + `,"provider":"a","model":"m","usageStatus":"reported","usage":{"inputTokens":10,"outputTokens":2}}`
	unseen := SummarizeQuery([]entry{mustUsageEntry(t, unseenRaw)}, Query{Range: RangeAll, Now: now, Location: time.UTC}, NewTable())
	raw, err := json.Marshal(unseen.Summary)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if strings.Contains(body, "cacheReadInputTokens") || strings.Contains(body, "cachedInputTokens") || strings.Contains(body, "cacheCreationInputTokens") {
		t.Fatalf("unknown cache serialized as zero: %s", body)
	}

	zeroRaw := `{"timestamp":` + strconv.FormatInt(now, 10) + `,"provider":"a","model":"m","usageStatus":"reported","usage":{"inputTokens":10,"outputTokens":2,"cacheReadInputTokens":0}}`
	zero := SummarizeQuery([]entry{mustUsageEntry(t, zeroRaw)}, Query{Range: RangeAll, Now: now, Location: time.UTC}, NewTable())
	if zero.Summary.CacheReadInputTokens == nil || *zero.Summary.CacheReadInputTokens != 0 {
		t.Fatalf("genuine zero dropped: %+v", zero.Summary)
	}
	rawZero, err := json.Marshal(zero.Summary)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rawZero), `"cacheReadInputTokens":0`) {
		t.Fatalf("genuine zero omitted: %s", rawZero)
	}

	mixed := SummarizeQuery([]entry{
		mustUsageEntry(t, `{"timestamp":`+strconv.FormatInt(now, 10)+`,"provider":"a","model":"m","usageStatus":"reported","usage":{"inputTokens":10,"outputTokens":2,"cacheReadInputTokens":4}}`),
		mustUsageEntry(t, unseenRaw),
	}, Query{Range: RangeAll, Now: now, Location: time.UTC}, NewTable())
	if mixed.Summary.CacheReadInputTokens == nil || *mixed.Summary.CacheReadInputTokens != 4 {
		t.Fatalf("sum invented missing cache as zero: %+v", mixed.Summary)
	}
}

func TestSummarizeQueryFiltersProviderModelAccountWithCustomRange(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, loc).UnixMilli()
	inRange := time.Date(2026, 8, 21, 12, 0, 0, 0, loc).UnixMilli()
	outRange := time.Date(2026, 8, 19, 12, 0, 0, 0, loc).UnixMilli()
	entries := []entry{
		{Timestamp: inRange, Provider: "openai", Model: "gpt-5", Account: "acct-a", UsageStatus: "reported"},
		{Timestamp: inRange, Provider: "openai", Model: "gpt-4", Account: "acct-a", UsageStatus: "reported"},
		{Timestamp: inRange, Provider: "xai", Model: "gpt-5", Account: "acct-a", UsageStatus: "reported"},
		{Timestamp: inRange, Provider: "openai", Model: "gpt-5", Account: "acct-b", UsageStatus: "reported"},
		{Timestamp: outRange, Provider: "openai", Model: "gpt-5", Account: "acct-a", UsageStatus: "reported"},
	}
	got := SummarizeQuery(entries, Query{
		Start:    "2026-08-20",
		End:      "2026-08-22",
		Provider: "openai",
		Model:    "gpt-5",
		Account:  "acct-a",
		Now:      now,
		Location: loc,
	}, NewTable())
	if got.Summary.Requests != 1 {
		t.Fatalf("composed filters=%+v", got.Summary)
	}
}

func TestSummarizeAccountsFromAuthoritativeAccountField(t *testing.T) {
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC).UnixMilli()
	got := SummarizeQuery([]entry{
		{Timestamp: now, Provider: "openai", Model: "gpt-5", Account: "acct-a", UsageStatus: "reported", Usage: &TokenUsage{InputTokens: 2, OutputTokens: 1}},
		{Timestamp: now, Provider: "openai", Model: "gpt-5", Account: "acct-a", UsageStatus: "unreported"},
		{Timestamp: now, Provider: "openai", Model: "gpt-5", Account: "acct-b", UsageStatus: "reported", Usage: &TokenUsage{InputTokens: 1, OutputTokens: 0}},
	}, Query{Range: RangeAll, Now: now}, NewTable())
	if len(got.Accounts) != 2 {
		t.Fatalf("accounts=%+v", got.Accounts)
	}
	if got.Accounts[0].AccountLogLabel != "acct-a" || got.Accounts[0].Requests != 2 || got.Accounts[0].UsageCoverageRatio != 0.5 {
		t.Fatalf("acct-a=%+v", got.Accounts[0])
	}
}

func mustUsageEntry(t *testing.T, raw string) entry {
	t.Helper()
	var item entry
	if err := json.Unmarshal([]byte(raw), &item); err != nil {
		t.Fatal(err)
	}
	return item
}
