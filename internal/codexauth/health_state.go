package codexauth

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

const QuotaProbeInterval = 5 * time.Minute

type QuotaScope string

const (
	QuotaScopeShared QuotaScope = "shared"
	QuotaScopeSpark  QuotaScope = "spark"
)

type CooldownSource string

const (
	CooldownSourceRetryAfter   CooldownSource = "retry-after"
	CooldownSourceResetDerived CooldownSource = "reset-derived"
	CooldownSourceDefault      CooldownSource = "default"
)

type Cooldown struct {
	Until                time.Time
	Since                time.Time
	Source               CooldownSource
	Generation           uint64
	ProbeLeaseID         string
	ProbeLeaseGeneration uint64
	LastProbeAt          time.Time
}

type ProbeLease struct {
	AccountID          string
	Scope              QuotaScope
	LeaseID            string
	CooldownGeneration uint64
}

type accountHealth struct {
	hardCooldown   *Cooldown
	softAvoidUntil time.Time
}

type HealthState struct {
	mu                       sync.RWMutex
	account                  map[string]accountHealth
	scoped                   map[string]map[QuotaScope]Cooldown
	lastReconciledGeneration uint64
}

func NewHealthState() *HealthState {
	return &HealthState{
		account: make(map[string]accountHealth),
		scoped:  make(map[string]map[QuotaScope]Cooldown),
	}
}

func (s *HealthState) SetHardCooldown(accountID string, until time.Time, source CooldownSource) {
	s.SetHardCooldownAt(accountID, time.Now(), until, source)
}

func (s *HealthState) SetHardCooldownAt(accountID string, since, until time.Time, source CooldownSource) {
	s.mu.Lock()
	defer s.mu.Unlock()
	health := s.account[accountID]
	health.hardCooldown = nextCooldown(health.hardCooldown, since, until, source)
	s.account[accountID] = health
}

func (s *HealthState) HardCooldown(accountID string, now time.Time) (Cooldown, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	health, exists := s.account[accountID]
	if !exists || health.hardCooldown == nil || !health.hardCooldown.Until.After(now) {
		return Cooldown{}, false
	}
	return *health.hardCooldown, true
}

func (s *HealthState) ClearHardCooldown(accountID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	health, exists := s.account[accountID]
	if !exists {
		return
	}
	health.hardCooldown = nil
	if health.softAvoidUntil.IsZero() {
		delete(s.account, accountID)
		return
	}
	s.account[accountID] = health
}

func (s *HealthState) SetSoftAvoid(accountID string, until time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	health := s.account[accountID]
	health.softAvoidUntil = until
	s.account[accountID] = health
}

func (s *HealthState) SoftAvoidUntil(accountID string, now time.Time) (time.Time, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	health, exists := s.account[accountID]
	if !exists || !health.softAvoidUntil.After(now) {
		return time.Time{}, false
	}
	return health.softAvoidUntil, true
}

func (s *HealthState) ClearSoftAvoid(accountID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	health, exists := s.account[accountID]
	if !exists {
		return
	}
	health.softAvoidUntil = time.Time{}
	if health.hardCooldown == nil {
		delete(s.account, accountID)
		return
	}
	s.account[accountID] = health
}

func (s *HealthState) SetScopedHardCooldown(accountID string, scope QuotaScope, until time.Time, source CooldownSource) {
	s.SetScopedHardCooldownAt(accountID, scope, time.Now(), until, source)
}

func (s *HealthState) SetScopedHardCooldownAt(accountID string, scope QuotaScope, since, until time.Time, source CooldownSource) {
	s.mu.Lock()
	defer s.mu.Unlock()
	scopes := s.scoped[accountID]
	if scopes == nil {
		scopes = make(map[QuotaScope]Cooldown)
		s.scoped[accountID] = scopes
	}
	var previous *Cooldown
	if current, exists := scopes[scope]; exists {
		copy := current
		previous = &copy
	}
	scopes[scope] = *nextCooldown(previous, since, until, source)
}

func (s *HealthState) ScopedHardCooldown(accountID string, scope QuotaScope, now time.Time) (Cooldown, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cooldown, exists := s.scoped[accountID][scope]
	if !exists || !cooldown.Until.After(now) {
		return Cooldown{}, false
	}
	return cooldown, true
}

func (s *HealthState) ClearScopedHardCooldown(accountID string, scope QuotaScope) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clearScopedHardCooldownLocked(accountID, scope)
}

func (s *HealthState) TryAcquireHardCooldownProbe(accountID string, now time.Time) (ProbeLease, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	health, exists := s.account[accountID]
	if !exists || health.hardCooldown == nil {
		return ProbeLease{}, false, nil
	}
	lease, ok, err := tryAcquireProbeLocked(accountID, "", health.hardCooldown, now)
	if ok {
		s.account[accountID] = health
	}
	return lease, ok, err
}

func (s *HealthState) TryAcquireScopedHardCooldownProbe(accountID string, scope QuotaScope, now time.Time) (ProbeLease, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	scopes := s.scoped[accountID]
	if scopes == nil {
		return ProbeLease{}, false, nil
	}
	cooldown, exists := scopes[scope]
	if !exists {
		return ProbeLease{}, false, nil
	}
	lease, ok, err := tryAcquireProbeLocked(accountID, scope, &cooldown, now)
	if ok {
		scopes[scope] = cooldown
	}
	return lease, ok, err
}

func (s *HealthState) ReleaseProbe(lease ProbeLease, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if lease.Scope == "" {
		health, exists := s.account[lease.AccountID]
		if !exists || health.hardCooldown == nil || !releaseProbeLocked(health.hardCooldown, lease, now) {
			return false
		}
		s.account[lease.AccountID] = health
		return true
	}
	scopes := s.scoped[lease.AccountID]
	if scopes == nil {
		return false
	}
	cooldown, exists := scopes[lease.Scope]
	if !exists || !releaseProbeLocked(&cooldown, lease, now) {
		return false
	}
	scopes[lease.Scope] = cooldown
	return true
}

func (s *HealthState) CompleteProbeSuccess(lease ProbeLease, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if lease.Scope == "" {
		health, exists := s.account[lease.AccountID]
		if !exists || health.hardCooldown == nil || health.hardCooldown.ProbeLeaseID != lease.LeaseID {
			return false
		}
		if probeCanClear(*health.hardCooldown, lease) {
			health.hardCooldown = nil
			if health.softAvoidUntil.IsZero() {
				delete(s.account, lease.AccountID)
			} else {
				s.account[lease.AccountID] = health
			}
			return true
		}
		releaseProbeLocked(health.hardCooldown, lease, now)
		s.account[lease.AccountID] = health
		return false
	}

	scopes := s.scoped[lease.AccountID]
	if scopes == nil {
		return false
	}
	cooldown, exists := scopes[lease.Scope]
	if !exists || cooldown.ProbeLeaseID != lease.LeaseID {
		return false
	}
	if probeCanClear(cooldown, lease) {
		s.clearScopedHardCooldownLocked(lease.AccountID, lease.Scope)
		return true
	}
	releaseProbeLocked(&cooldown, lease, now)
	scopes[lease.Scope] = cooldown
	return false
}

func (s *HealthState) ClearAccount(accountID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.account, accountID)
	delete(s.scoped, accountID)
}

func (s *HealthState) Reconcile(generation uint64, liveAccountIDs map[string]struct{}) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if generation <= s.lastReconciledGeneration {
		return 0
	}
	removed := 0
	for accountID := range s.account {
		if _, live := liveAccountIDs[accountID]; live {
			continue
		}
		delete(s.account, accountID)
		removed++
	}
	for accountID := range s.scoped {
		if _, live := liveAccountIDs[accountID]; live {
			continue
		}
		delete(s.scoped, accountID)
		removed++
	}
	s.lastReconciledGeneration = generation
	return removed
}

func nextCooldown(previous *Cooldown, since, until time.Time, source CooldownSource) *Cooldown {
	generation := uint64(1)
	cooldown := &Cooldown{Since: since, Until: until, Source: source, Generation: generation}
	if previous == nil {
		return cooldown
	}
	cooldown.Generation = previous.Generation + 1
	cooldown.ProbeLeaseID = previous.ProbeLeaseID
	cooldown.ProbeLeaseGeneration = previous.ProbeLeaseGeneration
	cooldown.LastProbeAt = previous.LastProbeAt
	return cooldown
}

func tryAcquireProbeLocked(accountID string, scope QuotaScope, cooldown *Cooldown, now time.Time) (ProbeLease, bool, error) {
	if !cooldown.Until.After(now) || cooldown.Source == CooldownSourceRetryAfter || cooldown.ProbeLeaseID != "" {
		return ProbeLease{}, false, nil
	}
	origin := cooldown.Since
	if !cooldown.LastProbeAt.IsZero() {
		origin = cooldown.LastProbeAt
	}
	if origin.IsZero() || now.Sub(origin) < QuotaProbeInterval {
		return ProbeLease{}, false, nil
	}
	leaseID, err := newProbeLeaseID()
	if err != nil {
		return ProbeLease{}, false, err
	}
	cooldown.ProbeLeaseID = leaseID
	cooldown.ProbeLeaseGeneration = cooldown.Generation
	return ProbeLease{
		AccountID: accountID, Scope: scope, LeaseID: leaseID, CooldownGeneration: cooldown.Generation,
	}, true, nil
}

func releaseProbeLocked(cooldown *Cooldown, lease ProbeLease, now time.Time) bool {
	if cooldown.ProbeLeaseID == "" || cooldown.ProbeLeaseID != lease.LeaseID {
		return false
	}
	cooldown.ProbeLeaseID = ""
	cooldown.ProbeLeaseGeneration = 0
	cooldown.LastProbeAt = now
	return true
}

func probeCanClear(cooldown Cooldown, lease ProbeLease) bool {
	return cooldown.ProbeLeaseID == lease.LeaseID &&
		cooldown.ProbeLeaseGeneration == cooldown.Generation &&
		lease.CooldownGeneration == cooldown.Generation
}

func newProbeLeaseID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

func (s *HealthState) clearScopedHardCooldownLocked(accountID string, scope QuotaScope) {
	scopes := s.scoped[accountID]
	if scopes == nil {
		return
	}
	delete(scopes, scope)
	if len(scopes) == 0 {
		delete(s.scoped, accountID)
	}
}
