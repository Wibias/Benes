package codexauth

import "time"

const PoolSelectionAlternate PoolSelectionReason = "alternate"

// PickAlternate chooses one account for the current upstream retry only.
// It deliberately skips thread affinity and active-account commits. Round-robin
// still advances its ring because the alternate picker does the
// same when choosing the next request-local account.
func (s *PoolSelector) PickAlternate(input PoolSelectionInput) PoolSelectionResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	if input.Now.IsZero() {
		input.Now = time.Now()
	}
	s.observeMainIdentity(input)
	excluded := input.ExcludeAccountID
	if excluded == "" {
		return PoolSelectionResult{Status: PoolSelectionNone}
	}
	threshold := selectionThreshold(input.AutoSwitchThreshold)
	pinned, _ := s.effectivePin(input, threshold)
	strategy := NormalizePoolStrategy(string(input.Strategy))

	var picked string
	switch strategy {
	case PoolStrategyRoundRobin:
		eligible := s.tierCandidates(input, excluded, pinned, threshold)
		picked = s.Rotation.PickRoundRobin(poolKeyForQuota(input.Scope), eligible, input.StickyLimit)
	case PoolStrategyFillFirst:
		eligible := s.tierCandidates(input, excluded, pinned, threshold)
		picked = PickFillFirst(eligible, s.allConfiguredIDs(input, excluded), excluded, func(id string) bool {
			return s.headroom(input, id, threshold)
		})
	case PoolStrategyResetWindow:
		eligible := s.tierCandidates(input, excluded, pinned, threshold)
		picked = s.pickResetWindow(input, eligible, threshold)
	default:
		picked = s.pickLowest(input, excluded, pinned, threshold)
	}
	if picked == "" {
		return PoolSelectionResult{Status: PoolSelectionNone}
	}
	return s.selectedResult(input, picked, PoolSelectionAlternate, false, strategy, false)
}

// PromoteQuotaAlternate applies the active-account move only after the rejected
// quota outcome has been recorded. It never binds thread affinity. If another
// request moved the active account in the meantime, or the request used an
// independent quota scope, the stale retry cannot overwrite that newer state.
func (s *PoolSelector) PromoteQuotaAlternate(input PoolSelectionInput, rejectedAccountID, alternateAccountID string) bool {
	if rejectedAccountID == "" || alternateAccountID == "" || rejectedAccountID == alternateAccountID || independentQuotaScope(input.Scope) {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	active := s.runtimeActive
	if active == "" {
		active = input.Accounts.ActiveAccountID
	}
	if active != rejectedAccountID {
		return false
	}
	s.runtimeActive = alternateAccountID
	return true
}

func (r *PoolCredentialResolver) PromoteQuotaAlternate(input PoolSelectionInput, rejectedAccountID, alternateAccountID string) bool {
	return r.selector.PromoteQuotaAlternate(input, rejectedAccountID, alternateAccountID)
}
