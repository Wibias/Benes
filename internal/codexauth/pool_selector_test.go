package codexauth

import (
	"testing"
	"time"
)

func TestPoolSelectorQuotaFiltersRoutingFencesAndPicksLowestUsageInTopTier(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	selector := NewPoolSelector(PoolSelectorDependencies{})
	input := poolSelectorFixture(now)
	input.Accounts.Accounts = []ManagedAccount{
		{ID: "high-a", Plan: "plus"},
		{ID: "high-b", Plan: "plus"},
		{ID: "low", Plan: "plus"},
		{ID: "soft", Plan: "plus"},
		{ID: "reauth", Plan: "plus"},
	}
	input.Accounts.Priorities = map[string]int{"high-a": 10, "high-b": 10, "low": 0, "soft": 10, "reauth": 10}
	input.Credentials = credentialSnapshotFor("high-a", "high-b", "low", "soft", "reauth")
	input.Quotas = map[string]*QuotaSnapshot{
		"high-a": {WeeklyPercent: float64Ptr(60)},
		"high-b": {WeeklyPercent: float64Ptr(20)},
		"low":    {WeeklyPercent: float64Ptr(1)},
		"soft":   {WeeklyPercent: float64Ptr(2)},
		"reauth": {WeeklyPercent: float64Ptr(3)},
	}
	selector.Health.SetSoftAvoid("soft", now.Add(time.Minute))
	selector.Reauth.Mark("reauth", 1)

	got := selector.Resolve(input)
	if got.Status != PoolSelectionSelected || got.AccountID != "high-b" || got.Generation != 1 || !got.GenerationKnown {
		t.Fatalf("selection=%#v", got)
	}
}

func TestPoolSelectorAffinityReuseAndQuotaReevaluation(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	selector := NewPoolSelector(PoolSelectorDependencies{})
	input := poolSelectorFixture(now)
	input.ThreadID = "thread"
	input.Accounts.Accounts = []ManagedAccount{{ID: "a", Plan: "plus"}, {ID: "b", Plan: "plus"}}
	input.Credentials = credentialSnapshotFor("a", "b")
	input.Quotas = map[string]*QuotaSnapshot{"a": {WeeklyPercent: float64Ptr(10)}, "b": {WeeklyPercent: float64Ptr(1)}}
	if !selector.Affinity.Bind("thread", "a", AffinityScopeLegacy, now, input.Credentials) {
		t.Fatal("bind failed")
	}

	input.Strategy = PoolStrategyRoundRobin
	got := selector.Resolve(input)
	if got.AccountID != "a" || got.Reason != PoolSelectionAffinity {
		t.Fatalf("round-robin affinity=%#v", got)
	}

	input.Strategy = PoolStrategyQuota
	input.Now = now.Add(ThreadAffinityReevaluationInterval)
	got = selector.Resolve(input)
	if got.AccountID != "a" || got.Reason != PoolSelectionAffinity {
		t.Fatalf("under-threshold re-evaluation moved affinity=%#v", got)
	}

	input.Now = now.Add(ThreadAffinityReevaluationInterval + time.Second)
	input.Quotas["a"] = &QuotaSnapshot{WeeklyPercent: float64Ptr(90)}
	got = selector.Resolve(input)
	if got.AccountID != "b" || got.Reason != PoolSelectionQuotaReevaluation {
		t.Fatalf("over-threshold re-evaluation=%#v", got)
	}
	resolved := selector.Affinity.Resolve("thread", AffinityScopeLegacy, input.Now, input.Credentials)
	if resolved.AccountID != "b" {
		t.Fatalf("affinity not rebound=%#v", resolved)
	}
}

func TestPoolSelectorAffinityLeavesSoftAvoidedOrFailoverAccount(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	for _, tc := range []struct {
		name string
		mark func(*PoolSelector)
	}{
		{
			name: "soft avoid",
			mark: func(selector *PoolSelector) { selector.Health.SetSoftAvoid("a", now.Add(time.Minute)) },
		},
		{
			name: "failure streak",
			mark: func(selector *PoolSelector) {
				selector.Failures.RecordFailure("a", now, 502, 1)
				selector.Failures.RecordFailure("a", now, 502, 1)
				selector.Failures.RecordFailure("a", now, 502, 1)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			selector := NewPoolSelector(PoolSelectorDependencies{})
			input := poolSelectorFixture(now)
			input.ThreadID = "thread"
			input.Accounts.Accounts = []ManagedAccount{{ID: "a", Plan: "plus"}, {ID: "b", Plan: "plus"}}
			input.Credentials = credentialSnapshotFor("a", "b")
			input.Quotas = map[string]*QuotaSnapshot{"a": {WeeklyPercent: float64Ptr(10)}, "b": {WeeklyPercent: float64Ptr(20)}}
			if !selector.Affinity.Bind("thread", "a", AffinityScopeLegacy, now, input.Credentials) {
				t.Fatal("bind failed")
			}
			tc.mark(selector)
			got := selector.Resolve(input)
			if got.AccountID != "b" || got.Reason == PoolSelectionAffinity {
				t.Fatalf("selection=%#v", got)
			}
		})
	}
}

func TestPoolSelectorExpiredAffinityReturnsExpiredWithoutSelectingReplacement(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	selector := NewPoolSelector(PoolSelectorDependencies{})
	input := poolSelectorFixture(now)
	input.ThreadID = "thread"
	input.Accounts.Accounts = []ManagedAccount{{ID: "a"}, {ID: "b"}}
	input.Credentials = credentialSnapshotFor("a", "b")
	if !selector.Affinity.Bind("thread", "a", AffinityScopeLegacy, now, input.Credentials) {
		t.Fatal("bind failed")
	}
	input.Now = now.Add(ThreadAffinityIdleTTL + time.Nanosecond)
	got := selector.Resolve(input)
	if got.Status != PoolSelectionExpired || got.AccountID != "a" {
		t.Fatalf("selection=%#v", got)
	}
}

func TestPoolSelectorHardCooldownUsesFallbackOrMarksProbeRequired(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	selector := NewPoolSelector(PoolSelectorDependencies{})
	input := poolSelectorFixture(now)
	input.Accounts.Accounts = []ManagedAccount{{ID: "a"}, {ID: "b"}}
	input.Accounts.ActiveAccountID = "a"
	input.Credentials = credentialSnapshotFor("a", "b")
	input.Quotas = map[string]*QuotaSnapshot{"a": nil, "b": nil}
	selector.Health.SetHardCooldown("a", now.Add(time.Minute), CooldownSourceRetryAfter)

	got := selector.Resolve(input)
	if got.AccountID != "b" || got.RequiresProbe {
		t.Fatalf("fallback=%#v", got)
	}

	input.Accounts.Accounts = []ManagedAccount{{ID: "a"}}
	input.Credentials = credentialSnapshotFor("a")
	got = selector.Resolve(input)
	if got.AccountID != "a" || !got.RequiresProbe || !got.Degraded {
		t.Fatalf("probe selection=%#v", got)
	}
}

func TestPoolSelectorRoundRobinAndFillFirstAreNewSessionOnly(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	selector := NewPoolSelector(PoolSelectorDependencies{})
	input := poolSelectorFixture(now)
	input.Accounts.Accounts = []ManagedAccount{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	input.Credentials = credentialSnapshotFor("a", "b", "c")
	input.Strategy = PoolStrategyRoundRobin
	input.StickyLimit = 1

	input.ThreadID = "t1"
	first := selector.Resolve(input)
	input.ThreadID = "t2"
	second := selector.Resolve(input)
	if first.AccountID != "a" || second.AccountID != "b" {
		t.Fatalf("round-robin first=%#v second=%#v", first, second)
	}
	input.ThreadID = "t1"
	again := selector.Resolve(input)
	if again.AccountID != "a" || again.Reason != PoolSelectionAffinity {
		t.Fatalf("existing thread rotated=%#v", again)
	}

	selector = NewPoolSelector(PoolSelectorDependencies{})
	input.Strategy = PoolStrategyFillFirst
	input.Accounts.ActiveAccountID = "b"
	input.Quotas = map[string]*QuotaSnapshot{"a": nil, "b": {WeeklyPercent: float64Ptr(10)}, "c": nil}
	input.ThreadID = "fill"
	fill := selector.Resolve(input)
	if fill.AccountID != "b" {
		t.Fatalf("fill-first=%#v", fill)
	}
}

func TestPoolSelectorPriorityPreemptsRecoveredHigherTierUnlessLivePinCapsIt(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	selector := NewPoolSelector(PoolSelectorDependencies{})
	input := poolSelectorFixture(now)
	input.Accounts.Accounts = []ManagedAccount{{ID: "high", Plan: "plus"}, {ID: "low", Plan: "plus"}}
	input.Accounts.Priorities = map[string]int{"high": 10, "low": 0}
	input.Accounts.ActiveAccountID = "low"
	input.Credentials = credentialSnapshotFor("high", "low")
	input.Quotas = map[string]*QuotaSnapshot{"high": {WeeklyPercent: float64Ptr(5)}, "low": {WeeklyPercent: float64Ptr(10)}}

	got := selector.Resolve(input)
	if got.AccountID != "high" || got.Reason != PoolSelectionPriorityPreemption {
		t.Fatalf("preemption=%#v", got)
	}

	selector = NewPoolSelector(PoolSelectorDependencies{})
	input.Accounts.PinnedAccountID = "low"
	got = selector.Resolve(input)
	if got.AccountID != "low" {
		t.Fatalf("live pin was preempted=%#v", got)
	}
}

func TestPoolSelectorSparkScopeDoesNotMoveSharedRuntimeCursor(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	selector := NewPoolSelector(PoolSelectorDependencies{})
	input := poolSelectorFixture(now)
	input.Accounts.Accounts = []ManagedAccount{{ID: "a"}, {ID: "b"}}
	input.Credentials = credentialSnapshotFor("a", "b")
	input.Strategy = PoolStrategyRoundRobin
	input.ThreadID = "shared"
	shared := selector.Resolve(input)
	if shared.AccountID != "a" {
		t.Fatalf("shared=%#v", shared)
	}

	input.Scope = QuotaScopeSpark
	input.ThreadID = "spark"
	spark := selector.Resolve(input)
	if spark.AccountID != "a" {
		t.Fatalf("spark=%#v", spark)
	}
	if got := selector.RuntimeActive(); got != "a" {
		t.Fatalf("spark changed shared runtime active=%q", got)
	}
}

func poolSelectorFixture(now time.Time) PoolSelectionInput {
	threshold := DefaultAutoSwitchThreshold
	failover := 3
	return PoolSelectionInput{
		Accounts:            ManagedAccountConfig{PausedAccountIDs: map[string]bool{}, Priorities: map[string]int{}},
		Credentials:         ManagedCredentialSnapshot{Status: ManagedCredentialStoreOK, Records: map[string]ManagedCredentialRecord{}},
		IncludeMain:         false,
		Strategy:            PoolStrategyQuota,
		StickyLimit:         1,
		AutoSwitchThreshold: &threshold,
		FailoverThreshold:   &failover,
		Now:                 now,
		Quotas:              map[string]*QuotaSnapshot{},
	}
}

func credentialSnapshotFor(ids ...string) ManagedCredentialSnapshot {
	records := make(map[string]ManagedCredentialRecord, len(ids))
	for _, id := range ids {
		records[id] = ManagedCredentialRecord{
			Generation: 1,
			Credential: &ManagedCredential{AccessToken: "access-" + id, RefreshToken: "refresh-" + id, ChatGPTAccountID: "chat-" + id},
		}
	}
	return ManagedCredentialSnapshot{Status: ManagedCredentialStoreOK, Records: records}
}
