package codexauth

import (
	"testing"
	"time"
)

func TestPoolSelectorExcludeAccountOverridesAffinityAndActiveWithoutReleasingPin(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)

	t.Run("affinity", func(t *testing.T) {
		selector := NewPoolSelector(PoolSelectorDependencies{})
		input := poolSelectorFixture(now)
		input.ThreadID = "thread"
		input.Accounts.Accounts = []ManagedAccount{{ID: "a", Plan: "plus"}, {ID: "b", Plan: "plus"}}
		input.Credentials = credentialSnapshotFor("a", "b")
		input.Quotas = map[string]*QuotaSnapshot{"a": {WeeklyPercent: float64Ptr(10)}, "b": {WeeklyPercent: float64Ptr(20)}}
		if !selector.Affinity.Bind("thread", "a", AffinityScopeLegacy, now, input.Credentials) {
			t.Fatal("bind failed")
		}
		input.ExcludeAccountID = "a"

		got := selector.Resolve(input)
		if got.Status != PoolSelectionSelected || got.AccountID != "b" || got.Reason == PoolSelectionAffinity {
			t.Fatalf("excluded affinity selection=%#v", got)
		}
		resolved := selector.Affinity.Resolve("thread", AffinityScopeLegacy, now, input.Credentials)
		if resolved.AccountID != "b" {
			t.Fatalf("affinity not rebound away from excluded account: %#v", resolved)
		}
	})

	t.Run("active priority and pin", func(t *testing.T) {
		selector := NewPoolSelector(PoolSelectorDependencies{})
		input := poolSelectorFixture(now)
		input.Accounts.Accounts = []ManagedAccount{{ID: "a", Plan: "plus"}, {ID: "b", Plan: "plus"}}
		input.Accounts.ActiveAccountID = "a"
		input.Accounts.PinnedAccountID = "a"
		input.Accounts.Priorities = map[string]int{"a": 10, "b": 0}
		input.Credentials = credentialSnapshotFor("a", "b")
		input.Quotas = map[string]*QuotaSnapshot{"a": {WeeklyPercent: float64Ptr(10)}, "b": {WeeklyPercent: float64Ptr(20)}}
		input.ExcludeAccountID = "a"

		got := selector.Resolve(input)
		if got.Status != PoolSelectionSelected || got.AccountID != "b" {
			t.Fatalf("excluded active selection=%#v", got)
		}
		if got.PinReleased {
			t.Fatalf("same-request exclusion must not release durable pin: %#v", got)
		}
	})
}
