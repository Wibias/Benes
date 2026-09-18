package quota

import (
	"strings"
	"time"
)

const entitlementAuthoritative = "authoritative"

func providerIdentity(provider Provider) string {
	return Report{Provider: provider.Name, AccountID: provider.AccountID}.Identity()
}

func bindReport(report Report, provider Provider, ent *Entitlement) Report {
	report.AccountID = strings.TrimSpace(provider.AccountID)
	if ent == nil || ent.unused() {
		return report
	}
	copy := *ent
	copy.AccountID = report.AccountID
	if copy.ObservedAt == 0 {
		copy.ObservedAt = report.UpdatedAt
	}
	if copy.Confidence == "" {
		copy.Confidence = entitlementAuthoritative
	}
	if strings.TrimSpace(copy.Source) == "" {
		copy.Source = report.Source
	}
	report.Entitlement = &copy
	return report
}

func authoritativeEntitlement(source string, billingEnd, planRenews, subscriptionExpires, entitlementExpires int64) *Entitlement {
	ent := Entitlement{
		BillingPeriodEndsAt:   billingEnd,
		PlanRenewsAt:          planRenews,
		SubscriptionExpiresAt: subscriptionExpires,
		EntitlementExpiresAt:  entitlementExpires,
		Source:                source,
		Confidence:            entitlementAuthoritative,
	}
	if ent.unused() {
		return nil
	}
	return &ent
}

func firstEpoch(record map[string]any, names ...string) (int64, bool) {
	if record == nil {
		return 0, false
	}
	for _, name := range names {
		if ms, ok := epochMillis(record[name]); ok && ms > 0 {
			return ms, true
		}
	}
	return 0, false
}

func parseTimeMillis(raw string) (int64, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, false
	}
	if parsed, err := parseFloat(s); err == nil {
		return epochMillis(parsed)
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if ts, err := time.Parse(layout, s); err == nil {
			ms := ts.UnixMilli()
			if ms > 0 {
				return ms, true
			}
		}
	}
	return 0, false
}

func a6Entitlement(subscription map[string]any) *Entitlement {
	billing, _ := firstEpoch(subscription, "current_period_end", "currentPeriodEnd", "period_end")
	renews, _ := firstEpoch(subscription, "renews_at", "renewsAt", "next_billing_at", "nextBillingAt")
	subExpires, _ := firstEpoch(subscription, "access_until", "subscription_end", "subscriptionExpiresAt")
	return authoritativeEntitlement("a6api:subscription", billing, renews, subExpires, 0)
}

func xaiBillingEnd(cfg map[string]any) int64 {
	ms, _ := firstEpoch(cfg, "billingPeriodEnd", "billing_period_end")
	return ms
}
