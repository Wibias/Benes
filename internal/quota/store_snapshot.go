package quota

// Snapshot returns the last cached quota response without triggering a provider
// probe. Callers receive a deep copy so management/read paths cannot mutate the
// cache used by FetchReports.
func (s *Store) Snapshot() Response {
	if s == nil {
		return Response{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneResponse(s.value)
}

func cloneResponse(in Response) Response {
	out := Response{GeneratedAt: in.GeneratedAt}
	if len(in.Reports) == 0 {
		return out
	}
	out.Reports = make([]Report, len(in.Reports))
	for i, report := range in.Reports {
		out.Reports[i] = cloneReport(report)
	}
	return out
}

func cloneReport(in Report) Report {
	out := in
	out.Quota = cloneQuota(in.Quota)
	if in.Entitlement != nil {
		entitlement := *in.Entitlement
		out.Entitlement = &entitlement
	}
	return out
}

func cloneQuota(in Quota) Quota {
	out := in
	out.FiveHourPercent = cloneFloat64(in.FiveHourPercent)
	out.WeeklyPercent = cloneFloat64(in.WeeklyPercent)
	out.MonthlyPercent = cloneFloat64(in.MonthlyPercent)
	if len(in.CustomWindows) > 0 {
		out.CustomWindows = append([]Window(nil), in.CustomWindows...)
	}
	if in.CreditsUsd != nil {
		credits := *in.CreditsUsd
		out.CreditsUsd = &credits
	}
	return out
}

func cloneFloat64(value *float64) *float64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
