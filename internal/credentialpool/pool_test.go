package credentialpool

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func testCandidate(ref, label, destination, authClass string, priority int, evidence Evidence) Candidate {
	return Candidate{
		Ref:           ref,
		ProviderLabel: label,
		Destination:   destination,
		AuthClass:     authClass,
		Priority:      priority,
		Evidence:      evidence,
	}
}

func healthyKnown(utilization float64, now time.Time) Evidence {
	return Evidence{
		Auth:        AuthUsable,
		Quota:       QuotaKnown,
		Utilization: utilization,
		ObservedAt:  now,
		ValidUntil:  now.Add(time.Minute),
		Limit:       LimitAvailable,
	}
}

func TestPoolKeepsHealthySessionAffinity(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	pool := New([]Candidate{
		testCandidate("acct-a", "openai", "https://chatgpt.com/backend-api/codex", "oauth", 10, healthyKnown(0.20, now)),
		testCandidate("acct-b", "openai", "https://chatgpt.com/backend-api/codex", "oauth", 50, healthyKnown(0.05, now)),
	})

	request := Request{AffinityKey: "thread-1", Destination: "https://chatgpt.com/backend-api/codex", AuthClass: "oauth", Now: now}
	first, err := pool.Select(request)
	if err != nil {
		t.Fatalf("first select: %v", err)
	}
	if first.Candidate.Ref != "acct-b" {
		t.Fatalf("first candidate = %q, want acct-b", first.Candidate.Ref)
	}
	if first.Reason != ReasonSelected {
		t.Fatalf("first reason = %q", first.Reason)
	}

	pool.Upsert(testCandidate("acct-c", "renamed-provider", "https://chatgpt.com/backend-api/codex", "oauth", 100, healthyKnown(0, now)))
	second, err := pool.Select(request)
	if err != nil {
		t.Fatalf("second select: %v", err)
	}
	if second.Candidate.Ref != "acct-b" || second.Reason != ReasonAffinity {
		t.Fatalf("affinity moved: candidate=%q reason=%q", second.Candidate.Ref, second.Reason)
	}
}

func TestPoolFailsOverOAuthAccountAndRebindsPortableAffinity(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	pool := New([]Candidate{
		testCandidate("acct-a", "openai", "dest", "oauth", 20, healthyKnown(0.10, now)),
		testCandidate("acct-b", "openai", "dest", "oauth", 10, healthyKnown(0.20, now)),
	})
	request := Request{AffinityKey: "portable-thread", Destination: "dest", AuthClass: "oauth", Portable: true, Now: now}
	first, err := pool.Select(request)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if first.Candidate.Ref != "acct-a" {
		t.Fatalf("candidate = %q", first.Candidate.Ref)
	}

	failover, err := pool.NextAfterFailure(request, "acct-a", Failure{Class: FailureQuotaExhausted, RetryAt: now.Add(time.Minute)})
	if err != nil {
		t.Fatalf("failover: %v", err)
	}
	if failover.Candidate.Ref != "acct-b" || failover.Reason != ReasonFailedOver {
		t.Fatalf("failover = %#v", failover)
	}

	again, err := pool.Select(request)
	if err != nil {
		t.Fatalf("select rebound affinity: %v", err)
	}
	if again.Candidate.Ref != "acct-b" || again.Reason != ReasonAffinity {
		t.Fatalf("affinity not rebound: %#v", again)
	}
}

func TestPoolRotatesAPIKeyOnRateLimitWithoutBurningSameKeyRetry(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	pool := New([]Candidate{
		testCandidate("key-a", "opencode-go-a", "https://opencode.ai/zen/v1", "api-key", 10, healthyKnown(0.10, now)),
		testCandidate("key-b", "custom-name", "https://opencode.ai/zen/v1", "api-key", 5, healthyKnown(0.20, now)),
	})
	request := Request{Destination: "https://opencode.ai/zen/v1", AuthClass: "api-key", Portable: true, Now: now}
	first, err := pool.Select(request)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if first.Candidate.Ref != "key-a" {
		t.Fatalf("candidate = %q", first.Candidate.Ref)
	}
	second, err := pool.NextAfterFailure(request, "key-a", Failure{Class: FailureRateLimited, RetryAt: now.Add(30 * time.Second)})
	if err != nil {
		t.Fatalf("failover: %v", err)
	}
	if second.Candidate.Ref != "key-b" {
		t.Fatalf("rate-limited key retried instead of rotating: %q", second.Candidate.Ref)
	}
}

func TestPoolOverageEnabledAtCapStaysEligibleAndDoesNotChangeCodexExhaustion(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	overage := healthyKnown(1, now)
	overage.OverageEnabled = true
	pool := New([]Candidate{
		testCandidate("codex-exhausted", "openai", "https://chatgpt.com/backend-api/codex", "oauth", 1, healthyKnown(1, now)),
		testCandidate("kiro-overage", "kiro", "kiro", "oauth", 1, overage),
		testCandidate("kiro-headroom", "kiro", "kiro", "oauth", 1, healthyKnown(0.2, now)),
	})
	codex, err := pool.Select(Request{Destination: "https://chatgpt.com/backend-api/codex", AuthClass: "oauth", Now: now})
	if err == nil {
		t.Fatalf("codex exhausted account became eligible: %#v", codex)
	}
	kiro, err := pool.Select(Request{Destination: "kiro", AuthClass: "oauth", Now: now})
	if err != nil {
		t.Fatalf("kiro select: %v", err)
	}
	if kiro.Candidate.Ref != "kiro-headroom" {
		t.Fatalf("headroom lost to overage-at-cap or exhausted: %q", kiro.Candidate.Ref)
	}
	failover, err := pool.NextAfterFailure(Request{Destination: "kiro", AuthClass: "oauth", Portable: true, Now: now}, "kiro-headroom", Failure{Class: FailureRateLimited, RetryAt: now.Add(time.Minute)})
	if err != nil {
		t.Fatalf("overage failover: %v", err)
	}
	if failover.Candidate.Ref != "kiro-overage" {
		t.Fatalf("overage-at-cap was not usable after 429: %q", failover.Candidate.Ref)
	}
}

func TestPoolCapabilityUsesDestinationAndAuthClassNotProviderLabel(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	pool := New([]Candidate{
		testCandidate("one", "friendly-a", "canonical", "api-key", 1, healthyKnown(0.30, now)),
		testCandidate("two", "friendly-b", "canonical", "api-key", 2, healthyKnown(0.20, now)),
		testCandidate("lookalike", "friendly-b", "canonical.example.evil", "api-key", 100, healthyKnown(0, now)),
	})
	decision, err := pool.Select(Request{Destination: "canonical", AuthClass: "api-key", Now: now})
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if decision.Candidate.Ref != "two" {
		t.Fatalf("provider label affected capability or lookalike leaked in: %q", decision.Candidate.Ref)
	}
}

func TestPoolTreatsStaleKnownQuotaAsUnknownButDoesNotDisableIt(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	stale := healthyKnown(0.01, now.Add(-2*time.Minute))
	stale.ValidUntil = now.Add(-time.Second)
	unknown := Evidence{Auth: AuthUsable, Quota: QuotaUnknown, ObservedAt: now, Limit: LimitAvailable}
	pool := New([]Candidate{
		testCandidate("stale", "p", "dest", "oauth", 20, stale),
		testCandidate("unknown", "p", "dest", "oauth", 10, unknown),
	})
	decision, err := pool.Select(Request{Destination: "dest", AuthClass: "oauth", Now: now})
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if decision.Candidate.Ref != "stale" {
		t.Fatalf("stale quota should remain eligible and priority should decide among unknown evidence: %q", decision.Candidate.Ref)
	}
	if decision.Reason != ReasonQuotaUnknown {
		t.Fatalf("reason = %q, want quota_unknown", decision.Reason)
	}
}

func TestPoolRealZeroPercentQuotaIsKnownHealthyCapacity(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	pool := New([]Candidate{
		testCandidate("zero", "p", "dest", "oauth", 1, healthyKnown(0, now)),
		testCandidate("unknown", "p", "dest", "oauth", 100, Evidence{Auth: AuthUsable, Quota: QuotaUnknown, Limit: LimitAvailable, ObservedAt: now}),
	})
	decision, err := pool.Select(Request{Destination: "dest", AuthClass: "oauth", Now: now})
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if decision.Candidate.Ref != "zero" {
		t.Fatalf("known 0%% capacity was not preferred: %q", decision.Candidate.Ref)
	}
}

func TestPoolAuthFailureMarksReauthAndExcludesCandidate(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	pool := New([]Candidate{
		testCandidate("bad", "p", "dest", "oauth", 20, healthyKnown(0.10, now)),
		testCandidate("good", "p", "dest", "oauth", 10, healthyKnown(0.20, now)),
	})
	pool.ReportFailure("bad", Failure{Class: FailureAuthUnusable})
	decision, err := pool.Select(Request{Destination: "dest", AuthClass: "oauth", Now: now})
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if decision.Candidate.Ref != "good" {
		t.Fatalf("reauth candidate selected: %q", decision.Candidate.Ref)
	}
	if !traceHas(decision.Trace, "bad", ReasonReauthRequired) {
		t.Fatalf("missing reauth trace: %#v", decision.Trace)
	}
}

func TestPoolGenericForbiddenDoesNotPoisonOrTriggerAutomaticFailover(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	pool := New([]Candidate{
		testCandidate("a", "p", "dest", "oauth", 20, healthyKnown(0.10, now)),
		testCandidate("b", "p", "dest", "oauth", 10, healthyKnown(0.20, now)),
	})
	request := Request{Destination: "dest", AuthClass: "oauth", Portable: true, Now: now}
	if _, err := pool.NextAfterFailure(request, "a", Failure{Class: FailureGenericForbidden}); !errors.Is(err, ErrFailureNotEligible) {
		t.Fatalf("generic 403 triggered failover: %v", err)
	}
	decision, err := pool.Select(request)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if decision.Candidate.Ref != "a" {
		t.Fatalf("generic 403 poisoned candidate: %q", decision.Candidate.Ref)
	}
}

func TestPoolCooldownExpiresAndCandidateReenters(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	pool := New([]Candidate{
		testCandidate("a", "p", "dest", "api-key", 20, healthyKnown(0.10, now)),
		testCandidate("b", "p", "dest", "api-key", 10, healthyKnown(0.20, now)),
	})
	pool.ReportFailure("a", Failure{Class: FailureRateLimited, RetryAt: now.Add(30 * time.Second)})
	before, err := pool.Select(Request{Destination: "dest", AuthClass: "api-key", Now: now})
	if err != nil {
		t.Fatalf("select during cooldown: %v", err)
	}
	if before.Candidate.Ref != "b" || !traceHas(before.Trace, "a", ReasonCooldown) {
		t.Fatalf("cooldown not honored: %#v", before)
	}
	after, err := pool.Select(Request{Destination: "dest", AuthClass: "api-key", Now: now.Add(31 * time.Second)})
	if err != nil {
		t.Fatalf("select after cooldown: %v", err)
	}
	if after.Candidate.Ref != "a" {
		t.Fatalf("candidate did not re-enter: %q", after.Candidate.Ref)
	}
}

func TestPoolReturnsTruthfulNoCandidateInsteadOfCarousel(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	pool := New([]Candidate{
		testCandidate("a", "p", "dest", "oauth", 1, Evidence{Auth: AuthReauthRequired, Limit: LimitAvailable}),
		testCandidate("b", "p", "dest", "oauth", 1, Evidence{Auth: AuthUsable, Quota: QuotaKnown, Utilization: 1, ValidUntil: now.Add(time.Minute), Limit: LimitAvailable}),
	})
	_, err := pool.Select(Request{Destination: "dest", AuthClass: "oauth", Now: now})
	if !errors.Is(err, ErrNoEligibleCandidate) {
		t.Fatalf("expected bounded no-candidate error, got %v", err)
	}
}

func TestPoolConcurrentSessionsKeepIndependentAffinity(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	pool := New([]Candidate{
		testCandidate("a", "p", "dest", "oauth", 20, healthyKnown(0.10, now)),
		testCandidate("b", "p", "dest", "oauth", 10, healthyKnown(0.20, now)),
	})

	var wg sync.WaitGroup
	errCh := make(chan error, 32)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			key := "session-even"
			if index%2 == 1 {
				key = "session-odd"
			}
			first, err := pool.Select(Request{AffinityKey: key, Destination: "dest", AuthClass: "oauth", Now: now})
			if err != nil {
				errCh <- err
				return
			}
			second, err := pool.Select(Request{AffinityKey: key, Destination: "dest", AuthClass: "oauth", Now: now})
			if err != nil {
				errCh <- err
				return
			}
			if first.Candidate.Ref != second.Candidate.Ref {
				errCh <- errors.New("affinity changed within session")
			}
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}
}

func TestPoolFailoverReturnsCompleteNewAuthoritySnapshot(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	a := testCandidate("a", "p", "dest", "oauth", 20, healthyKnown(0.10, now))
	a.AuthorityGeneration = 7
	a.RoutingMetadata = map[string]string{"project": "project-a", "profile": "profile-a"}
	b := testCandidate("b", "p", "dest", "oauth", 10, healthyKnown(0.20, now))
	b.AuthorityGeneration = 11
	b.RoutingMetadata = map[string]string{"project": "project-b", "profile": "profile-b"}
	pool := New([]Candidate{a, b})
	request := Request{Destination: "dest", AuthClass: "oauth", Portable: true, Now: now}
	decision, err := pool.NextAfterFailure(request, "a", Failure{Class: FailureQuotaExhausted, RetryAt: now.Add(time.Minute)})
	if err != nil {
		t.Fatalf("failover: %v", err)
	}
	if decision.Candidate.Ref != "b" || decision.Candidate.AuthorityGeneration != 11 || decision.Candidate.RoutingMetadata["project"] != "project-b" || decision.Candidate.RoutingMetadata["profile"] != "profile-b" {
		t.Fatalf("mixed authority snapshot: %#v", decision.Candidate)
	}
	decision.Candidate.RoutingMetadata["project"] = "mutated"
	again, err := pool.Select(request)
	if err != nil {
		t.Fatalf("select again: %v", err)
	}
	if again.Candidate.RoutingMetadata["project"] != "project-b" {
		t.Fatal("caller mutated pool-owned routing metadata")
	}
}

func TestPoolNeverReplaysAfterDownstreamCommit(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	pool := New([]Candidate{
		testCandidate("a", "p", "dest", "api-key", 20, healthyKnown(0.10, now)),
		testCandidate("b", "p", "dest", "api-key", 10, healthyKnown(0.20, now)),
	})
	_, err := pool.NextAfterFailure(Request{Destination: "dest", AuthClass: "api-key", Portable: true, Committed: true, Now: now}, "a", Failure{Class: FailureRateLimited, RetryAt: now.Add(time.Minute)})
	if !errors.Is(err, ErrOutputCommitted) {
		t.Fatalf("post-commit failover allowed: %v", err)
	}
}

func TestPoolNonPortableAffinityDoesNotSwitchPhysicalOwner(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	pool := New([]Candidate{
		testCandidate("a", "p", "dest", "oauth", 20, healthyKnown(0.10, now)),
		testCandidate("b", "p", "dest", "oauth", 10, healthyKnown(0.20, now)),
	})
	request := Request{AffinityKey: "account-bound", Destination: "dest", AuthClass: "oauth", Portable: false, Now: now}
	first, err := pool.Select(request)
	if err != nil || first.Candidate.Ref != "a" {
		t.Fatalf("initial affinity: %#v %v", first, err)
	}
	pool.ReportFailure("a", Failure{Class: FailureQuotaExhausted, RetryAt: now.Add(time.Minute)})
	if _, err := pool.Select(request); !errors.Is(err, ErrAffinityUnavailable) {
		t.Fatalf("non-portable affinity switched instead of stopping: %v", err)
	}
}

func TestPoolPriorityNeverOverridesKnownUnusableState(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	pool := New([]Candidate{
		testCandidate("bad", "p", "dest", "oauth", 10_000, Evidence{Auth: AuthUsable, Quota: QuotaKnown, Utilization: 1, ValidUntil: now.Add(time.Minute), Limit: LimitAvailable}),
		testCandidate("good", "p", "dest", "oauth", -10, healthyKnown(0.90, now)),
	})
	decision, err := pool.Select(Request{Destination: "dest", AuthClass: "oauth", Now: now})
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if decision.Candidate.Ref != "good" {
		t.Fatalf("priority overrode exhaustion: %q", decision.Candidate.Ref)
	}
}

func traceHas(trace []Observation, ref string, reason Reason) bool {
	for _, item := range trace {
		if item.CandidateRef == ref && item.Reason == reason {
			return true
		}
	}
	return false
}
