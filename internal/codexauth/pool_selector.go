package codexauth

import (
	"sync"
	"time"
)

type PoolSelectionStatus string

const (
	PoolSelectionNone     PoolSelectionStatus = "none"
	PoolSelectionSelected PoolSelectionStatus = "selected"
	PoolSelectionExpired  PoolSelectionStatus = "expired"
)

type PoolSelectionReason string

const (
	PoolSelectionAffinity           PoolSelectionReason = "affinity"
	PoolSelectionQuotaReevaluation  PoolSelectionReason = "quota-reevaluation"
	PoolSelectionRoundRobin         PoolSelectionReason = "round-robin"
	PoolSelectionFillFirst          PoolSelectionReason = "fill-first"
	PoolSelectionResetWindow        PoolSelectionReason = "reset-window"
	PoolSelectionInitialQuota       PoolSelectionReason = "initial-quota"
	PoolSelectionFallback           PoolSelectionReason = "fallback"
	PoolSelectionPriorityPreemption PoolSelectionReason = "priority-preemption"
	PoolSelectionQuotaSwitch        PoolSelectionReason = "quota-switch"
	PoolSelectionFailureFailover    PoolSelectionReason = "failure-failover"
	PoolSelectionDegradedActive     PoolSelectionReason = "degraded-active"
)

type PoolSelectionInput struct {
	Accounts            ManagedAccountConfig
	Credentials         ManagedCredentialSnapshot
	Main                MainCredentialResult
	IncludeMain         bool
	MainPlan            string
	Strategy            PoolStrategy
	ResetOrder          ResetOrder
	StickyLimit         int
	AutoSwitchThreshold *float64
	FailoverThreshold   *int
	ThreadID            string
	Scope               QuotaScope
	ExcludeAccountID    string
	Now                 time.Time
	Quotas              map[string]*QuotaSnapshot
}

type PoolSelectionResult struct {
	Status          PoolSelectionStatus
	Reason          PoolSelectionReason
	AccountID       string
	Generation      int64
	GenerationKnown bool
	Degraded        bool
	RequiresProbe   bool
	ActiveChanged   bool
	PersistActive   bool
	PinReleased     bool
}

type PoolSelectorDependencies struct {
	Reauth    *ReauthState
	Health    *HealthState
	Failures  *FailureState
	Affinity  *ThreadAffinityState
	Rotation  *RotationState
	MainFence *MainPhysicalFence
}

type PoolSelector struct {
	Reauth        *ReauthState
	Health        *HealthState
	Failures      *FailureState
	Affinity      *ThreadAffinityState
	Rotation      *RotationState
	MainFence     *MainPhysicalFence
	mu            sync.Mutex
	runtimeActive string
}

func NewPoolSelector(deps PoolSelectorDependencies) *PoolSelector {
	if deps.Reauth == nil {
		deps.Reauth = NewReauthState()
	}
	if deps.Health == nil {
		deps.Health = NewHealthState()
	}
	if deps.Failures == nil {
		deps.Failures = NewFailureState()
	}
	if deps.Affinity == nil {
		deps.Affinity = NewThreadAffinityState()
	}
	if deps.Rotation == nil {
		deps.Rotation = NewRotationState()
	}
	if deps.MainFence == nil {
		deps.MainFence = NewMainPhysicalFence(deps.Health, deps.Failures, deps.Reauth, deps.Affinity)
	}
	return &PoolSelector{
		Reauth: deps.Reauth, Health: deps.Health, Failures: deps.Failures, Affinity: deps.Affinity, Rotation: deps.Rotation,
		MainFence: deps.MainFence,
	}
}

func (s *PoolSelector) observeMainIdentity(input PoolSelectionInput) {
	if input.IncludeMain {
		s.MainFence.Bind(input.Main.Identity)
	}
}

func (s *PoolSelector) RuntimeActive() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runtimeActive
}

func (s *PoolSelector) Resolve(input PoolSelectionInput) PoolSelectionResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	if input.Now.IsZero() {
		input.Now = time.Now()
	}
	s.observeMainIdentity(input)
	strategy := NormalizePoolStrategy(string(input.Strategy))
	threshold := selectionThreshold(input.AutoSwitchThreshold)
	failoverThreshold := selectionFailoverThreshold(input.FailoverThreshold)
	pinned, pinReleased := s.effectivePin(input, threshold)
	scope := affinityScopeForQuota(input.Scope)

	if input.ThreadID != "" {
		affinity := s.Affinity.Resolve(input.ThreadID, scope, input.Now, input.Credentials)
		switch affinity.Status {
		case AffinityExpired:
			if affinity.AccountID != input.ExcludeAccountID {
				return PoolSelectionResult{Status: PoolSelectionExpired, AccountID: affinity.AccountID, PinReleased: pinReleased}
			}
			s.Affinity.ClearThread(input.ThreadID, scope)
		case AffinitySelected:
			if s.selectable(input, affinity.AccountID) && !s.Failures.ShouldFailover(affinity.AccountID, input.Now, failoverThreshold) {
				if strategy == PoolStrategyQuota {
					usage := s.usage(input, affinity.AccountID)
					overThreshold := threshold > 0 && usage < UnknownUsageScore && usage >= threshold
					if AffinityReevaluationDue(affinity, input.Now, overThreshold) {
						s.Affinity.MarkReevaluated(input.ThreadID, scope, affinity.AccountID, affinity.Generation, input.Now)
						if overThreshold {
							if best := s.pickLowerUsage(input, affinity.AccountID, usage, pinned); best != affinity.AccountID {
								changed := s.rememberActive(input, best)
								s.Affinity.Bind(input.ThreadID, best, scope, input.Now, input.Credentials)
								return s.selectedResult(input, best, PoolSelectionQuotaReevaluation, changed, strategy, pinReleased)
							}
						}
					}
				}
				return PoolSelectionResult{
					Status:          PoolSelectionSelected,
					Reason:          PoolSelectionAffinity,
					AccountID:       affinity.AccountID,
					Generation:      affinity.Generation,
					GenerationKnown: true,
					PinReleased:     pinReleased,
				}
			}
			s.Affinity.ClearThread(input.ThreadID, scope)
		}
	}

	if strategy == PoolStrategyRoundRobin {
		eligible := s.tierCandidates(input, input.ExcludeAccountID, pinned, threshold)
		if picked := s.Rotation.PickRoundRobin(poolKeyForQuota(input.Scope), eligible, input.StickyLimit); picked != "" {
			changed := s.rememberActive(input, picked)
			if input.ThreadID != "" {
				s.Affinity.Bind(input.ThreadID, picked, scope, input.Now, input.Credentials)
			}
			s.Rotation.NoteSuccess(poolKeyForQuota(input.Scope), picked, input.StickyLimit)
			return s.selectedResult(input, picked, PoolSelectionRoundRobin, changed, strategy, pinReleased)
		}
	}
	if strategy == PoolStrategyFillFirst {
		eligible := s.tierCandidates(input, input.ExcludeAccountID, pinned, threshold)
		active := s.effectiveActive(input)
		picked := PickFillFirst(eligible, s.allConfiguredIDs(input, active), active, func(id string) bool {
			return s.headroom(input, id, threshold)
		})
		if picked != "" {
			changed := s.rememberActive(input, picked)
			if input.ThreadID != "" {
				s.Affinity.Bind(input.ThreadID, picked, scope, input.Now, input.Credentials)
			}
			return s.selectedResult(input, picked, PoolSelectionFillFirst, changed, strategy, pinReleased)
		}
	}
	if strategy == PoolStrategyResetWindow {
		eligible := s.tierCandidates(input, input.ExcludeAccountID, pinned, threshold)
		picked := s.pickResetWindow(input, eligible, threshold)
		if picked != "" {
			changed := s.rememberActive(input, picked)
			if input.ThreadID != "" {
				s.Affinity.Bind(input.ThreadID, picked, scope, input.Now, input.Credentials)
			}
			return s.selectedResult(input, picked, PoolSelectionResetWindow, changed, strategy, pinReleased)
		}
	}

	active := s.effectiveActive(input)
	reason := PoolSelectionAffinity
	activeChanged := false
	persistActive := false
	if active == "" {
		active = s.pickLowest(input, input.ExcludeAccountID, pinned, threshold)
		if active == "" {
			if degraded := s.pickDegraded(input, input.ExcludeAccountID); degraded != "" {
				return s.degradedResult(input, degraded, pinReleased)
			}
			return PoolSelectionResult{Status: PoolSelectionNone, PinReleased: pinReleased}
		}
		changed := s.rememberActive(input, active)
		activeChanged = activeChanged || changed
		persistActive = persistActive || (changed && !independentQuotaScope(input.Scope))
		reason = PoolSelectionInitialQuota
	}

	if !s.selectable(input, active) {
		if fallback := s.pickLowest(input, active, pinned, threshold); fallback != "" {
			active = fallback
			changed := s.rememberActive(input, active)
			activeChanged = activeChanged || changed
			persistActive = persistActive || (changed && !independentQuotaScope(input.Scope))
			reason = PoolSelectionFallback
		} else if s.configured(input, active) && !input.Accounts.PausedAccountIDs[active] {
			return s.degradedResult(input, active, pinReleased)
		} else {
			return PoolSelectionResult{Status: PoolSelectionNone, PinReleased: pinReleased}
		}
	}

	if preempted := s.pickPriorityPreemption(input, active, pinned, threshold); preempted != "" {
		active = preempted
		changed := s.rememberActive(input, active)
		activeChanged = activeChanged || changed
		reason = PoolSelectionPriorityPreemption
	}

	if threshold > 0 {
		usage := s.usage(input, active)
		if usage < UnknownUsageScore && usage >= threshold {
			if best := s.pickLowerUsage(input, active, usage, pinned); best != active {
				active = best
				changed := s.rememberActive(input, active)
				activeChanged = activeChanged || changed
				persistActive = persistActive || (changed && !independentQuotaScope(input.Scope))
				reason = PoolSelectionQuotaSwitch
			}
		}
	}

	if s.Failures.ShouldFailover(active, input.Now, failoverThreshold) {
		if best := s.pickLowest(input, active, pinned, threshold); best != "" {
			active = best
			changed := s.rememberActive(input, active)
			activeChanged = activeChanged || changed
			persistActive = persistActive || (changed && !independentQuotaScope(input.Scope))
			reason = PoolSelectionFailureFailover
		}
	}

	if !s.durablyUsable(input, active) {
		if s.configured(input, active) {
			return s.degradedResult(input, active, pinReleased)
		}
		return PoolSelectionResult{Status: PoolSelectionNone, PinReleased: pinReleased}
	}
	if input.Accounts.PausedAccountIDs[active] {
		return PoolSelectionResult{Status: PoolSelectionNone, PinReleased: pinReleased}
	}
	if s.hardBlocked(input, active) || s.softBlocked(input, active) || s.Reauth.Needs(active) {
		return s.degradedResult(input, active, pinReleased)
	}
	if input.ThreadID != "" {
		s.Affinity.Bind(input.ThreadID, active, scope, input.Now, input.Credentials)
	}
	result := s.selectedResult(input, active, reason, activeChanged, PoolStrategyQuota, pinReleased)
	result.PersistActive = persistActive
	return result
}

func (s *PoolSelector) selectedResult(input PoolSelectionInput, id string, reason PoolSelectionReason, changed bool, strategy PoolStrategy, pinReleased bool) PoolSelectionResult {
	gen, known := selectionGeneration(input, id)
	return PoolSelectionResult{Status: PoolSelectionSelected, Reason: reason, AccountID: id, Generation: gen, GenerationKnown: known, ActiveChanged: changed, PersistActive: changed && strategy == PoolStrategyQuota && !independentQuotaScope(input.Scope), PinReleased: pinReleased}
}
func (s *PoolSelector) degradedResult(input PoolSelectionInput, id string, pinReleased bool) PoolSelectionResult {
	gen, known := selectionGeneration(input, id)
	return PoolSelectionResult{Status: PoolSelectionSelected, Reason: PoolSelectionDegradedActive, AccountID: id, Generation: gen, GenerationKnown: known, Degraded: true, RequiresProbe: s.hardBlocked(input, id), PinReleased: pinReleased}
}
func (s *PoolSelector) baseCandidates(input PoolSelectionInput, exclude string) []string {
	ids := StaticEligibleAccountIDs(input.Accounts, input.Credentials, input.Main, StaticEligibilityOptions{IncludeMain: input.IncludeMain, ExcludeAccountID: input.ExcludeAccountID})
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == exclude || s.Reauth.Needs(id) || s.hardBlocked(input, id) || s.softBlocked(input, id) {
			continue
		}
		out = append(out, id)
	}
	return out
}
func (s *PoolSelector) tierCandidates(input PoolSelectionInput, exclude, pinned string, threshold float64) []string {
	ids := s.baseCandidates(input, exclude)
	return SelectPriorityTier(ids, func(id string) int { return AccountPriority(input.Accounts, id) }, func(id string) bool { return s.headroom(input, id, threshold) }, pinned)
}
func (s *PoolSelector) selectable(input PoolSelectionInput, id string) bool {
	return id != input.ExcludeAccountID && s.durablyUsable(input, id) && !input.Accounts.PausedAccountIDs[id] && !s.Reauth.Needs(id) && !s.hardBlocked(input, id) && !s.softBlocked(input, id)
}
func (s *PoolSelector) durablyUsable(input PoolSelectionInput, id string) bool {
	if id == MainAccountID {
		return input.IncludeMain && input.Main.Selectable()
	}
	for _, a := range input.Accounts.Accounts {
		if a.ID == id && IsSelectableManagedAccount(a) {
			r, ok := input.Credentials.Records[id]
			return input.Credentials.Status == ManagedCredentialStoreOK && ok && r.Credential != nil && r.DeletedAtMS == nil
		}
	}
	return false
}
func (s *PoolSelector) configured(input PoolSelectionInput, id string) bool {
	if id == MainAccountID {
		return input.IncludeMain && input.Main.Selectable()
	}
	for _, a := range input.Accounts.Accounts {
		if a.ID == id && IsSelectableManagedAccount(a) {
			return true
		}
	}
	return false
}
func (s *PoolSelector) hardBlocked(input PoolSelectionInput, id string) bool {
	if _, ok := s.Health.HardCooldown(id, input.Now); ok {
		return true
	}
	if input.Scope != "" {
		if _, ok := s.Health.ScopedHardCooldown(id, input.Scope, input.Now); ok {
			return true
		}
	}
	return false
}
func (s *PoolSelector) softBlocked(input PoolSelectionInput, id string) bool {
	_, ok := s.Health.SoftAvoidUntil(id, input.Now)
	return ok
}
func (s *PoolSelector) effectivePin(input PoolSelectionInput, threshold float64) (string, bool) {
	p := input.Accounts.PinnedAccountID
	if p == "" {
		return "", false
	}
	if !s.durablyUsable(input, p) || input.Accounts.PausedAccountIDs[p] || s.Reauth.Needs(p) || !s.headroom(input, p, threshold) {
		return "", true
	}
	return p, false
}
func (s *PoolSelector) headroom(input PoolSelectionInput, id string, threshold float64) bool {
	return QuotaHasHeadroom(input.Quotas[id], s.plan(input, id), threshold)
}
func (s *PoolSelector) usage(input PoolSelectionInput, id string) float64 {
	return ComputeQuotaUsageScore(input.Quotas[id], s.plan(input, id))
}
func (s *PoolSelector) plan(input PoolSelectionInput, id string) string {
	if id == MainAccountID {
		return input.MainPlan
	}
	for _, a := range input.Accounts.Accounts {
		if a.ID == id && IsSelectableManagedAccount(a) {
			return a.Plan
		}
	}
	return ""
}

func (s *PoolSelector) pickResetWindow(input PoolSelectionInput, eligible []string, threshold float64) string {
	return PickResetWindow(eligible, input.ResetOrder, input.Now, func(id string) *QuotaSnapshot {
		return input.Quotas[id]
	}, func(id string) string {
		return s.plan(input, id)
	}, func(id string) bool {
		return s.headroom(input, id, threshold)
	})
}

func (s *PoolSelector) pickDegraded(input PoolSelectionInput, exclude string) string {
	for _, id := range StaticEligibleAccountIDs(input.Accounts, input.Credentials, input.Main, StaticEligibilityOptions{IncludeMain: input.IncludeMain, ExcludeAccountID: exclude}) {
		if id == exclude || input.Accounts.PausedAccountIDs[id] || !s.configured(input, id) {
			continue
		}
		if s.hardBlocked(input, id) {
			return id
		}
	}
	return ""
}
func (s *PoolSelector) pickLowerUsage(input PoolSelectionInput, active string, activeUsage float64, pinned string) string {
	threshold := selectionThreshold(input.AutoSwitchThreshold)
	best := active
	bestUsage := activeUsage
	for _, id := range s.tierCandidates(input, active, pinned, threshold) {
		u := s.usage(input, id)
		if u < bestUsage {
			best = id
			bestUsage = u
		}
	}
	return best
}
func (s *PoolSelector) pickPriorityPreemption(input PoolSelectionInput, active, pinned string, threshold float64) string {
	e := s.tierCandidates(input, "", pinned, threshold)
	if len(e) == 0 || containsString(e, active) {
		return ""
	}
	if pinned != "" && containsString(e, pinned) && s.headroom(input, pinned, threshold) {
		return ""
	}
	if AccountPriority(input.Accounts, e[0]) <= AccountPriority(input.Accounts, active) {
		return ""
	}
	h := []string{}
	for _, id := range e {
		if s.headroom(input, id, threshold) {
			h = append(h, id)
		}
	}
	return PickLowestUsage(h, func(id string) *QuotaSnapshot { return input.Quotas[id] }, func(id string) string { return s.plan(input, id) })
}
func (s *PoolSelector) effectiveActive(input PoolSelectionInput) string {
	if s.runtimeActive != "" {
		if s.runtimeActive == input.ExcludeAccountID {
			return ""
		}
		if s.configured(input, s.runtimeActive) {
			return s.runtimeActive
		}
		s.runtimeActive = ""
	}
	if input.Accounts.ActiveAccountID == input.ExcludeAccountID {
		return ""
	}
	return input.Accounts.ActiveAccountID
}
func (s *PoolSelector) rememberActive(input PoolSelectionInput, id string) bool {
	if independentQuotaScope(input.Scope) {
		return false
	}
	changed := s.effectiveActive(input) != id
	s.runtimeActive = id
	return changed
}
func (s *PoolSelector) allConfiguredIDs(input PoolSelectionInput, active string) []string {
	ids := []string{}
	if (input.IncludeMain && input.Main.Selectable()) || active == MainAccountID {
		ids = append(ids, MainAccountID)
	}
	for _, a := range input.Accounts.Accounts {
		if !a.IsMain {
			ids = append(ids, a.ID)
		}
	}
	return ids
}
func selectionGeneration(input PoolSelectionInput, id string) (int64, bool) {
	if id == MainAccountID {
		if input.IncludeMain && input.Main.Selectable() {
			return 0, true
		}
		return 0, false
	}
	if input.Credentials.Status != ManagedCredentialStoreOK {
		return 0, false
	}
	r, ok := input.Credentials.Records[id]
	if !ok || r.Credential == nil || r.DeletedAtMS != nil {
		return 0, false
	}
	return r.Generation, true
}
func selectionThreshold(v *float64) float64 {
	if v == nil {
		return DefaultAutoSwitchThreshold
	}
	return *v
}
func selectionFailoverThreshold(v *int) int {
	if v == nil {
		return 3
	}
	return *v
}
func affinityScopeForQuota(scope QuotaScope) AffinityScope {
	switch scope {
	case QuotaScopeShared:
		return AffinityScopeShared
	case QuotaScopeSpark:
		return AffinityScopeSpark
	default:
		return AffinityScopeLegacy
	}
}
func poolKeyForQuota(scope QuotaScope) string {
	if scope == "" {
		return "codex"
	}
	return "codex:" + string(scope)
}
func independentQuotaScope(scope QuotaScope) bool { return scope == QuotaScopeSpark }
