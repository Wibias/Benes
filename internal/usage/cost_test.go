package usage

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestUsageProvenanceAndCoverageArithmetic(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC).UnixMilli()
	reported := entry{Timestamp: now, Provider: "a", Model: "r", UsageStatus: "reported", Usage: tokenUsage(10, 2)}
	estimated := entry{Timestamp: now, Provider: "a", Model: "e", UsageStatus: "estimated", Usage: tokenUsage(4, 1)}
	unsupported := entry{Timestamp: now, Provider: "a", Model: "u", UsageStatus: "unsupported"}
	unreported := entry{Timestamp: now, Provider: "a", Model: "n", UsageStatus: "unreported"}
	missing := entry{Timestamp: now, Provider: "a", Model: "m"}
	got := SummarizeQuery([]entry{reported, estimated, unsupported, unreported, missing}, Query{Range: RangeAll, Now: now}, NewTable())
	s := got.Summary
	if s.Requests != 5 {
		t.Fatalf("requests=%d", s.Requests)
	}
	if s.ReportedRequests != 1 || s.EstimatedRequests != 1 || s.UnsupportedRequests != 1 || s.UnreportedRequests != 2 {
		t.Fatalf("provenance=%+v", s)
	}
	if s.MeasuredRequests != s.ReportedRequests+s.EstimatedRequests {
		t.Fatalf("measured=%d reported=%d estimated=%d", s.MeasuredRequests, s.ReportedRequests, s.EstimatedRequests)
	}
	if s.MeasuredRequests != 2 {
		t.Fatalf("missing usage counted as measured: %+v", s)
	}
	if s.InputTokens != 14 || s.OutputTokens != 3 || s.TotalTokens != 17 {
		t.Fatalf("tokens invented zeros: %+v", s)
	}
	if s.CoverageRatio != 0.4 {
		t.Fatalf("coverage=%v", s.CoverageRatio)
	}
}

func TestCostConfidenceBucketsAndLegacyFields(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC).UnixMilli()
	table := NewTable()
	table.Add(PriceRecord{Provider: "a", Model: "exact", Cost4: Cost4{Input: 1, Output: 0}, SourceClass: SourceManufacturer, Status: StatusVerified, Currency: "USD"})
	table.Add(PriceRecord{Provider: "a", Model: "est", Cost4: Cost4{Input: 2, Output: 0}, SourceClass: SourceDerivedThirdParty, Status: StatusDerived, Currency: "USD"})
	table.Add(PriceRecord{Provider: "a", Model: "low", Cost4: Cost4{Input: 3, Output: 0}, SourceClass: SourceManufacturer, Status: StatusVerified, Currency: "USD", LowerBound: true})
	table.Add(PriceRecord{Provider: "a", Model: "stale", Cost4: Cost4{Input: 4, Output: 0}, SourceClass: SourceManufacturer, Status: StatusVerified, Currency: "USD", VerifiedUntil: now - 1})
	entries := []entry{
		mustEntry(t, now, "a", "exact"),
		mustEntry(t, now, "a", "est"),
		mustEntry(t, now, "a", "low"),
		mustEntry(t, now, "a", "stale"),
		mustEntry(t, now, "a", "none"),
		{Timestamp: now, Provider: "a", Model: "bare", UsageStatus: "reported"},
	}
	got := SummarizeWithTable(entries, RangeAll, SurfaceAll, now, table)
	s := got.Summary
	if s.ExactCostUsd != 1 || s.EstimatedCostUsd != 3 || s.LowerBoundCostUsd != 3 || s.StaleCostUsd != 4 {
		t.Fatalf("legacy totals=%+v", s)
	}
	if s.PricedRequests != 4 || s.UnpricedRequests != 1 || s.UnmeteredRequests != 1 {
		t.Fatalf("priced=%d unpriced=%d unmetered=%d", s.PricedRequests, s.UnpricedRequests, s.UnmeteredRequests)
	}
	cost := got.Cost
	if cost.Currency != "USD" {
		t.Fatalf("currency=%q", cost.Currency)
	}
	if cost.Exact != (CostBucket{AmountUsd: 1, Requests: 1}) || cost.Estimated != (CostBucket{AmountUsd: 2, Requests: 1}) {
		t.Fatalf("exact/estimated buckets=%+v", cost)
	}
	if cost.LowerBound != (CostBucket{AmountUsd: 3, Requests: 1}) || cost.Stale != (CostBucket{AmountUsd: 4, Requests: 1}) {
		t.Fatalf("lower/stale buckets=%+v", cost)
	}
	if cost.PricedRequests != 4 || cost.UnpricedRequests != 1 || cost.UnmeteredRequests != 1 {
		t.Fatalf("cost counts=%+v", cost)
	}
	if cost.DisplayTotalSafe {
		t.Fatal("mixed confidence must not claim a safe display total")
	}
}

func TestMixedModelAndProviderCostIsOrderIndependent(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC).UnixMilli()
	table := NewTable()
	table.Add(PriceRecord{Provider: "p", Model: "m", Cost4: Cost4{Input: 1, Output: 0}, SourceClass: SourceManufacturer, Status: StatusVerified, Currency: "USD"})
	table.Add(PriceRecord{Provider: "p", Model: "n", Cost4: Cost4{Input: 2, Output: 0}, SourceClass: SourceDerivedThirdParty, Status: StatusDerived, Currency: "USD"})
	first := []entry{mustEntry(t, now, "p", "m"), mustEntry(t, now, "p", "n")}
	second := []entry{mustEntry(t, now, "p", "n"), mustEntry(t, now, "p", "m")}
	a := SummarizeWithTable(first, RangeAll, SurfaceAll, now, table)
	b := SummarizeWithTable(second, RangeAll, SurfaceAll, now, table)
	if a.Cost != b.Cost {
		t.Fatalf("order changed cost %+v vs %+v", a.Cost, b.Cost)
	}
	rowA := modelByName(a, "m")
	rowB := modelByName(b, "m")
	if rowA.CostStatus != string(ConfidenceExact) || rowB.CostStatus != string(ConfidenceExact) {
		t.Fatalf("exact model status a=%q b=%q", rowA.CostStatus, rowB.CostStatus)
	}
	provA := a.Providers[0]
	provB := b.Providers[0]
	if provA.CostStatus != "mixed" || provB.CostStatus != "mixed" {
		t.Fatalf("provider status a=%q b=%q", provA.CostStatus, provB.CostStatus)
	}
	if provA.Cost.Status != "mixed" || provB.Cost.Status != "mixed" {
		t.Fatalf("provider cost status a=%q b=%q", provA.Cost.Status, provB.Cost.Status)
	}
}

func TestDisplayTotalSafeRequiresSinglePricedClassAndNoGaps(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC).UnixMilli()
	table := NewTable()
	table.Add(PriceRecord{Provider: "a", Model: "exact", Cost4: Cost4{Input: 5, Output: 0}, SourceClass: SourceManufacturer, Status: StatusVerified, Currency: "USD"})
	table.Add(PriceRecord{Provider: "a", Model: "est", Cost4: Cost4{Input: 2, Output: 0}, SourceClass: SourceDerivedThirdParty, Status: StatusDerived, Currency: "USD"})
	table.Add(PriceRecord{Provider: "a", Model: "low", Cost4: Cost4{Input: 3, Output: 0}, SourceClass: SourceManufacturer, Status: StatusVerified, Currency: "USD", LowerBound: true})
	table.Add(PriceRecord{Provider: "a", Model: "stale", Cost4: Cost4{Input: 4, Output: 0}, SourceClass: SourceManufacturer, Status: StatusVerified, Currency: "USD", VerifiedUntil: now - 1})

	exact := SummarizeWithTable([]entry{mustEntry(t, now, "a", "exact")}, RangeAll, SurfaceAll, now, table)
	if !exact.Cost.DisplayTotalSafe || exact.Cost.Status != string(ConfidenceExact) {
		t.Fatalf("exact-only=%+v", exact.Cost)
	}

	estimated := SummarizeWithTable([]entry{mustEntry(t, now, "a", "est")}, RangeAll, SurfaceAll, now, table)
	if !estimated.Cost.DisplayTotalSafe || estimated.Cost.Status != string(ConfidenceEstimated) {
		t.Fatalf("estimated-only=%+v", estimated.Cost)
	}

	lower := SummarizeWithTable([]entry{mustEntry(t, now, "a", "low")}, RangeAll, SurfaceAll, now, table)
	if !lower.Cost.DisplayTotalSafe || lower.Cost.Status != string(ConfidenceLowerBound) {
		t.Fatalf("lower-bound-only must be safe only with explicit status: %+v", lower.Cost)
	}

	stale := SummarizeWithTable([]entry{mustEntry(t, now, "a", "stale")}, RangeAll, SurfaceAll, now, table)
	if !stale.Cost.DisplayTotalSafe || stale.Cost.Status != string(ConfidenceStale) {
		t.Fatalf("stale-only must be safe only with explicit status: %+v", stale.Cost)
	}

	mixed := SummarizeWithTable([]entry{mustEntry(t, now, "a", "exact"), mustEntry(t, now, "a", "est")}, RangeAll, SurfaceAll, now, table)
	if mixed.Cost.DisplayTotalSafe || mixed.Cost.Status != "mixed" {
		t.Fatalf("exact+estimated=%+v", mixed.Cost)
	}

	unpriced := SummarizeWithTable([]entry{mustEntry(t, now, "a", "exact"), mustEntry(t, now, "a", "none")}, RangeAll, SurfaceAll, now, table)
	if unpriced.Cost.DisplayTotalSafe {
		t.Fatalf("exact+unpriced claimed safe: %+v", unpriced.Cost)
	}

	unmetered := SummarizeWithTable([]entry{
		mustEntry(t, now, "a", "exact"),
		{Timestamp: now, Provider: "a", Model: "bare", UsageStatus: "reported"},
	}, RangeAll, SurfaceAll, now, table)
	if unmetered.Cost.DisplayTotalSafe {
		t.Fatalf("exact+unmetered claimed safe: %+v", unmetered.Cost)
	}

	allUnmetered := SummarizeWithTable([]entry{{Timestamp: now, Provider: "a", Model: "bare", UsageStatus: "reported"}}, RangeAll, SurfaceAll, now, table)
	if allUnmetered.Cost.DisplayTotalSafe {
		t.Fatalf("all unmetered claimed safe: %+v", allUnmetered.Cost)
	}

	empty := SummarizeWithTable(nil, RangeAll, SurfaceAll, now, table)
	if empty.Cost.DisplayTotalSafe || empty.Summary.Requests != 0 {
		t.Fatalf("empty claimed safe: %+v", empty.Cost)
	}
}

func TestDayModelProviderAccountCostClassesStayConsistent(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, loc).UnixMilli()
	table := NewTable()
	table.Add(PriceRecord{Provider: "p", Model: "exact", Cost4: Cost4{Input: 1, Output: 0}, SourceClass: SourceManufacturer, Status: StatusVerified, Currency: "USD"})
	table.Add(PriceRecord{Provider: "p", Model: "est", Cost4: Cost4{Input: 2, Output: 0}, SourceClass: SourceDerivedThirdParty, Status: StatusDerived, Currency: "USD"})
	table.Add(PriceRecord{Provider: "p", Model: "low", Cost4: Cost4{Input: 3, Output: 0}, SourceClass: SourceManufacturer, Status: StatusVerified, Currency: "USD", LowerBound: true})
	table.Add(PriceRecord{Provider: "p", Model: "stale", Cost4: Cost4{Input: 4, Output: 0}, SourceClass: SourceManufacturer, Status: StatusVerified, Currency: "USD", VerifiedUntil: now - 1})

	staleOnly := entry{Timestamp: now, Provider: "p", Model: "stale", Account: "alpha", UsageStatus: "reported", Usage: tokenUsage(1_000_000, 0)}
	lowerOnly := entry{Timestamp: now, Provider: "p", Model: "low", Account: "alpha", UsageStatus: "reported", Usage: tokenUsage(1_000_000, 0)}
	exactOnly := entry{Timestamp: now, Provider: "p", Model: "exact", Account: "beta", UsageStatus: "reported", Usage: tokenUsage(1_000_000, 0)}
	estimatedOnly := entry{Timestamp: now, Provider: "p", Model: "est", Account: "beta", UsageStatus: "reported", Usage: tokenUsage(1_000_000, 0)}
	unpriced := entry{Timestamp: now, Provider: "p", Model: "none", Account: "gamma", UsageStatus: "reported", Usage: tokenUsage(1_000_000, 0)}
	unmetered := entry{Timestamp: now, Provider: "p", Model: "bare", Account: "gamma", UsageStatus: "reported"}

	q := Query{Range: RangeToday, Now: now, Location: loc}
	first := SummarizeQuery([]entry{staleOnly, lowerOnly, exactOnly, estimatedOnly, unpriced, unmetered}, q, table)
	second := SummarizeQuery([]entry{unmetered, unpriced, estimatedOnly, exactOnly, lowerOnly, staleOnly}, q, table)
	if first.Cost != second.Cost {
		t.Fatalf("order changed total cost %+v vs %+v", first.Cost, second.Cost)
	}
	assertCostClassEverywhere(t, first, "stale", ConfidenceStale, 4)
	assertCostClassEverywhere(t, first, "low", ConfidenceLowerBound, 3)
	assertCostClassEverywhere(t, first, "exact", ConfidenceExact, 1)
	assertCostClassEverywhere(t, first, "est", ConfidenceEstimated, 2)

	if first.Cost.UnpricedRequests != 1 || first.Cost.UnmeteredRequests != 1 {
		t.Fatalf("gap counts=%+v", first.Cost)
	}
	if first.Days[0].Cost.Stale.Requests != 1 || first.Days[0].UnpricedRequests != 1 {
		t.Fatalf("day stale leaked into unpriced: %+v", first.Days[0])
	}
	if first.Days[0].Cost.UnmeteredRequests != 1 {
		t.Fatalf("day unmetered=%+v", first.Days[0].Cost)
	}
	acc := accountByLabel(first, "alpha")
	if acc.Cost.Stale.Requests != 1 || acc.Cost.LowerBound.Requests != 1 || acc.Cost.DisplayTotalSafe {
		t.Fatalf("account cost=%+v", acc.Cost)
	}
	raw, err := json.Marshal(acc)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "estimatedCostUsd") {
		t.Fatalf("account estimatedCostUsd leaked mixed confidence: %s", raw)
	}
	if first.Cost.Exact.AmountUsd != 1 || first.Cost.Estimated.AmountUsd != 2 || first.Cost.LowerBound.AmountUsd != 3 || first.Cost.Stale.AmountUsd != 4 {
		t.Fatalf("total buckets=%+v", first.Cost)
	}
	if first.Days[0].Cost.Exact != first.Cost.Exact || first.Providers[0].Cost.Exact != first.Cost.Exact {
		t.Fatalf("day/provider exact drifted from total")
	}
	if modelByName(first, "exact").Cost.Exact.AmountUsd+modelByName(first, "est").Cost.Estimated.AmountUsd != first.Cost.Exact.AmountUsd+first.Cost.Estimated.AmountUsd {
		t.Fatalf("model priced sum drifted")
	}
}

func TestExactOnlyDisplayTotalIsSafe(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC).UnixMilli()
	table := NewTable()
	table.Add(PriceRecord{Provider: "a", Model: "exact", Cost4: Cost4{Input: 5, Output: 0}, SourceClass: SourceManufacturer, Status: StatusVerified, Currency: "USD"})
	got := SummarizeWithTable([]entry{mustEntry(t, now, "a", "exact")}, RangeAll, SurfaceAll, now, table)
	if !got.Cost.DisplayTotalSafe || got.Cost.Exact.AmountUsd != 5 || got.Cost.Estimated.Requests != 0 {
		t.Fatalf("exact-only cost=%+v", got.Cost)
	}
	row := modelByName(got, "exact")
	if row.CostStatus != string(ConfidenceExact) {
		t.Fatalf("model status=%q", row.CostStatus)
	}
}

func tokenUsage(in, out int64) *TokenUsage {
	return &TokenUsage{InputTokens: in, OutputTokens: out}
}

func modelByName(s Summary, model string) modelRow {
	for _, row := range s.Models {
		if row.Model == model {
			return row
		}
	}
	return modelRow{}
}

func accountByLabel(s Summary, label string) accountRow {
	for _, row := range s.Accounts {
		if row.AccountLogLabel == label || row.Account == label {
			return row
		}
	}
	return accountRow{}
}

func assertCostClassEverywhere(t *testing.T, got Summary, model string, class Confidence, amount float64) {
	t.Helper()
	row := modelByName(got, model)
	if bucketRequests(row.Cost, class) != 1 || bucketAmount(row.Cost, class) != amount {
		t.Fatalf("model %s class %s cost=%+v", model, class, row.Cost)
	}
	if len(got.Days) == 0 || bucketRequests(got.Days[0].Cost, class) < 1 {
		t.Fatalf("day missing %s for %s: %+v", class, model, got.Days)
	}
	if len(got.Providers) == 0 || bucketRequests(got.Providers[0].Cost, class) < 1 {
		t.Fatalf("provider missing %s for %s: %+v", class, model, got.Providers)
	}
	foundAccount := false
	for _, acc := range got.Accounts {
		if bucketRequests(acc.Cost, class) > 0 {
			foundAccount = true
			break
		}
	}
	if !foundAccount {
		t.Fatalf("account missing %s for %s: %+v", class, model, got.Accounts)
	}
}

func bucketRequests(cost CostSummary, class Confidence) int {
	switch class {
	case ConfidenceExact:
		return cost.Exact.Requests
	case ConfidenceEstimated:
		return cost.Estimated.Requests
	case ConfidenceLowerBound:
		return cost.LowerBound.Requests
	case ConfidenceStale:
		return cost.Stale.Requests
	default:
		return 0
	}
}

func bucketAmount(cost CostSummary, class Confidence) float64 {
	switch class {
	case ConfidenceExact:
		return cost.Exact.AmountUsd
	case ConfidenceEstimated:
		return cost.Estimated.AmountUsd
	case ConfidenceLowerBound:
		return cost.LowerBound.AmountUsd
	case ConfidenceStale:
		return cost.Stale.AmountUsd
	default:
		return 0
	}
}
