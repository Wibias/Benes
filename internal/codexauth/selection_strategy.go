package codexauth

import (
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	UnknownUsageScore          = 101.0
	DefaultAutoSwitchThreshold = 80.0
	defaultStickyLimit         = 1
	maxStickyLimit             = 100
	ResetEvidenceMaxAge        = 30 * time.Minute
)

type PoolStrategy string

const (
	PoolStrategyQuota       PoolStrategy = "quota"
	PoolStrategyRoundRobin  PoolStrategy = "round-robin"
	PoolStrategyFillFirst   PoolStrategy = "fill-first"
	PoolStrategyResetWindow PoolStrategy = "reset-window"
)

type ResetOrder string

const (
	ResetOrderSoonest ResetOrder = "soonest"
	ResetOrderLatest  ResetOrder = "latest"
)

type QuotaSnapshot struct {
	WeeklyPercent  *float64
	MonthlyPercent *float64
	ShortPercent   *float64
	WeeklyResetAt  *float64
	MonthlyResetAt *float64
	ShortResetAt   *float64
	UpdatedAt      time.Time
}

func NormalizePoolStrategy(raw string) PoolStrategy {
	switch PoolStrategy(raw) {
	case PoolStrategyQuota, PoolStrategyRoundRobin, PoolStrategyFillFirst, PoolStrategyResetWindow:
		return PoolStrategy(raw)
	default:
		return PoolStrategyQuota
	}
}

func NormalizeResetOrder(raw string) ResetOrder {
	switch ResetOrder(strings.TrimSpace(raw)) {
	case ResetOrderLatest:
		return ResetOrderLatest
	default:
		return ResetOrderSoonest
	}
}
func NormalizeStickyLimit(raw int) int {
	if raw < 1 || raw > maxStickyLimit {
		return defaultStickyLimit
	}
	return raw
}
func ComputeQuotaUsageScore(q *QuotaSnapshot, plan string) float64 {
	if q == nil {
		return UnknownUsageScore
	}
	if isThirtyDayOnlyPlan(plan) {
		vals := []float64{}
		if finitePercent(q.MonthlyPercent) {
			vals = append(vals, *q.MonthlyPercent)
		}
		if finitePercent(q.ShortPercent) {
			vals = append(vals, *q.ShortPercent)
		}
		if len(vals) == 0 {
			return UnknownUsageScore
		}
		return maxFloat(vals)
	}
	vals := []float64{}
	if finitePercent(q.WeeklyPercent) {
		vals = append(vals, *q.WeeklyPercent)
	}
	if finitePercent(q.MonthlyPercent) {
		vals = append(vals, *q.MonthlyPercent)
	}
	if finitePercent(q.ShortPercent) {
		vals = append(vals, *q.ShortPercent)
	}
	if len(vals) == 0 {
		return UnknownUsageScore
	}
	return maxFloat(vals)
}

func maxFloat(vals []float64) float64 {
	best := vals[0]
	for _, v := range vals[1:] {
		if v > best {
			best = v
		}
	}
	return best
}
func QuotaHasHeadroom(q *QuotaSnapshot, plan string, threshold float64) bool {
	if threshold <= 0 {
		return true
	}
	s := ComputeQuotaUsageScore(q, plan)
	return s == UnknownUsageScore || s < threshold
}
func PickResetWindow(
	ids []string,
	order ResetOrder,
	now time.Time,
	quotaOf func(string) *QuotaSnapshot,
	planOf func(string) string,
	hasHeadroom func(string) bool,
) string {
	if len(ids) == 0 {
		return ""
	}
	headroom := make([]string, 0, len(ids))
	for _, id := range ids {
		if hasHeadroom == nil || hasHeadroom(id) {
			headroom = append(headroom, id)
		}
	}
	ranked := headroom
	if len(ranked) == 0 {
		ranked = ids
	}
	type scored struct {
		id    string
		reset float64
	}
	comparable := make([]scored, 0, len(ranked))
	for _, id := range ranked {
		reset, ok := GoverningReset(quotaOf(id), planOf(id), now)
		if !ok {
			return PickLowestUsage(ranked, quotaOf, planOf)
		}
		comparable = append(comparable, scored{id: id, reset: reset})
	}
	sort.SliceStable(comparable, func(i, j int) bool {
		if comparable[i].reset == comparable[j].reset {
			return comparable[i].id < comparable[j].id
		}
		if NormalizeResetOrder(string(order)) == ResetOrderLatest {
			return comparable[i].reset > comparable[j].reset
		}
		return comparable[i].reset < comparable[j].reset
	})
	return comparable[0].id
}

func GoverningReset(q *QuotaSnapshot, plan string, now time.Time) (float64, bool) {
	if q == nil || q.UpdatedAt.IsZero() || now.Sub(q.UpdatedAt) > ResetEvidenceMaxAge {
		return 0, false
	}
	windows := governingWindows(q, plan)
	if len(windows) == 0 {
		return 0, false
	}
	maxPercent := windows[0].percent
	for _, window := range windows[1:] {
		if window.percent > maxPercent {
			maxPercent = window.percent
		}
	}
	var reset *float64
	for _, window := range windows {
		if window.percent != maxPercent {
			continue
		}
		if window.reset == nil {
			return 0, false
		}
		if reset == nil {
			value := *window.reset
			reset = &value
			continue
		}
		if *window.reset != *reset {
			return 0, false
		}
	}
	if reset == nil {
		return 0, false
	}
	return *reset, true
}

type quotaWindow struct {
	percent float64
	reset   *float64
}

func governingWindows(q *QuotaSnapshot, plan string) []quotaWindow {
	if q == nil {
		return nil
	}
	if isThirtyDayOnlyPlan(plan) {
		return finiteWindows([]quotaWindow{
			{percent: derefPercent(q.MonthlyPercent), reset: q.MonthlyResetAt},
			{percent: derefPercent(q.ShortPercent), reset: q.ShortResetAt},
		})
	}
	return finiteWindows([]quotaWindow{
		{percent: derefPercent(q.WeeklyPercent), reset: q.WeeklyResetAt},
		{percent: derefPercent(q.MonthlyPercent), reset: q.MonthlyResetAt},
		{percent: derefPercent(q.ShortPercent), reset: q.ShortResetAt},
	})
}

func finiteWindows(windows []quotaWindow) []quotaWindow {
	out := make([]quotaWindow, 0, len(windows))
	for _, window := range windows {
		if !math.IsNaN(window.percent) && !math.IsInf(window.percent, 0) {
			out = append(out, window)
		}
	}
	return out
}

func derefPercent(v *float64) float64 {
	if v == nil {
		return math.NaN()
	}
	return *v
}

func PickLowestUsage(ids []string, quotaOf func(string) *QuotaSnapshot, planOf func(string) string) string {
	best := ""
	usage := math.Inf(1)
	for _, id := range ids {
		u := ComputeQuotaUsageScore(quotaOf(id), planOf(id))
		if u < usage {
			best = id
			usage = u
		}
	}
	return best
}
func PickFillFirst(eligible, all []string, active string, head func(string) bool) string {
	if len(eligible) == 0 {
		return ""
	}
	set := map[string]struct{}{}
	for _, id := range eligible {
		set[id] = struct{}{}
	}
	if _, ok := set[active]; active != "" && ok && head(active) {
		return active
	}
	ordered := uniqueSortedASCII(eligible)
	if active == "" {
		return firstWithHeadroomOrFallback(ordered, head)
	}
	stable := uniqueSortedASCII(all)
	start := -1
	for i, id := range stable {
		if id == active {
			start = i
			break
		}
	}
	if start < 0 {
		return firstWithHeadroomOrFallback(ordered, head)
	}
	fallback := ""
	for step := 1; step <= len(stable); step++ {
		c := stable[(start+step)%len(stable)]
		if _, ok := set[c]; !ok {
			continue
		}
		if fallback == "" {
			fallback = c
		}
		if head(c) {
			return c
		}
	}
	if fallback != "" {
		return fallback
	}
	return ordered[0]
}
func isThirtyDayOnlyPlan(plan string) bool {
	k := strings.ToLower(strings.TrimSpace(plan))
	return k == "go" || k == "free"
}
func finitePercent(v *float64) bool { return v != nil && !math.IsNaN(*v) && !math.IsInf(*v, 0) }
func uniqueSortedASCII(v []string) []string {
	seen := map[string]struct{}{}
	r := []string{}
	for _, x := range v {
		if _, ok := seen[x]; ok {
			continue
		}
		seen[x] = struct{}{}
		r = append(r, x)
	}
	sort.Strings(r)
	return r
}
func firstWithHeadroomOrFallback(ids []string, h func(string) bool) string {
	for _, id := range ids {
		if h(id) {
			return id
		}
	}
	if len(ids) == 0 {
		return ""
	}
	return ids[0]
}

type rotationPoolState struct {
	activeKey      string
	successes      int
	currentWeights map[string]int
}
type RotationState struct {
	mu                       sync.Mutex
	pools                    map[string]*rotationPoolState
	lastReconciledGeneration uint64
}

func NewRotationState() *RotationState { return &RotationState{pools: map[string]*rotationPoolState{}} }
func (s *RotationState) PickRoundRobin(k string, e []string, l int) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return pickRoundRobinFromState(e, NormalizeStickyLimit(l), s.getOrCreateLocked(k), true)
}
func (s *RotationState) PeekRoundRobin(k string, e []string, l int) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return pickRoundRobinFromState(e, NormalizeStickyLimit(l), cloneRotationPoolState(s.pools[k]), false)
}
func (s *RotationState) NoteSuccess(k, a string, l int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.pools[k]
	if st == nil {
		return
	}
	limit := NormalizeStickyLimit(l)
	if st.activeKey != a {
		st.activeKey = a
		st.successes = 0
	}
	st.successes++
	if st.successes >= limit {
		st.activeKey = ""
		st.successes = 0
	}
}
func (s *RotationState) NoteFailure(k, a string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.pools[k]
	if st != nil && st.activeKey == a {
		st.activeKey = ""
		st.successes = 0
	}
}
func (s *RotationState) Seed(k, a string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.getOrCreateLocked(k)
	st.activeKey = a
	st.successes = 0
	st.currentWeights = map[string]int{}
}
func (s *RotationState) Clear(k string) { s.mu.Lock(); defer s.mu.Unlock(); delete(s.pools, k) }
func (s *RotationState) Reconcile(g uint64, live map[string]struct{}) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if g <= s.lastReconciledGeneration {
		return 0
	}
	removed := 0
	for k, st := range s.pools {
		if len(live) == 0 {
			delete(s.pools, k)
			removed++
			continue
		}
		if st.activeKey != "" {
			if _, ok := live[st.activeKey]; !ok {
				st.activeKey = ""
				st.successes = 0
				removed++
			}
		}
		for id := range st.currentWeights {
			if _, ok := live[id]; !ok {
				delete(st.currentWeights, id)
				removed++
			}
		}
	}
	s.lastReconciledGeneration = g
	return removed
}
func (s *RotationState) getOrCreateLocked(k string) *rotationPoolState {
	st := s.pools[k]
	if st == nil {
		st = &rotationPoolState{currentWeights: map[string]int{}}
		s.pools[k] = st
	}
	return st
}
func cloneRotationPoolState(src *rotationPoolState) *rotationPoolState {
	c := &rotationPoolState{currentWeights: map[string]int{}}
	if src == nil {
		return c
	}
	c.activeKey = src.activeKey
	c.successes = src.successes
	for id, w := range src.currentWeights {
		c.currentWeights[id] = w
	}
	return c
}
func pickRoundRobinFromState(e []string, l int, st *rotationPoolState, commit bool) string {
	if len(e) == 0 {
		return ""
	}
	if st.activeKey != "" && containsString(e, st.activeKey) {
		return st.activeKey
	}
	if st.activeKey != "" {
		st.activeKey = ""
		st.successes = 0
	}
	i := smoothWeightedIndex(e, st)
	if i < 0 {
		return ""
	}
	p := e[i]
	if commit && l > 1 {
		st.activeKey = p
		st.successes = 0
	}
	return p
}
func smoothWeightedIndex(ids []string, st *rotationPoolState) int {
	best := -1
	bestScore := math.MinInt
	total := 0
	for i, id := range ids {
		score := st.currentWeights[id] + 1
		st.currentWeights[id] = score
		total++
		if score > bestScore {
			best = i
			bestScore = score
		}
	}
	if best >= 0 {
		key := ids[best]
		st.currentWeights[key] -= total
	}
	return best
}
func containsString(v []string, t string) bool {
	for _, x := range v {
		if x == t {
			return true
		}
	}
	return false
}
