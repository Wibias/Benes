package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/quota"
)

const (
	policyStickyTTL    = 24 * time.Hour
	policyStickyMax    = 4096
	policyLatencyAlpha = 0.3
	policyAnalyticsCap = 500
)

type policyRequestSample struct {
	ProfileID  string
	Member     string
	DurationMs int
	Success    bool
	Hops       int
	At         time.Time
}

type policyRuntime struct {
	mu        sync.Mutex
	latency   map[string]float64
	success   map[string]int
	fail      map[string]int
	sticky    map[string]policyStickyBinding
	requests  []policyRequestSample
	truncated bool
}

type policyStickyBinding struct {
	Member string
	At     time.Time
}

func newPolicyRuntime() *policyRuntime {
	return &policyRuntime{
		latency:  map[string]float64{},
		success:  map[string]int{},
		fail:     map[string]int{},
		sticky:   map[string]policyStickyBinding{},
		requests: make([]policyRequestSample, 0, 16),
	}
}

func (r *policyRuntime) observeOpen(memberID string, d time.Duration, ok bool) {
	if r == nil || memberID == "" {
		return
	}
	ms := float64(d.Milliseconds())
	if ms < 1 {
		ms = 1
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if prev, exists := r.latency[memberID]; exists {
		r.latency[memberID] = prev*(1-policyLatencyAlpha) + ms*policyLatencyAlpha
	} else {
		r.latency[memberID] = ms
	}
	if ok {
		r.success[memberID]++
	} else {
		r.fail[memberID]++
	}
}

func (r *policyRuntime) bind(keys []string, member string) {
	if r == nil || member == "" {
		return
	}
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pruneStickyLocked(now)
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		r.sticky[key] = policyStickyBinding{Member: member, At: now}
	}
}

func (r *policyRuntime) lookup(keys []string) string {
	if r == nil {
		return ""
	}
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pruneStickyLocked(now)
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if binding, ok := r.sticky[key]; ok {
			return binding.Member
		}
	}
	return ""
}

func (r *policyRuntime) recordRequest(sample policyRequestSample) {
	if r == nil {
		return
	}
	if sample.DurationMs < 1 {
		sample.DurationMs = 1
	}
	if sample.Hops < 1 {
		sample.Hops = 1
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.requests) >= policyAnalyticsCap {
		r.requests = append(r.requests[:0], r.requests[1:]...)
		r.truncated = true
	}
	r.requests = append(r.requests, sample)
}

func (r *policyRuntime) analyticsSnapshot(profile string) map[string]any {
	if r == nil {
		return emptyRoutingAnalytics()
	}
	r.mu.Lock()
	samples := append([]policyRequestSample(nil), r.requests...)
	truncated := r.truncated
	r.mu.Unlock()
	return buildRoutingAnalytics(samples, profile, truncated)
}

func buildRoutingAnalytics(samples []policyRequestSample, profile string, truncated bool) map[string]any {
	profile = strings.TrimSpace(profile)
	filtered := make([]policyRequestSample, 0, len(samples))
	for _, sample := range samples {
		if profile != "" && sample.ProfileID != profile {
			continue
		}
		filtered = append(filtered, sample)
	}
	n := len(filtered)
	if n == 0 {
		out := emptyRoutingAnalytics()
		out["historyTruncated"] = truncated
		return out
	}
	ok := 0
	fallback := 0
	failed := 0
	durations := make([]int, 0, n)
	type memberAgg struct {
		provider string
		model    string
		requests int
		ok       int
		ms       []int
	}
	byMember := map[string]*memberAgg{}
	for _, sample := range filtered {
		durations = append(durations, sample.DurationMs)
		if sample.Success {
			ok++
		} else {
			failed++
		}
		if sample.Hops > 1 {
			fallback++
		}
		if sample.Member == "" {
			continue
		}
		agg, exists := byMember[sample.Member]
		if !exists {
			provider, model := splitPolicyMember(sample.Member)
			agg = &memberAgg{provider: provider, model: model}
			byMember[sample.Member] = agg
		}
		agg.requests++
		if sample.Success {
			agg.ok++
		}
		agg.ms = append(agg.ms, sample.DurationMs)
	}
	duration := durationPercentiles(durations)
	first := durationPercentiles(durations)
	first["coverage"] = 1.0
	breakdown := make([]map[string]any, 0, len(byMember))
	for _, agg := range byMember {
		row := map[string]any{
			"provider":    agg.provider,
			"model":       agg.model,
			"requests":    agg.requests,
			"successRate": float64(agg.ok) / float64(agg.requests),
		}
		if stats := durationPercentiles(agg.ms); stats["p50"] != nil {
			row["p50DurationMs"] = stats["p50"]
		}
		breakdown = append(breakdown, row)
	}
	sort.Slice(breakdown, func(i, j int) bool {
		ri, _ := breakdown[i]["requests"].(int)
		rj, _ := breakdown[j]["requests"].(int)
		if ri != rj {
			return ri > rj
		}
		pi, _ := breakdown[i]["provider"].(string)
		pj, _ := breakdown[j]["provider"].(string)
		if pi != pj {
			return pi < pj
		}
		mi, _ := breakdown[i]["model"].(string)
		mj, _ := breakdown[j]["model"].(string)
		return mi < mj
	})
	return map[string]any{
		"totalRequests":              n,
		"successRate":                float64(ok) / float64(n),
		"fallbackRate":               float64(fallback) / float64(n),
		"confidence":                 analyticsConfidence(n),
		"historyTruncated":           truncated,
		"cooldownTriggeringFailures": failed,
		"durationMs":                 duration,
		"firstOutputMs":              first,
		"breakdown":                  breakdown,
	}
}

func splitPolicyMember(member string) (provider, model string) {
	provider, model, ok := strings.Cut(member, "/")
	if !ok {
		return member, ""
	}
	return provider, model
}

func durationPercentiles(ms []int) map[string]any {
	out := map[string]any{"sampleCount": len(ms)}
	if len(ms) == 0 {
		return out
	}
	sorted := append([]int(nil), ms...)
	sort.Ints(sorted)
	out["p50"] = percentileNearestRank(sorted, 0.50)
	out["p95"] = percentileNearestRank(sorted, 0.95)
	out["p99"] = percentileNearestRank(sorted, 0.99)
	return out
}

func percentileNearestRank(sorted []int, p float64) int {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	rank := int(math.Ceil(p * float64(n)))
	if rank < 1 {
		rank = 1
	}
	if rank > n {
		rank = n
	}
	return sorted[rank-1]
}

func analyticsConfidence(n int) string {
	if n >= 50 {
		return "high"
	}
	if n >= 10 {
		return "medium"
	}
	return "low"
}

func (r *policyRuntime) snapshotLatency() map[string]float64 {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.latency) == 0 {
		return nil
	}
	out := make(map[string]float64, len(r.latency))
	for id, ms := range r.latency {
		out[id] = ms
	}
	return out
}

func (r *policyRuntime) snapshotHealth() map[string]float64 {
	if r == nil {
		return map[string]float64{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := map[string]float64{}
	seen := map[string]struct{}{}
	for id := range r.success {
		seen[id] = struct{}{}
	}
	for id := range r.fail {
		seen[id] = struct{}{}
	}
	for id := range seen {
		ok := r.success[id]
		bad := r.fail[id]
		total := ok + bad
		if total <= 0 {
			continue
		}
		out[id] = float64(ok) / float64(total)
	}
	return out
}

func (r *policyRuntime) pruneStickyLocked(now time.Time) {
	for key, binding := range r.sticky {
		if now.Sub(binding.At) > policyStickyTTL {
			delete(r.sticky, key)
		}
	}
	if len(r.sticky) <= policyStickyMax {
		return
	}
	oldestKey := ""
	var oldest time.Time
	for key, binding := range r.sticky {
		if oldestKey == "" || binding.At.Before(oldest) {
			oldestKey = key
			oldest = binding.At
		}
	}
	if oldestKey != "" {
		delete(r.sticky, oldestKey)
	}
}

func policyStickyKeys(profileID string, evidence policyRequestEvidence) []string {
	keys := make([]string, 0, 4)
	if evidence.PreviousResponseID != "" {
		keys = append(keys, "resp:"+evidence.PreviousResponseID)
	}
	if evidence.PromptCacheKey != "" {
		keys = append(keys, "cache:"+evidence.PromptCacheKey)
	}
	if evidence.User != "" {
		keys = append(keys, "user:"+evidence.User)
	}
	if profileID != "" && evidence.FirstUserText != "" {
		sum := sha256.Sum256([]byte(profileID + "\x00" + evidence.FirstUserText))
		keys = append(keys, "conv:"+profileID+":"+hex.EncodeToString(sum[:8]))
	}
	return keys
}

func (h *handler) policySignals() policySignals {
	signals := policySignals{
		QuotaRemaining: map[string]float64{},
		Health:         map[string]float64{},
	}
	if h == nil {
		return signals
	}
	if h.policyRuntime != nil {
		signals.LatencyMs = h.policyRuntime.snapshotLatency()
		signals.Health = h.policyRuntime.snapshotHealth()
	}
	if signals.Health == nil {
		signals.Health = map[string]float64{}
	}
	for id := range h.disabledProviderIDs() {
		signals.Health[id] = 0
	}
	reports := []quota.Report{}
	if h.quotaStore != nil {
		reports = append(reports, h.quotaStore.Snapshot().Reports...)
	}
	if h.codexQuota != nil {
		reports = mergeQuotaReports(reports, h.codexQuota())
	}
	best := map[string]float64{}
	for _, report := range reports {
		remaining, ok := quotaRemainingFraction(report.Quota)
		if !ok {
			continue
		}
		if prev, exists := best[report.Provider]; !exists || remaining > prev {
			best[report.Provider] = remaining
		}
	}
	signals.QuotaRemaining = best
	return signals
}

func (h *handler) disabledProviderIDs() map[string]bool {
	out := map[string]bool{}
	if h == nil || strings.TrimSpace(h.configPath) == "" {
		return out
	}
	disk, err := config.LoadDiskConfig(h.configPath, 0)
	if err != nil {
		return out
	}
	for id, raw := range disk.Providers {
		var body struct {
			Disabled bool `json:"disabled"`
		}
		if json.Unmarshal(raw, &body) != nil || !body.Disabled {
			continue
		}
		out[id] = true
	}
	return out
}

func quotaRemainingFraction(q quota.Quota) (float64, bool) {
	parts := make([]float64, 0, 4)
	addUsed := func(percent *float64) {
		if percent == nil {
			return
		}
		remaining := (100 - *percent) / 100
		if remaining < 0 {
			remaining = 0
		}
		if remaining > 1 {
			remaining = 1
		}
		parts = append(parts, remaining)
	}
	addUsed(q.FiveHourPercent)
	addUsed(q.WeeklyPercent)
	addUsed(q.MonthlyPercent)
	for _, window := range q.CustomWindows {
		remaining := (100 - window.Percent) / 100
		if remaining < 0 {
			remaining = 0
		}
		if remaining > 1 {
			remaining = 1
		}
		parts = append(parts, remaining)
	}
	if q.CreditsUsd != nil {
		if q.CreditsUsd.Unlimited {
			parts = append(parts, 1)
		} else if q.CreditsUsd.Limit > 0 {
			remaining := q.CreditsUsd.Remaining / q.CreditsUsd.Limit
			if remaining < 0 {
				remaining = 0
			}
			if remaining > 1 {
				remaining = 1
			}
			parts = append(parts, remaining)
		}
	}
	if len(parts) == 0 {
		return 0, false
	}
	min := parts[0]
	for _, part := range parts[1:] {
		if part < min {
			min = part
		}
	}
	return min, true
}
