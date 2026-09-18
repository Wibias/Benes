package codexauth

import "strings"

func (p *MainQuotaPrimer) ObserveUpstreamQuota(identity string, reading QuotaReading) bool {
	identity = strings.TrimSpace(identity)
	if identity == "" || !quotaReadingHasUsage(reading) {
		return false
	}

	if !p.identityStillLive(identity) {
		return false
	}

	// Serialize with WHAM publication. The physical identity can change at any
	// time, so re-check it while the state write is exclusively owned. This
	// prevents stale identity A from overwriting a newer identity B snapshot.
	p.state.mu.Lock()
	defer p.state.mu.Unlock()

	current := p.credentials.Read(p.now())
	if current.Status != MainCredentialOK || current.Identity != identity {
		return false
	}

	plan := p.configuredPlan
	var existing *QuotaReading
	if p.state.snapshot.Identity == identity {
		if strings.TrimSpace(p.state.snapshot.Plan) != "" {
			plan = p.state.snapshot.Plan
		}
		if p.state.snapshot.Quota != nil {
			cloned := cloneQuotaReading(*p.state.snapshot.Quota)
			existing = &cloned
		}
	}
	merged := mergeLiveQuotaReading(existing, reading)
	p.state.snapshot = MainQuotaSnapshot{
		Identity:  identity,
		Plan:      strings.TrimSpace(plan),
		Quota:     merged,
		UpdatedAt: p.now(),
	}

	current = p.credentials.Read(p.now())
	if current.Status == MainCredentialOK && current.Identity == identity {
		return true
	}
	if p.state.snapshot.Identity == identity {
		p.state.snapshot = MainQuotaSnapshot{}
	}
	return false
}

func mergeLiveQuotaReading(existing *QuotaReading, reading QuotaReading) *QuotaReading {
	next := QuotaReading{}
	readingHasWeekly := reading.WeeklyPercent != nil || reading.WeeklyResetAt != nil
	readingHasMonthly := reading.MonthlyPercent != nil || reading.MonthlyResetAt != nil
	readingHasShort := reading.ShortPercent != nil || reading.ShortResetAt != nil || reading.ShortWindowSeconds != nil
	readingHasCredits := reading.ResetCredits != nil

	if readingHasWeekly {
		next.WeeklyPercent = cloneFloat64(reading.WeeklyPercent)
		next.WeeklyResetAt = cloneFloat64(reading.WeeklyResetAt)
	} else if !readingHasMonthly && existing != nil {
		next.WeeklyPercent = cloneFloat64(existing.WeeklyPercent)
		next.WeeklyResetAt = cloneFloat64(existing.WeeklyResetAt)
	}

	if readingHasMonthly {
		next.MonthlyPercent = cloneFloat64(reading.MonthlyPercent)
		next.MonthlyResetAt = cloneFloat64(reading.MonthlyResetAt)
		next.MonthlyIsPrimaryWindow = reading.MonthlyIsPrimaryWindow
	} else if readingHasWeekly && existing != nil {
		next.MonthlyPercent = cloneFloat64(existing.MonthlyPercent)
		next.MonthlyResetAt = cloneFloat64(existing.MonthlyResetAt)
		next.MonthlyIsPrimaryWindow = existing.MonthlyIsPrimaryWindow
	} else if existing != nil {
		next.MonthlyPercent = cloneFloat64(existing.MonthlyPercent)
		next.MonthlyResetAt = cloneFloat64(existing.MonthlyResetAt)
		next.MonthlyIsPrimaryWindow = existing.MonthlyIsPrimaryWindow
	}

	if readingHasShort {
		next.ShortPercent = cloneFloat64(reading.ShortPercent)
		next.ShortResetAt = cloneFloat64(reading.ShortResetAt)
		next.ShortWindowSeconds = cloneFloat64(reading.ShortWindowSeconds)
	} else if existing != nil {
		next.ShortPercent = cloneFloat64(existing.ShortPercent)
		next.ShortResetAt = cloneFloat64(existing.ShortResetAt)
		next.ShortWindowSeconds = cloneFloat64(existing.ShortWindowSeconds)
	}
	if readingHasCredits {
		next.ResetCredits = cloneFloat64(reading.ResetCredits)
	} else if existing != nil {
		next.ResetCredits = cloneFloat64(existing.ResetCredits)
	}
	return &next
}
