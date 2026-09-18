package usage

import (
	"sort"
	"strings"
)

type Range string
type Surface string

const (
	Range7d        Range = "7d"
	Range30d       Range = "30d"
	RangeAll       Range = "all"
	RangeToday     Range = "today"
	RangeYesterday Range = "yesterday"
	RangeDate      Range = "date"
	RangeCustom    Range = "custom"

	SurfaceAll    Surface = "all"
	SurfaceCodex  Surface = "codex"
	SurfaceClaude Surface = "claude"
	SurfaceGrok   Surface = "grok"
)

const (
	maxReadBytes = 64 << 20
	maxModelRows = 256
)

type TokenUsage struct {
	InputTokens              int64  `json:"inputTokens"`
	OutputTokens             int64  `json:"outputTokens"`
	CachedInputTokens        *int64 `json:"cachedInputTokens"`
	CacheReadInputTokens     *int64 `json:"cacheReadInputTokens"`
	CacheCreationInputTokens *int64 `json:"cacheCreationInputTokens"`
	ReasoningOutputTokens    *int64 `json:"reasoningOutputTokens"`
}

type Attempt struct {
	Provider    string `json:"provider"`
	Model       string `json:"model"`
	UsageStatus string `json:"usageStatus"`
}

type RouteProfile struct {
	ID string `json:"id,omitempty"`
}

type RouteSelected struct {
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
}

type RouteDecision struct {
	RouteKind     string              `json:"routeKind,omitempty"`
	Profile       *RouteProfile       `json:"profile,omitempty"`
	Selected      *RouteSelected      `json:"selected,omitempty"`
	SidecarPolicy *RouteSidecarPolicy `json:"sidecarPolicy,omitempty"`
}

// RouteSidecarPolicy is the per-request Harness sidecar decision evidence. It
// separates the configured policy from the actual resolution, and records
// execution only when a sidecar actually ran.
type RouteSidecarPolicy struct {
	Identity  RouteSidecarIdentity `json:"identity"`
	WebSearch RouteSidecarOutcome  `json:"webSearch"`
	Vision    RouteSidecarOutcome  `json:"vision"`
}

type RouteSidecarIdentity struct {
	Status    string `json:"status"`
	HarnessID string `json:"harnessId,omitempty"`
}

type RouteSidecarConfigured struct {
	Enabled bool   `json:"enabled"`
	Source  string `json:"source"`
}

type RouteSidecarOutcome struct {
	Configured RouteSidecarConfigured `json:"configured"`
	Resolution string                 `json:"resolution"`
	Ran        bool                   `json:"ran"`
	Failure    string                 `json:"failure,omitempty"`
}

type Entry struct {
	Timestamp      int64          `json:"timestamp"`
	RequestID      string         `json:"requestId,omitempty"`
	Status         int            `json:"status,omitempty"`
	DurationMs     int64          `json:"durationMs"`
	RequestedModel string         `json:"requestedModel,omitempty"`
	Provider       string         `json:"provider"`
	Model          string         `json:"model"`
	Account        string         `json:"account,omitempty"`
	Surface        string         `json:"surface,omitempty"`
	UsageStatus    string         `json:"usageStatus"`
	TotalTokens    *int64         `json:"totalTokens,omitempty"`
	Usage          *TokenUsage    `json:"usage,omitempty"`
	Attempts       []Attempt      `json:"attempts,omitempty"`
	RouteDecision  *RouteDecision `json:"routeDecision,omitempty"`
}

type entry = Entry

type SurfaceCounts struct {
	Codex         int `json:"codex"`
	Claude        int `json:"claude"`
	ClaudeDesktop int `json:"claudeDesktop"`
	Grok          int `json:"grok"`
	Unattributed  int `json:"unattributed"`
}

type CostBucket struct {
	AmountUsd float64 `json:"amountUsd"`
	Requests  int     `json:"requests"`
}

type CostSummary struct {
	Currency          string     `json:"currency,omitempty"`
	Exact             CostBucket `json:"exact"`
	Estimated         CostBucket `json:"estimated"`
	LowerBound        CostBucket `json:"lowerBound"`
	Stale             CostBucket `json:"stale"`
	PricedRequests    int        `json:"pricedRequests"`
	UnpricedRequests  int        `json:"unpricedRequests"`
	UnmeteredRequests int        `json:"unmeteredRequests"`
	DisplayTotalSafe  bool       `json:"displayTotalSafe"`
	Status            string     `json:"status,omitempty"`
}

type Totals struct {
	Requests                 int     `json:"requests"`
	AttemptCount             int     `json:"attemptCount"`
	MeasuredRequests         int     `json:"measuredRequests"`
	ReportedRequests         int     `json:"reportedRequests"`
	UnreportedRequests       int     `json:"unreportedRequests"`
	UnsupportedRequests      int     `json:"unsupportedRequests"`
	EstimatedRequests        int     `json:"estimatedRequests"`
	InputTokens              int64   `json:"inputTokens"`
	OutputTokens             int64   `json:"outputTokens"`
	CachedInputTokens        *int64  `json:"cachedInputTokens,omitempty"`
	CacheReadInputTokens     *int64  `json:"cacheReadInputTokens,omitempty"`
	CacheCreationInputTokens *int64  `json:"cacheCreationInputTokens,omitempty"`
	ReasoningOutputTokens    int64   `json:"reasoningOutputTokens"`
	TotalTokens              int64   `json:"totalTokens"`
	CoverageRatio            float64 `json:"coverageRatio"`
	EstimatedCostUsd         float64 `json:"estimatedCostUsd"`
	ExactCostUsd             float64 `json:"exactCostUsd"`
	LowerBoundCostUsd        float64 `json:"lowerBoundCostUsd"`
	StaleCostUsd             float64 `json:"staleCostUsd"`
	PricedRequests           int     `json:"pricedRequests"`
	UnpricedRequests         int     `json:"unpricedRequests"`
	UnmeteredRequests        int     `json:"unmeteredRequests"`
}

type Summary struct {
	Range                Range         `json:"range"`
	Surface              Surface       `json:"surface"`
	Since                *int64        `json:"since"`
	Until                *int64        `json:"until,omitempty"`
	GeneratedAt          int64         `json:"generatedAt"`
	Summary              Totals        `json:"summary"`
	Days                 []dayRow      `json:"days"`
	Models               []modelRow    `json:"models"`
	Providers            []providerRow `json:"providers"`
	Accounts             []accountRow  `json:"accounts"`
	HistoryTruncated     bool          `json:"historyTruncated"`
	TruncatedPrefixBytes int64         `json:"truncatedPrefixBytes"`
	SnapshotWindowStart  *int64        `json:"snapshotWindowStart"`
	SnapshotWindowEnd    *int64        `json:"snapshotWindowEnd"`
	SurfaceAttribution   SurfaceCounts `json:"surfaceAttribution"`
	Cost                 CostSummary   `json:"cost"`
}

type dayRow struct {
	Date              string        `json:"date"`
	Requests          int           `json:"requests"`
	MeasuredRequests  int           `json:"measuredRequests"`
	ReportedRequests  int           `json:"reportedRequests"`
	TotalTokens       int64         `json:"totalTokens"`
	ExactCostUsd      float64       `json:"exactCostUsd"`
	EstimatedCostUsd  float64       `json:"estimatedCostUsd"`
	LowerBoundCostUsd float64       `json:"lowerBoundCostUsd"`
	StaleCostUsd      float64       `json:"staleCostUsd"`
	UnpricedRequests  int           `json:"unpricedRequests"`
	Cost              CostSummary   `json:"cost"`
	Models            []dayModelRow `json:"models"`
}

type dayModelRow struct {
	Model       string `json:"model"`
	Provider    string `json:"provider"`
	Requests    int    `json:"requests"`
	TotalTokens int64  `json:"totalTokens"`
}

type modelRow struct {
	Provider          string      `json:"provider"`
	Model             string      `json:"model"`
	Requests          int         `json:"requests"`
	AttemptCount      int         `json:"attemptCount"`
	MeasuredRequests  int         `json:"measuredRequests"`
	ReportedRequests  int         `json:"reportedRequests"`
	EstimatedRequests int         `json:"estimatedRequests"`
	TotalTokens       int64       `json:"totalTokens"`
	InputTokens       int64       `json:"inputTokens"`
	OutputTokens      int64       `json:"outputTokens"`
	ShareRatio        float64     `json:"shareRatio"`
	EstimatedCostUsd  *float64    `json:"estimatedCostUsd,omitempty"`
	CostStatus        string      `json:"costStatus,omitempty"`
	Cost              CostSummary `json:"cost"`
}

type accountRow struct {
	Account            string      `json:"account"`
	AccountLogLabel    string      `json:"accountLogLabel"`
	Requests           int         `json:"requests"`
	MeasuredRequests   int         `json:"measuredRequests"`
	ReportedRequests   int         `json:"reportedRequests"`
	TotalTokens        int64       `json:"totalTokens"`
	UsageCoverageRatio float64     `json:"usageCoverageRatio"`
	Cost               CostSummary `json:"cost"`
}

type providerRow struct {
	Provider          string      `json:"provider"`
	Requests          int         `json:"requests"`
	AttemptCount      int         `json:"attemptCount"`
	MeasuredRequests  int         `json:"measuredRequests"`
	ReportedRequests  int         `json:"reportedRequests"`
	EstimatedRequests int         `json:"estimatedRequests"`
	TotalTokens       int64       `json:"totalTokens"`
	ShareRatio        float64     `json:"shareRatio"`
	EstimatedCostUsd  *float64    `json:"estimatedCostUsd,omitempty"`
	CostStatus        string      `json:"costStatus,omitempty"`
	Cost              CostSummary `json:"cost"`
}

func ParseRange(raw string) Range {
	switch raw {
	case "7d", "30d", "all", "today", "yesterday", "date", "custom":
		return Range(raw)
	default:
		return Range30d
	}
}

func ParseSurface(raw string) Surface {
	switch raw {
	case "codex", "claude", "grok":
		return Surface(raw)
	default:
		return SurfaceAll
	}
}

func SummarizeFile(path string, rng Range, surface Surface, now int64) (Summary, error) {
	return SummarizeFileWithTable(path, rng, surface, now, DefaultTable())
}

func SummarizeFileWithTable(path string, rng Range, surface Surface, now int64, table *Table) (Summary, error) {
	return SummarizeFileQuery(path, Query{Range: rng, Surface: surface, Now: now}, table)
}

func SummarizeFileQuery(path string, q Query, table *Table) (Summary, error) {
	return summarizeFileQueryLimited(path, q, table, defaultReadLimits())
}

func SummarizeHomeQuery(home string, q Query, table *Table) (Summary, error) {
	return summarizeHomeQueryLimited(home, q, table, defaultReadLimits())
}

func summarizeFileQueryLimited(path string, q Query, table *Table, limits ReadLimits) (Summary, error) {
	if _, err := resolveWindow(q); err != nil {
		return emptySummary(q.Range, q.Surface, q.Now), err
	}
	snap, err := readFileSnapshot(path, limits)
	if err != nil {
		return emptySummary(q.Range, q.Surface, q.Now), err
	}
	out := SummarizeQuery(snap.Entries, q, table)
	applySnapshot(&out, snap)
	return out, nil
}

func applySnapshot(out *Summary, snap fileSnapshot) {
	if out == nil {
		return
	}
	out.HistoryTruncated = snap.HistoryTruncated
	out.TruncatedPrefixBytes = snap.TruncatedPrefixBytes
	out.SnapshotWindowStart = snap.SnapshotWindowStart
	out.SnapshotWindowEnd = snap.SnapshotWindowEnd
}

func emptySummary(rng Range, surface Surface, now int64) Summary {
	return Summary{
		Range:       rng,
		Surface:     surface,
		GeneratedAt: now,
		Days:        []dayRow{},
		Models:      []modelRow{},
		Providers:   []providerRow{},
		Accounts:    []accountRow{},
	}
}

func EmptyQuerySummary(q Query) Summary {
	return SummarizeQuery(nil, q, NewTable())
}

func Summarize(entries []entry, rng Range, surface Surface, now int64) Summary {
	return SummarizeWithTable(entries, rng, surface, now, DefaultTable())
}

func SummarizeWithTable(entries []entry, rng Range, surface Surface, now int64, table *Table) Summary {
	return SummarizeQuery(entries, Query{Range: rng, Surface: surface, Now: now}, table)
}

func SummarizeQuery(entries []entry, q Query, table *Table) Summary {
	if table == nil {
		table = DefaultTable()
	}
	if q.Range == "" {
		q.Range = Range30d
	}
	if q.Surface == "" {
		q.Surface = SurfaceAll
	}
	win := queryWindow(q)
	filtered := make([]entry, 0, len(entries))
	for _, item := range entries {
		if !win.contains(item.Timestamp) {
			continue
		}
		if !matchSurface(item.Surface, q.Surface) {
			continue
		}
		if !matchFilter(item.Provider, q.Provider) || !matchFilter(item.Model, q.Model) || !matchFilter(item.Account, q.Account) {
			continue
		}
		filtered = append(filtered, item)
	}
	totals := Totals{}
	cost := CostSummary{}
	models := map[string]*modelRow{}
	providers := map[string]*providerRow{}
	accounts := map[string]*accountRow{}
	attr := SurfaceCounts{}
	for _, item := range filtered {
		status := item.UsageStatus
		if status == "" {
			status = "unreported"
		}
		bump(&totals, status)
		if len(item.Attempts) > 0 {
			totals.AttemptCount += len(item.Attempts)
		} else {
			totals.AttemptCount++
		}
		addTokens(&totals, item)
		key := item.Provider + "/" + item.Model
		row := models[key]
		if row == nil {
			row = &modelRow{Provider: item.Provider, Model: item.Model}
			models[key] = row
		}
		row.Requests++
		if len(item.Attempts) > 0 {
			row.AttemptCount += len(item.Attempts)
		} else {
			row.AttemptCount++
		}
		if status == "reported" || status == "estimated" {
			row.MeasuredRequests++
		}
		if status == "reported" {
			row.ReportedRequests++
		}
		if status == "estimated" {
			row.EstimatedRequests++
		}
		if item.Usage != nil {
			row.InputTokens += item.Usage.InputTokens
			row.OutputTokens += item.Usage.OutputTokens
			row.TotalTokens += displayTokens(item)
		}
		prov := providers[item.Provider]
		if prov == nil {
			prov = &providerRow{Provider: item.Provider}
			providers[item.Provider] = prov
		}
		prov.Requests++
		if len(item.Attempts) > 0 {
			prov.AttemptCount += len(item.Attempts)
		} else {
			prov.AttemptCount++
		}
		if status == "reported" || status == "estimated" {
			prov.MeasuredRequests++
		}
		if status == "reported" {
			prov.ReportedRequests++
		}
		if status == "estimated" {
			prov.EstimatedRequests++
		}
		if item.Usage != nil {
			prov.TotalTokens += displayTokens(item)
		}
		var acc *accountRow
		if label := strings.TrimSpace(item.Account); label != "" {
			acc = accounts[label]
			if acc == nil {
				acc = &accountRow{Account: label, AccountLogLabel: label}
				accounts[label] = acc
			}
			acc.Requests++
			if status == "reported" || status == "estimated" {
				acc.MeasuredRequests++
			}
			if status == "reported" {
				acc.ReportedRequests++
			}
			if item.Usage != nil {
				acc.TotalTokens += displayTokens(item)
			}
		}
		applyCost(&totals, &cost, row, prov, acc, table, item, q.Now)
		countSurface(&attr, item.Surface)
	}
	if totals.Requests > 0 {
		totals.CoverageRatio = float64(totals.MeasuredRequests) / float64(totals.Requests)
	}
	out := emptySummary(q.Range, q.Surface, q.Now)
	if q.Date != "" {
		out.Range = RangeDate
	} else if q.Start != "" || q.End != "" {
		out.Range = RangeCustom
	}
	out.Since = win.since
	out.Until = win.until
	out.Summary = totals
	out.Cost = finishCost(cost)
	out.SurfaceAttribution = attr
	out.Days = finishDays(filtered, q, table)
	out.Models = finishModels(models, totals.TotalTokens)
	out.Providers = finishProviders(providers, totals.TotalTokens)
	out.Accounts = finishAccounts(accounts)
	return out
}

func countSurface(attr *SurfaceCounts, got string) {
	if attr == nil {
		return
	}
	switch got {
	case "codex":
		attr.Codex++
	case "claude":
		attr.Claude++
	case "claude-desktop":
		attr.ClaudeDesktop++
	case "grok":
		attr.Grok++
	default:
		attr.Unattributed++
	}
}

func applyCost(totals *Totals, cost *CostSummary, row *modelRow, prov *providerRow, acc *accountRow, table *Table, item entry, now int64) {
	var accountCost *CostSummary
	if acc != nil {
		accountCost = &acc.Cost
	}
	if item.Usage == nil {
		totals.UnmeteredRequests++
		addUnmetered(cost, &row.Cost, &prov.Cost, accountCost)
		return
	}
	priced, ok := table.Price(item, now)
	if !ok {
		totals.UnpricedRequests++
		addUnpriced(cost, &row.Cost, &prov.Cost, accountCost)
		return
	}
	addPricedTotals(totals, priced)
	addPriced(priced, cost, &row.Cost, &prov.Cost, accountCost)
	addOptionalCost(&row.EstimatedCostUsd, priced.USD)
	addOptionalCost(&prov.EstimatedCostUsd, priced.USD)
}

func addUnmetered(targets ...*CostSummary) {
	for _, target := range targets {
		if target != nil {
			target.UnmeteredRequests++
		}
	}
}

func addUnpriced(targets ...*CostSummary) {
	for _, target := range targets {
		if target != nil {
			target.UnpricedRequests++
		}
	}
}

func addPriced(priced ResolvedPrice, targets ...*CostSummary) {
	for _, target := range targets {
		if target == nil {
			continue
		}
		switch priced.Confidence {
		case ConfidenceExact:
			addCostBucket(&target.Exact, priced.USD)
		case ConfidenceEstimated:
			addCostBucket(&target.Estimated, priced.USD)
		case ConfidenceLowerBound:
			addCostBucket(&target.LowerBound, priced.USD)
		case ConfidenceStale:
			addCostBucket(&target.Stale, priced.USD)
		default:
			target.UnpricedRequests++
		}
	}
}

func addPricedTotals(totals *Totals, priced ResolvedPrice) {
	if totals == nil {
		return
	}
	switch priced.Confidence {
	case ConfidenceExact:
		totals.PricedRequests++
		totals.ExactCostUsd += priced.USD
		totals.EstimatedCostUsd += priced.USD
	case ConfidenceEstimated:
		totals.PricedRequests++
		totals.EstimatedCostUsd += priced.USD
	case ConfidenceLowerBound:
		totals.PricedRequests++
		totals.LowerBoundCostUsd += priced.USD
	case ConfidenceStale:
		totals.PricedRequests++
		totals.StaleCostUsd += priced.USD
	default:
		totals.UnpricedRequests++
	}
}

func addPricedDayLegacy(day *dayRow, priced ResolvedPrice) {
	if day == nil {
		return
	}
	switch priced.Confidence {
	case ConfidenceExact:
		day.ExactCostUsd += priced.USD
		day.EstimatedCostUsd += priced.USD
	case ConfidenceEstimated:
		day.EstimatedCostUsd += priced.USD
	case ConfidenceLowerBound:
		day.LowerBoundCostUsd += priced.USD
	case ConfidenceStale:
		day.StaleCostUsd += priced.USD
	default:
		day.UnpricedRequests++
	}
}

func addCostBucket(dst *CostBucket, usd float64) {
	dst.AmountUsd += usd
	dst.Requests++
}

func finishCost(s CostSummary) CostSummary {
	s.PricedRequests = s.Exact.Requests + s.Estimated.Requests + s.LowerBound.Requests + s.Stale.Requests
	classes := 0
	status := ""
	if s.Exact.Requests > 0 {
		classes++
		status = string(ConfidenceExact)
	}
	if s.Estimated.Requests > 0 {
		classes++
		status = string(ConfidenceEstimated)
	}
	if s.LowerBound.Requests > 0 {
		classes++
		status = string(ConfidenceLowerBound)
	}
	if s.Stale.Requests > 0 {
		classes++
		status = string(ConfidenceStale)
	}
	switch {
	case classes == 0 && s.UnpricedRequests > 0:
		status = string(ConfidenceUnknown)
	case classes > 1:
		status = "mixed"
	}
	s.Status = status
	s.DisplayTotalSafe = s.PricedRequests > 0 && s.UnpricedRequests == 0 && s.UnmeteredRequests == 0 && classes == 1
	if s.PricedRequests > 0 {
		s.Currency = "USD"
	}
	return s
}

func addOptionalCost(dst **float64, usd float64) {
	if *dst == nil {
		zero := 0.0
		*dst = &zero
	}
	**dst += usd
}

func addInt64(dst **int64, v int64) {
	if *dst == nil {
		zero := int64(0)
		*dst = &zero
	}
	**dst += v
}

func matchFilter(got, want string) bool {
	want = strings.TrimSpace(want)
	if want == "" {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(got), want)
}

func matchSurface(got string, want Surface) bool {
	switch want {
	case SurfaceClaude:
		return got == "claude" || got == "claude-desktop"
	case SurfaceGrok:
		return got == "grok"
	case SurfaceCodex:
		return got == "codex"
	default:
		return true
	}
}

func bump(totals *Totals, status string) {
	totals.Requests++
	switch status {
	case "reported":
		totals.MeasuredRequests++
		totals.ReportedRequests++
	case "estimated":
		totals.MeasuredRequests++
		totals.EstimatedRequests++
	case "unsupported":
		totals.UnsupportedRequests++
	default:
		totals.UnreportedRequests++
	}
}

func addTokens(totals *Totals, item entry) {
	if item.Usage == nil {
		return
	}
	totals.InputTokens += item.Usage.InputTokens
	totals.OutputTokens += item.Usage.OutputTokens
	creation := item.Usage.CacheCreationInputTokens
	read := item.Usage.CacheReadInputTokens
	if read == nil && item.Usage.CachedInputTokens != nil && creation != nil {
		v := *item.Usage.CachedInputTokens - *creation
		if v < 0 {
			v = 0
		}
		read = &v
	} else if read == nil {
		read = item.Usage.CachedInputTokens
	}
	if read != nil {
		addInt64(&totals.CachedInputTokens, *read)
		addInt64(&totals.CacheReadInputTokens, *read)
	}
	if creation != nil {
		addInt64(&totals.CacheCreationInputTokens, *creation)
	}
	if item.Usage.ReasoningOutputTokens != nil {
		totals.ReasoningOutputTokens += *item.Usage.ReasoningOutputTokens
	}
	totals.TotalTokens += displayTokens(item)
}

func displayTokens(item entry) int64 {
	if item.Usage == nil {
		if item.TotalTokens != nil {
			return *item.TotalTokens
		}
		return 0
	}
	sum := item.Usage.InputTokens + item.Usage.OutputTokens
	if item.TotalTokens != nil && *item.TotalTokens > sum {
		return *item.TotalTokens
	}
	return sum
}

func finishModels(in map[string]*modelRow, total int64) []modelRow {
	out := make([]modelRow, 0, len(in))
	for _, row := range in {
		if total > 0 {
			row.ShareRatio = float64(row.TotalTokens) / float64(total)
		}
		row.Cost = finishCost(row.Cost)
		row.CostStatus = row.Cost.Status
		out = append(out, *row)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TotalTokens == out[j].TotalTokens {
			return out[i].Provider+"/"+out[i].Model < out[j].Provider+"/"+out[j].Model
		}
		return out[i].TotalTokens > out[j].TotalTokens
	})
	if len(out) > maxModelRows {
		out = out[:maxModelRows]
	}
	return out
}

func finishAccounts(in map[string]*accountRow) []accountRow {
	out := make([]accountRow, 0, len(in))
	for _, row := range in {
		if row.Requests > 0 {
			row.UsageCoverageRatio = float64(row.MeasuredRequests) / float64(row.Requests)
		}
		row.Cost = finishCost(row.Cost)
		out = append(out, *row)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TotalTokens == out[j].TotalTokens {
			return out[i].Account < out[j].Account
		}
		return out[i].TotalTokens > out[j].TotalTokens
	})
	return out
}

func finishProviders(in map[string]*providerRow, total int64) []providerRow {
	out := make([]providerRow, 0, len(in))
	for _, row := range in {
		if total > 0 {
			row.ShareRatio = float64(row.TotalTokens) / float64(total)
		}
		row.Cost = finishCost(row.Cost)
		row.CostStatus = row.Cost.Status
		out = append(out, *row)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TotalTokens == out[j].TotalTokens {
			return out[i].Provider < out[j].Provider
		}
		return out[i].TotalTokens > out[j].TotalTokens
	})
	return out
}
