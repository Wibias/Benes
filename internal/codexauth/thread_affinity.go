package codexauth

import (
	"sync"
	"time"
)

const (
	ThreadAffinityIdleTTL              = 24 * time.Hour
	ThreadAffinityReevaluationInterval = 60 * time.Second
	ThreadAffinityMaxEntries           = 2048
	MaxAffinityComponentBytes          = 512
)

type AffinityScope string

const (
	AffinityScopeLegacy AffinityScope = "legacy"
	AffinityScopeShared AffinityScope = "shared"
	AffinityScopeSpark  AffinityScope = "spark"
)

type AffinityStatus string

const (
	AffinityNone     AffinityStatus = "none"
	AffinitySelected AffinityStatus = "selected"
	AffinityExpired  AffinityStatus = "expired"
	AffinityStale    AffinityStatus = "stale"
)

type AffinityResolution struct {
	Status            AffinityStatus
	AccountID         string
	Generation        int64
	CreatedAt         time.Time
	LastReevaluatedAt time.Time
}

type affinityEntry struct {
	accountID         string
	generation        int64
	createdAt         time.Time
	lastUsedAt        time.Time
	lastReevaluatedAt time.Time
}

type ThreadAffinityState struct {
	mu      sync.Mutex
	entries map[string]map[AffinityScope]affinityEntry
	count   int
}

func NewThreadAffinityState() *ThreadAffinityState {
	return &ThreadAffinityState{entries: make(map[string]map[AffinityScope]affinityEntry)}
}

func CredentialGenerationLive(snapshot ManagedCredentialSnapshot, accountID string, generation int64) bool {
	if accountID == MainAccountID {
		return generation == 0
	}
	if snapshot.Status != ManagedCredentialStoreOK {
		return false
	}
	record, exists := snapshot.Records[accountID]
	return exists && record.Credential != nil && record.DeletedAtMS == nil && record.Generation == generation
}

func (s *ThreadAffinityState) Bind(
	threadID string,
	accountID string,
	scope AffinityScope,
	now time.Time,
	credentials ManagedCredentialSnapshot,
) bool {
	if !admissibleAffinityComponent(threadID) || !admissibleAffinityComponent(accountID) {
		return false
	}
	generation, ok := affinityGeneration(accountID, credentials)
	if !ok {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneExpiredLocked(now)
	scopes := s.entries[threadID]
	if scopes == nil {
		scopes = make(map[AffinityScope]affinityEntry)
		s.entries[threadID] = scopes
	}
	createdAt := now
	if previous, exists := scopes[scope]; exists {
		createdAt = previous.createdAt
	} else {
		s.count++
	}
	scopes[scope] = affinityEntry{
		accountID:         accountID,
		generation:        generation,
		createdAt:         createdAt,
		lastUsedAt:        now,
		lastReevaluatedAt: now,
	}
	s.pruneLRULocked()
	return true
}

func (s *ThreadAffinityState) Resolve(
	threadID string,
	scope AffinityScope,
	now time.Time,
	credentials ManagedCredentialSnapshot,
) AffinityResolution {
	if !admissibleAffinityComponent(threadID) {
		return AffinityResolution{Status: AffinityNone}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	scopes := s.entries[threadID]
	if scopes == nil {
		return AffinityResolution{Status: AffinityNone}
	}
	entry, exists := scopes[scope]
	if !exists {
		return AffinityResolution{Status: AffinityNone}
	}
	if now.Sub(entry.lastUsedAt) > ThreadAffinityIdleTTL {
		s.deleteLocked(threadID, scope)
		return affinityResolution(AffinityExpired, entry)
	}
	if !CredentialGenerationLive(credentials, entry.accountID, entry.generation) {
		s.deleteLocked(threadID, scope)
		return affinityResolution(AffinityStale, entry)
	}
	entry.lastUsedAt = now
	scopes[scope] = entry
	return affinityResolution(AffinitySelected, entry)
}

func (s *ThreadAffinityState) MarkReevaluated(
	threadID string,
	scope AffinityScope,
	accountID string,
	generation int64,
	now time.Time,
) bool {
	if !admissibleAffinityComponent(threadID) || !admissibleAffinityComponent(accountID) {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	scopes := s.entries[threadID]
	if scopes == nil {
		return false
	}
	entry, exists := scopes[scope]
	if !exists || entry.accountID != accountID || entry.generation != generation {
		return false
	}
	entry.lastReevaluatedAt = now
	scopes[scope] = entry
	return true
}

func AffinityReevaluationDue(resolution AffinityResolution, now time.Time, overThreshold bool) bool {
	if resolution.Status != AffinitySelected {
		return false
	}
	if overThreshold || resolution.LastReevaluatedAt.IsZero() {
		return true
	}
	return !now.Before(resolution.LastReevaluatedAt.Add(ThreadAffinityReevaluationInterval))
}

func affinityResolution(status AffinityStatus, entry affinityEntry) AffinityResolution {
	return AffinityResolution{
		Status:            status,
		AccountID:         entry.accountID,
		Generation:        entry.generation,
		CreatedAt:         entry.createdAt,
		LastReevaluatedAt: entry.lastReevaluatedAt,
	}
}

func (s *ThreadAffinityState) ClearThread(threadID string, scope AffinityScope) {
	if !admissibleAffinityComponent(threadID) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleteLocked(threadID, scope)
}

func (s *ThreadAffinityState) ClearAccount(accountID string) {
	if !admissibleAffinityComponent(accountID) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for threadID, scopes := range s.entries {
		for scope, entry := range scopes {
			if entry.accountID != accountID {
				continue
			}
			delete(scopes, scope)
			s.count--
		}
		if len(scopes) == 0 {
			delete(s.entries, threadID)
		}
	}
}

func (s *ThreadAffinityState) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.count
}

func affinityGeneration(accountID string, credentials ManagedCredentialSnapshot) (int64, bool) {
	if accountID == MainAccountID {
		return 0, true
	}
	if credentials.Status != ManagedCredentialStoreOK {
		return 0, false
	}
	record, exists := credentials.Records[accountID]
	if !exists || record.Credential == nil || record.DeletedAtMS != nil {
		return 0, false
	}
	return record.Generation, true
}

func admissibleAffinityComponent(value string) bool {
	return len(value) <= MaxAffinityComponentBytes
}

func (s *ThreadAffinityState) deleteLocked(threadID string, scope AffinityScope) {
	scopes := s.entries[threadID]
	if scopes == nil {
		return
	}
	if _, exists := scopes[scope]; !exists {
		return
	}
	delete(scopes, scope)
	s.count--
	if len(scopes) == 0 {
		delete(s.entries, threadID)
	}
}

func (s *ThreadAffinityState) pruneExpiredLocked(now time.Time) {
	for threadID, scopes := range s.entries {
		for scope, entry := range scopes {
			if now.Sub(entry.lastUsedAt) <= ThreadAffinityIdleTTL {
				continue
			}
			delete(scopes, scope)
			s.count--
		}
		if len(scopes) == 0 {
			delete(s.entries, threadID)
		}
	}
}

func (s *ThreadAffinityState) pruneLRULocked() {
	for s.count > ThreadAffinityMaxEntries {
		oldestThread := ""
		oldestScope := AffinityScope("")
		var oldest time.Time
		found := false
		for threadID, scopes := range s.entries {
			for scope, entry := range scopes {
				if !found || entry.lastUsedAt.Before(oldest) {
					oldestThread = threadID
					oldestScope = scope
					oldest = entry.lastUsedAt
					found = true
				}
			}
		}
		if !found {
			return
		}
		s.deleteLocked(oldestThread, oldestScope)
	}
}
