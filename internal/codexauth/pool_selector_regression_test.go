package codexauth

import (
	"testing"
	"time"
)

func TestPoolSelectorInitialQuotaContinuesIntoFailureFailover(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	selector := NewPoolSelector(PoolSelectorDependencies{})
	input := poolSelectorFixture(now)
	input.Accounts.Accounts = []ManagedAccount{{ID: "a"}, {ID: "b"}}
	input.Credentials = credentialSnapshotFor("a", "b")
	input.Quotas = map[string]*QuotaSnapshot{"a": nil, "b": nil}
	for i := 0; i < 3; i++ {
		selector.Failures.RecordFailure("a", now, 502, 1)
	}

	got := selector.Resolve(input)
	if got.AccountID != "b" || got.Reason != PoolSelectionFailureFailover {
		t.Fatalf("selection=%#v", got)
	}
}

func TestPoolSelectorPriorityPreemptionDoesNotRequestPersistence(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	selector := NewPoolSelector(PoolSelectorDependencies{})
	input := poolSelectorFixture(now)
	input.Accounts.Accounts = []ManagedAccount{{ID: "high", Plan: "plus"}, {ID: "low", Plan: "plus"}}
	input.Accounts.Priorities = map[string]int{"high": 10, "low": 0}
	input.Accounts.ActiveAccountID = "low"
	input.Credentials = credentialSnapshotFor("high", "low")
	input.Quotas = map[string]*QuotaSnapshot{
		"high": {WeeklyPercent: float64Ptr(5)},
		"low":  {WeeklyPercent: float64Ptr(10)},
	}

	got := selector.Resolve(input)
	if got.AccountID != "high" || got.Reason != PoolSelectionPriorityPreemption {
		t.Fatalf("selection=%#v", got)
	}
	if !got.ActiveChanged || got.PersistActive {
		t.Fatalf("preemption persistence=%#v", got)
	}
}

func TestPoolSelectorDropsRuntimeCursorWhenAccountLeavesConfig(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	selector := NewPoolSelector(PoolSelectorDependencies{})
	input := poolSelectorFixture(now)
	input.Accounts.Accounts = []ManagedAccount{{ID: "a"}, {ID: "b"}}
	input.Accounts.ActiveAccountID = "a"
	input.Credentials = credentialSnapshotFor("a", "b")
	selector.Health.SetHardCooldown("a", now.Add(time.Minute), CooldownSourceRetryAfter)
	if got := selector.Resolve(input); got.AccountID != "b" {
		t.Fatalf("fallback=%#v", got)
	}

	input.Accounts.Accounts = []ManagedAccount{{ID: "a"}}
	input.Credentials = credentialSnapshotFor("a")
	got := selector.Resolve(input)
	if got.AccountID != "a" || !got.Degraded || !got.RequiresProbe {
		t.Fatalf("selection after runtime account removal=%#v", got)
	}
}
