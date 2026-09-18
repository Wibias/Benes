package codexauth

import "sync"

type ReauthState struct {
	mu                       sync.RWMutex
	accounts                 map[string]struct{}
	lastReconciledGeneration uint64
	liveAccountIDs           map[string]struct{}
}

func NewReauthState() *ReauthState {
	return &ReauthState{
		accounts:       make(map[string]struct{}),
		liveAccountIDs: make(map[string]struct{}),
	}
}

func (s *ReauthState) Mark(accountID string, writerGeneration uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if writerGeneration < s.lastReconciledGeneration {
		if _, live := s.liveAccountIDs[accountID]; !live {
			return
		}
	}
	s.accounts[accountID] = struct{}{}
}

func (s *ReauthState) Needs(accountID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, exists := s.accounts[accountID]
	return exists
}

func (s *ReauthState) Clear(accountID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.accounts, accountID)
}

func (s *ReauthState) Reconcile(generation uint64, liveAccountIDs map[string]struct{}) int {
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

func cloneAccountIDSet(values map[string]struct{}) map[string]struct{} {
	cloned := make(map[string]struct{}, len(values))
	for value := range values {
		cloned[value] = struct{}{}
	}
	return cloned
}
