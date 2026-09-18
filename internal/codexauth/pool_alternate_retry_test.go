package codexauth

import (
	"testing"
	"time"
)

func TestPoolSelectorPickAlternateIsRequestLocal(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	for _, strategy := range []PoolStrategy{PoolStrategyQuota, PoolStrategyRoundRobin, PoolStrategyFillFirst, PoolStrategyResetWindow} {
		t.Run(string(strategy), func(t *testing.T) {
			selector := NewPoolSelector(PoolSelectorDependencies{})
			input := poolSelectorFixture(now)
			input.Strategy = strategy
			input.StickyLimit = 1
			input.ThreadID = "thread"
			input.ExcludeAccountID = "a"
			input.Accounts.Accounts = []ManagedAccount{{ID: "a", Plan: "plus"}, {ID: "b", Plan: "plus"}}
			input.Accounts.ActiveAccountID = "a"
			input.Accounts.PinnedAccountID = "a"
			input.Credentials = credentialSnapshotFor("a", "b")
			input.Quotas = map[string]*QuotaSnapshot{
				"a": {WeeklyPercent: float64Ptr(10)},
				"b": {WeeklyPercent: float64Ptr(20)},
			}
			if !selector.Affinity.Bind("thread", "a", AffinityScopeLegacy, now, input.Credentials) {
				t.Fatal("bind failed")
			}

			got := selector.PickAlternate(input)
			if got.Status != PoolSelectionSelected || got.AccountID != "b" {
				t.Fatalf("alternate=%#v", got)
			}
			if got.ActiveChanged || got.PersistActive || got.PinReleased {
				t.Fatalf("request-local alternate reported durable mutation: %#v", got)
			}
			if active := selector.RuntimeActive(); active != "" {
				t.Fatalf("request-local alternate moved runtime active=%q", active)
			}
			affinity := selector.Affinity.Resolve("thread", AffinityScopeLegacy, now, input.Credentials)
			if affinity.Status != AffinitySelected || affinity.AccountID != "a" {
				t.Fatalf("request-local alternate rebound affinity=%#v", affinity)
			}
		})
	}
}

func TestPoolSelectorPromoteQuotaAlternateRunsAfterOutcomeWithoutBindingAffinity(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	selector := NewPoolSelector(PoolSelectorDependencies{})
	input := poolSelectorFixture(now)
	input.ThreadID = "thread"
	input.Accounts.Accounts = []ManagedAccount{{ID: "a"}, {ID: "b"}}
	input.Accounts.ActiveAccountID = "a"
	input.Credentials = credentialSnapshotFor("a", "b")
	if !selector.Affinity.Bind("thread", "a", AffinityScopeLegacy, now, input.Credentials) {
		t.Fatal("bind failed")
	}

	if !selector.PromoteQuotaAlternate(input, "a", "b") {
		t.Fatal("expected active alternate promotion")
	}
	if active := selector.RuntimeActive(); active != "b" {
		t.Fatalf("runtime active=%q", active)
	}
	affinity := selector.Affinity.Resolve("thread", AffinityScopeLegacy, now, input.Credentials)
	if affinity.Status != AffinitySelected || affinity.AccountID != "a" {
		t.Fatalf("quota promotion rebound affinity=%#v", affinity)
	}
}

func TestPoolSelectorPromoteQuotaAlternateDoesNotClobberNewerActiveOrSparkScope(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)

	t.Run("newer active", func(t *testing.T) {
		selector := NewPoolSelector(PoolSelectorDependencies{})
		selector.runtimeActive = "newer"
		input := poolSelectorFixture(now)
		input.Accounts.ActiveAccountID = "a"
		if selector.PromoteQuotaAlternate(input, "a", "b") {
			t.Fatal("stale retry clobbered newer active account")
		}
		if active := selector.RuntimeActive(); active != "newer" {
			t.Fatalf("runtime active=%q", active)
		}
	})

	t.Run("spark", func(t *testing.T) {
		selector := NewPoolSelector(PoolSelectorDependencies{})
		input := poolSelectorFixture(now)
		input.Scope = QuotaScopeSpark
		input.Accounts.ActiveAccountID = "a"
		if selector.PromoteQuotaAlternate(input, "a", "b") {
			t.Fatal("independent Spark scope moved shared active account")
		}
		if active := selector.RuntimeActive(); active != "" {
			t.Fatalf("runtime active=%q", active)
		}
	})
}
