package codexauth

import (
	"sync"
	"time"
)

const FailureWindow = 5 * time.Minute

type FailureSnapshot struct {
	ConsecutiveFailures  int
	ConsecutiveSuccesses int
	LastFailureStatus    int
	LastFailureAt        time.Time
}

type FailureState struct {
	mu                       sync.RWMutex
	accounts                 map[string]FailureSnapshot
	lastReconciledGeneration uint64
	liveAccountIDs           map[string]struct{}
}

func NewFailureState() *FailureState {
	return &FailureState{
		accounts:       make(map[string]FailureSnapshot),
		liveAccountIDs: make(map[string]struct{}),
	}
}

func (s *FailureState) RecordFailure(accountID string, now time.Time, status int, writerGeneration uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if writerGeneration < s.lastReconciledGeneration {
		if _, live := s.liveAccountIDs[accountID]; !live {
			return
		}
	}
	current, exists := s.accounts[accountID]
	stale := !exists || (!current.LastFailureAt.IsZero() && now.Sub(current.LastFailureAt) > FailureWindow)
	failures := 1
	if !stale {
		failures = current.ConsecutiveFailures + 1
	}
	s.accounts[accountID] = FailureSnapshot{
		ConsecutiveFailures: failures,
		LastFailureStatus:   status,
		LastFailureAt:       now,
	}
}

func (s *FailureState) RecordSuccess(accountID string, _ time.Time, failoverThreshold int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, exists := s.accounts[accountID]
	if !exists {
		return
	}
	if failoverThreshold > 0 && current.ConsecutiveFailures >= 2 {
		current.ConsecutiveSuccesses++
		if current.ConsecutiveSuccesses < 2 {
			s.accounts[accountID] = current
			return
		}
	}
	delete(s.accounts, accountID)
}

func (s *FailureState) ShouldFailover(accountID string, now time.Time, threshold int) bool {
	if threshold <= 0 {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	current, exists := s.accounts[accountID]
	if !exists {
		return false
	}
	if !current.LastFailureAt.IsZero() && now.Sub(current.LastFailureAt) > FailureWindow {
		return false
	}
	return current.ConsecutiveFailures >= threshold
}

func (s *FailureState) Snapshot(accountID string) (FailureSnapshot, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	current, exists := s.accounts[accountID]
	return current, exists
}

func (s *FailureState) Clear(accountID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.accounts, accountID)
}

func (s *FailureState) Reconcile(generation uint64, liveAccountIDs map[string]struct{}) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if generation <= s.lastReconciledGeneration {
		return 0
	}
	removed := 0
	for accountID := range s.accounts {
		if _, live := liveAccountIDs[accountID]; live {
			continue
		}
		delete(s.accounts, accountID)
		removed++
	}
	s.liveAccountIDs = cloneAccountIDSet(liveAccountIDs)
	s.lastReconciledGeneration = generation
	return removed
}
