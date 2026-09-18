package codexauth

import "time"

func (s *HealthState) ClearLiveCooldowns(accountID string) bool {
	if s == nil {
		return false
	}
	now := time.Now()
	_, hard := s.HardCooldown(accountID, now)
	_, spark := s.ScopedHardCooldown(accountID, QuotaScopeSpark, now)
	_, shared := s.ScopedHardCooldown(accountID, QuotaScopeShared, now)
	_, soft := s.SoftAvoidUntil(accountID, now)
	cleared := hard || spark || shared || soft
	s.ClearHardCooldown(accountID)
	s.ClearSoftAvoid(accountID)
	s.ClearScopedHardCooldown(accountID, QuotaScopeSpark)
	s.ClearScopedHardCooldown(accountID, QuotaScopeShared)
	return cleared
}
