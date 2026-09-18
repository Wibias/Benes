package codexauth

import (
	"sync"
	"time"
)

type StoredQuota struct {
	QuotaReading
	UpdatedAt            time.Time
	CredentialGeneration *int64
}

type QuotaState struct {
	mu                       sync.RWMutex
	accounts                 map[string]StoredQuota
	lastReconciledGeneration uint64
	liveAccountIDs           map[string]struct{}
}

func NewQuotaState() *QuotaState {
	return &QuotaState{
		accounts:       make(map[string]StoredQuota),
		liveAccountIDs: make(map[string]struct{}),
	}
}

func (s *QuotaState) SetParsed(accountID string, reading QuotaReading, writerGeneration uint64, now time.Time) bool {
	return s.setParsed(accountID, nil, reading, writerGeneration, now)
}

func (s *QuotaState) SetParsedForCredential(
	accountID string,
	credentialGeneration int64,
	reading QuotaReading,
	writerGeneration uint64,
	now time.Time,
) bool {
	return s.setParsed(accountID, &credentialGeneration, reading, writerGeneration, now)
}

func (s *QuotaState) setParsed(
	accountID string,
	credentialGeneration *int64,
	reading QuotaReading,
	writerGeneration uint64,
	now time.Time,
) bool {
	if accountID == "" {
		return false
	}
	if !quotaReadingHasUsage(reading) && reading.ResetCredits == nil {
		return false
	}
	if now.IsZero() {
		now = time.Now()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if writerGeneration < s.lastReconciledGeneration {
		if _, live := s.liveAccountIDs[accountID]; !live {
			return false
		}
	}

	existing, exists := s.accounts[accountID]
	if exists && credentialGeneration != nil && existing.CredentialGeneration != nil && *credentialGeneration < *existing.CredentialGeneration {
		return false
	}
	preserveExisting := exists && sameCredentialGeneration(existing.CredentialGeneration, credentialGeneration)
	next := StoredQuota{
		UpdatedAt:            now,
		CredentialGeneration: cloneOptionalInt64(credentialGeneration),
	}
	creditsOnly := reading.ResetCredits != nil && !quotaReadingHasUsage(reading)
	if creditsOnly {
		if preserveExisting {
			next.WeeklyPercent = cloneFloat64(existing.WeeklyPercent)
			next.WeeklyResetAt = cloneFloat64(existing.WeeklyResetAt)
			next.MonthlyPercent = cloneFloat64(existing.MonthlyPercent)
			next.MonthlyResetAt = cloneFloat64(existing.MonthlyResetAt)
			next.MonthlyIsPrimaryWindow = existing.MonthlyIsPrimaryWindow
			next.ShortPercent = cloneFloat64(existing.ShortPercent)
			next.ShortResetAt = cloneFloat64(existing.ShortResetAt)
			next.ShortWindowSeconds = cloneFloat64(existing.ShortWindowSeconds)
		}
		next.ResetCredits = cloneFloat64(reading.ResetCredits)
		s.accounts[accountID] = next
		return true
	}

	readingHasWeekly := reading.WeeklyPercent != nil || reading.WeeklyResetAt != nil
	readingHasMonthly := reading.MonthlyPercent != nil || reading.MonthlyResetAt != nil
	if readingHasWeekly {
		next.WeeklyPercent = cloneFloat64(reading.WeeklyPercent)
		next.WeeklyResetAt = cloneFloat64(reading.WeeklyResetAt)
	} else if readingHasMonthly {
		// Monthly-only snapshots intentionally clear stale weekly evidence.
	} else if preserveExisting {
		next.WeeklyPercent = cloneFloat64(existing.WeeklyPercent)
		next.WeeklyResetAt = cloneFloat64(existing.WeeklyResetAt)
	}

	if readingHasMonthly {
		next.MonthlyPercent = cloneFloat64(reading.MonthlyPercent)
		next.MonthlyResetAt = cloneFloat64(reading.MonthlyResetAt)
		next.MonthlyIsPrimaryWindow = reading.MonthlyIsPrimaryWindow
	} else if readingHasWeekly && preserveExisting {
		next.MonthlyPercent = cloneFloat64(existing.MonthlyPercent)
		next.MonthlyResetAt = cloneFloat64(existing.MonthlyResetAt)
		next.MonthlyIsPrimaryWindow = existing.MonthlyIsPrimaryWindow
	}

	if reading.ResetCredits != nil {
		next.ResetCredits = cloneFloat64(reading.ResetCredits)
	} else if preserveExisting {
		next.ResetCredits = cloneFloat64(existing.ResetCredits)
	}

	readingHasShort := reading.ShortPercent != nil || reading.ShortResetAt != nil || reading.ShortWindowSeconds != nil
	if readingHasShort {
		next.ShortPercent = cloneFloat64(reading.ShortPercent)
		next.ShortResetAt = cloneFloat64(reading.ShortResetAt)
		next.ShortWindowSeconds = cloneFloat64(reading.ShortWindowSeconds)
	} else if preserveExisting {
		next.ShortPercent = cloneFloat64(existing.ShortPercent)
		next.ShortResetAt = cloneFloat64(existing.ShortResetAt)
		next.ShortWindowSeconds = cloneFloat64(existing.ShortWindowSeconds)
	}
	s.accounts[accountID] = next
	return true
}

func (s *QuotaState) Get(accountID string) *StoredQuota {
	s.mu.RLock()
	defer s.mu.RUnlock()
	quota, ok := s.accounts[accountID]
	if !ok {
		return nil
	}
	cloned := cloneStoredQuota(quota)
	return &cloned
}

func (s *QuotaState) GetForCredential(accountID string, credentialGeneration int64) *StoredQuota {
	s.mu.RLock()
	defer s.mu.RUnlock()
	quota, ok := s.accounts[accountID]
	if !ok || quota.CredentialGeneration == nil || *quota.CredentialGeneration != credentialGeneration {
		return nil
	}
	cloned := cloneStoredQuota(quota)
	return &cloned
}

func (s *QuotaState) SelectionSnapshots() map[string]*QuotaSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.selectionSnapshotsLocked(nil, false)
}

func (s *QuotaState) SelectionSnapshotsForCredentials(credentials ManagedCredentialSnapshot) map[string]*QuotaSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.selectionSnapshotsLocked(&credentials, true)
}

func (s *QuotaState) selectionSnapshotsLocked(
	credentials *ManagedCredentialSnapshot,
	requireCredentialGeneration bool,
) map[string]*QuotaSnapshot {
	result := make(map[string]*QuotaSnapshot, len(s.accounts))
	for accountID, quota := range s.accounts {
		if requireCredentialGeneration {
			if quota.CredentialGeneration == nil || credentials == nil || !quotaCredentialGenerationLive(*credentials, accountID, *quota.CredentialGeneration) {
				continue
			}
		}
		result[accountID] = &QuotaSnapshot{
			WeeklyPercent:  cloneFloat64(quota.WeeklyPercent),
			MonthlyPercent: cloneFloat64(quota.MonthlyPercent),
			ShortPercent:   cloneFloat64(quota.ShortPercent),
			WeeklyResetAt:  cloneFloat64(quota.WeeklyResetAt),
			MonthlyResetAt: cloneFloat64(quota.MonthlyResetAt),
			ShortResetAt:   cloneFloat64(quota.ShortResetAt),
			UpdatedAt:      quota.UpdatedAt,
		}
	}
	return result
}

func (s *QuotaState) Reconcile(generation uint64, liveAccountIDs map[string]struct{}) int {
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

func IsQuotaReadingExhausted(reading *QuotaReading, plan string) bool {
	if reading == nil {
		return false
	}
	values := []*float64{reading.WeeklyPercent, reading.MonthlyPercent, reading.ShortPercent}
	if isThirtyDayOnlyPlan(plan) {
		values = []*float64{reading.MonthlyPercent, reading.ShortPercent}
	}
	for _, value := range values {
		if finitePercent(value) && *value >= 100 {
			return true
		}
	}
	return false
}

func quotaCredentialGenerationLive(snapshot ManagedCredentialSnapshot, accountID string, generation int64) bool {
	if snapshot.Status != ManagedCredentialStoreOK {
		return false
	}
	record, ok := snapshot.Records[accountID]
	return ok && record.Credential != nil && record.DeletedAtMS == nil && record.Generation == generation
}

func sameCredentialGeneration(left, right *int64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func cloneStoredQuota(quota StoredQuota) StoredQuota {
	return StoredQuota{
		QuotaReading:         cloneQuotaReading(quota.QuotaReading),
		UpdatedAt:            quota.UpdatedAt,
		CredentialGeneration: cloneOptionalInt64(quota.CredentialGeneration),
	}
}

func cloneQuotaReading(reading QuotaReading) QuotaReading {
	return QuotaReading{
		WeeklyPercent:          cloneFloat64(reading.WeeklyPercent),
		MonthlyPercent:         cloneFloat64(reading.MonthlyPercent),
		ShortPercent:           cloneFloat64(reading.ShortPercent),
		WeeklyResetAt:          cloneFloat64(reading.WeeklyResetAt),
		MonthlyResetAt:         cloneFloat64(reading.MonthlyResetAt),
		ShortResetAt:           cloneFloat64(reading.ShortResetAt),
		ShortWindowSeconds:     cloneFloat64(reading.ShortWindowSeconds),
		ResetCredits:           cloneFloat64(reading.ResetCredits),
		MonthlyIsPrimaryWindow: reading.MonthlyIsPrimaryWindow,
	}
}

func cloneFloat64(value *float64) *float64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
