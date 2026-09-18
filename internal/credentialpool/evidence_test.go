package credentialpool

import (
	"testing"
	"time"
)

func TestEvaluateEvidenceSharesRuntimeQuotaTruth(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	cases := []struct {
		name     string
		evidence Evidence
		want     Availability
	}{
		{"known available", Evidence{Auth: AuthUsable, Quota: QuotaKnown, Utilization: 0.25, ValidUntil: now.Add(time.Minute), Limit: LimitAvailable}, AvailabilityAvailable},
		{"known exhausted", Evidence{Auth: AuthUsable, Quota: QuotaKnown, Utilization: 1, ValidUntil: now.Add(time.Minute), Limit: LimitAvailable}, AvailabilityExhausted},
		{"overage at cap remains usable", Evidence{Auth: AuthUsable, Quota: QuotaKnown, Utilization: 1, ValidUntil: now.Add(time.Minute), Limit: LimitAvailable, OverageEnabled: true}, AvailabilityAvailable},
		{"unknown quota", Evidence{Auth: AuthUsable, Quota: QuotaUnknown, Limit: LimitAvailable}, AvailabilityUnknown},
		{"stale exhausted becomes unknown", Evidence{Auth: AuthUsable, Quota: QuotaKnown, Utilization: 1, ValidUntil: now.Add(-time.Second), Limit: LimitAvailable}, AvailabilityUnknown},
		{"reauth", Evidence{Auth: AuthReauthRequired, Quota: QuotaUnknown, Limit: LimitAvailable}, AvailabilityReauthRequired},
		{"cooldown", Evidence{Auth: AuthUsable, Quota: QuotaKnown, Utilization: .4, Limit: LimitRateLimited, ResetAt: now.Add(time.Minute)}, AvailabilityLimited},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := EvaluateEvidence(tc.evidence, now); got != tc.want {
				t.Fatalf("EvaluateEvidence() = %q, want %q", got, tc.want)
			}
		})
	}
}
