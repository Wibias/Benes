package codexauth

import (
	"testing"
	"time"
)

func TestPoolSelectorPickLowestUsesCredentialPoolRanking(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	selector := NewPoolSelector(PoolSelectorDependencies{})
	high := 90.0
	low := 10.0
	input := PoolSelectionInput{
		Accounts: ManagedAccountConfig{Accounts: []ManagedAccount{{ID: "acct-high"}, {ID: "acct-low"}}},
		Quotas: map[string]*QuotaSnapshot{
			"acct-high": {WeeklyPercent: &high},
			"acct-low":  {WeeklyPercent: &low},
		},
		Now: now,
	}
	if got := selectFromCredentialPool([]string{"acct-high", "acct-low"}, input, selector); got != "acct-low" {
		t.Fatalf("selectFromCredentialPool=%q", got)
	}
}
