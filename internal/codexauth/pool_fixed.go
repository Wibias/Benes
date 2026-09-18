package codexauth

import "time"

const PoolSelectionFixed PoolSelectionReason = "fixed"

// ResolveFixed evaluates one exact account without applying Pool strategy,
// thread affinity, rotation, recovery probes, or active-account changes.
func (s *PoolSelector) ResolveFixed(input PoolSelectionInput, accountID string) PoolSelectionResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	if input.Now.IsZero() {
		input.Now = time.Now()
	}
	s.observeMainIdentity(input)
	if accountID == "" || !s.durablyUsable(input, accountID) || input.Accounts.PausedAccountIDs[accountID] || s.Reauth.Needs(accountID) {
		return PoolSelectionResult{Status: PoolSelectionNone}
	}
	generation, known := selectionGeneration(input, accountID)
	result := PoolSelectionResult{
		Status:          PoolSelectionSelected,
		Reason:          PoolSelectionFixed,
		AccountID:       accountID,
		Generation:      generation,
		GenerationKnown: known,
	}
	if s.hardBlocked(input, accountID) {
		result.Degraded = true
		result.RequiresProbe = true
		return result
	}
	if s.softBlocked(input, accountID) {
		result.Degraded = true
	}
	return result
}
