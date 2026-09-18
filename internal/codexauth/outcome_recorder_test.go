package codexauth

import (
	"testing"
	"time"
)

func TestClassifyUpstreamOutcomeAndQuotaCooldown(t *testing.T) {
	cases := []struct {
		outcome UpstreamOutcome
		denial  OutcomeDenial
		want    OutcomeClass
	}{
		{HTTPOutcome(204), "", OutcomeSuccess},
		{HTTPOutcome(307), "", OutcomeNeutral},
		{HTTPOutcome(401), "", OutcomeCredential},
		{HTTPOutcome(403), "", OutcomeCredential},
		{HTTPOutcome(403), DenialWorkspace, OutcomeWorkspace},
		{HTTPOutcome(403), DenialEntitlement, OutcomeWorkspace},
		{HTTPOutcome(402), "", OutcomeQuota},
		{HTTPOutcome(429), "", OutcomeQuota},
		{HTTPOutcome(400), "", OutcomeCaller},
		{HTTPOutcome(503), "", OutcomeTransient},
		{UpstreamOutcome{Kind: OutcomeConnectError}, "", OutcomeTransient},
		{UpstreamOutcome{Kind: OutcomeTimeout}, "", OutcomeTransient},
		{UpstreamOutcome{Kind: OutcomeConnectNeutral}, "", OutcomeNeutral},
		{HTTPOutcome(700), "", OutcomeUnknown},
	}
	for _, tc := range cases {
		if got := ClassifyUpstreamOutcome(tc.outcome, tc.denial); got != tc.want {
			t.Fatalf("outcome=%#v class=%q want=%q", tc.outcome, got, tc.want)
		}
	}

	now := time.Date(2026, 8, 18, 7, 0, 0, 0, time.UTC)
	cooldown := ComputeOutcomeQuotaCooldown(OutcomeMeta{
		Now: now, RetryAfter: "7200", ResetAt: []any{now.Add(2 * time.Minute).Unix()},
	})
	if cooldown.Source != CooldownSourceRetryAfter || !cooldown.Until.Equal(now.Add(2*time.Hour)) {
		t.Fatalf("retry cooldown=%#v", cooldown)
	}
	cooldown = ComputeOutcomeQuotaCooldown(OutcomeMeta{Now: now, RetryAfter: "999999999"})
	if cooldown.Source != CooldownSourceRetryAfter || !cooldown.Until.Equal(now.Add(24*time.Hour)) {
		t.Fatalf("retry clamp=%#v", cooldown)
	}
	cooldown = ComputeOutcomeQuotaCooldown(OutcomeMeta{Now: now, ResetAt: []any{
		now.Add(30 * time.Minute).Unix(), now.Add(2 * time.Minute).UnixMilli(),
	}})
	if cooldown.Source != CooldownSourceResetDerived || !cooldown.Until.Equal(now.Add(2*time.Minute)) {
		t.Fatalf("reset cooldown=%#v", cooldown)
	}
	cooldown = ComputeOutcomeQuotaCooldown(OutcomeMeta{Now: now, ResetAt: []any{now.Add(10 * time.Hour).Unix()}})
	if cooldown.Source != CooldownSourceResetDerived || !cooldown.Until.Equal(now.Add(15*time.Minute)) {
		t.Fatalf("reset clamp=%#v", cooldown)
	}
	cooldown = ComputeOutcomeQuotaCooldown(OutcomeMeta{Now: now})
	if cooldown.Source != CooldownSourceDefault || !cooldown.Until.Equal(now.Add(time.Minute)) {
		t.Fatalf("default cooldown=%#v", cooldown)
	}
}

func TestExtractOutcomeDenialReadsDetailErrorAndRootCodes(t *testing.T) {
	cases := []struct {
		body string
		want OutcomeDenial
	}{
		{`{"detail":{"code":"codex_workspace_access_denied"}}`, DenialWorkspace},
		{`{"detail":{"code":"invalid_workspace_selected"}}`, DenialWorkspace},
		{`{"error":{"code":"workspace_access_denied"}}`, DenialWorkspace},
		{`{"code":"entitlement_missing"}`, DenialEntitlement},
		{`{"detail":{"code":"codex_entitlement_missing"}}`, DenialEntitlement},
		{`{"detail":{"code":"permission_denied"},"code":"workspace_access_denied"}`, ""},
		{`{"detail":["not-an-object"],"code":"workspace_access_denied"}`, ""},
		{`{"code":1}`, ""},
		{`{"detail":{"code":7}}`, ""},
		{`[1,2]`, ""},
		{`{`, ""},
	}
	for _, tc := range cases {
		got := ExtractOutcomeDenial([]byte(tc.body))
		if got != tc.want {
			t.Fatalf("body=%s got=%q want=%q", tc.body, got, tc.want)
		}
	}
	if got := ClassifyUpstreamOutcome(HTTPOutcome(403), ExtractOutcomeDenial([]byte(`{"detail":{"code":"codex_workspace_access_denied"}}`))); got != OutcomeWorkspace {
		t.Fatalf("detail workspace class=%q", got)
	}
}

func TestOutcomeRecorderAccountWideProbeSuccessClearsCooldownAndTransientStreak(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	recorder := NewOutcomeRecorder(OutcomeRecorderDependencies{})
	recorder.Health.SetHardCooldownAt("acct", now, now.Add(time.Hour), CooldownSourceResetDerived)
	lease, ok, err := recorder.Health.TryAcquireHardCooldownProbe("acct", now.Add(QuotaProbeInterval))
	if err != nil || !ok {
		t.Fatalf("lease=%#v ok=%v err=%v", lease, ok, err)
	}
	recorder.Failures.RecordFailure("acct", now, 502, 1)
	recorder.Failures.RecordFailure("acct", now, 502, 1)
	recorder.Health.SetSoftAvoid("acct", now.Add(time.Hour))

	recorder.Record("acct", HTTPOutcome(200), OutcomeMeta{
		Now: now.Add(QuotaProbeInterval + time.Second), ProbeLease: &lease, WriterGeneration: 1,
	})
	if _, ok := recorder.Health.HardCooldown("acct", now.Add(QuotaProbeInterval+time.Second)); ok {
		t.Fatal("current-generation probe did not clear cooldown")
	}
	if _, ok := recorder.Health.SoftAvoidUntil("acct", now.Add(QuotaProbeInterval+time.Second)); ok {
		t.Fatal("probe recovery kept soft avoid")
	}
	if _, ok := recorder.Failures.Snapshot("acct"); ok {
		t.Fatal("account-wide probe recovery kept transient streak")
	}
}

func TestOutcomeRecorderStaleProbeSuccessPreservesNewerCooldown(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	recorder := NewOutcomeRecorder(OutcomeRecorderDependencies{})
	recorder.Health.SetHardCooldownAt("acct", now, now.Add(time.Hour), CooldownSourceResetDerived)
	lease, ok, _ := recorder.Health.TryAcquireHardCooldownProbe("acct", now.Add(QuotaProbeInterval))
	if !ok {
		t.Fatal("probe lease not granted")
	}
	renewedAt := now.Add(QuotaProbeInterval + time.Second)
	recorder.Health.SetHardCooldownAt("acct", renewedAt, renewedAt.Add(2*time.Hour), CooldownSourceRetryAfter)
	recorder.Record("acct", HTTPOutcome(200), OutcomeMeta{
		Now: renewedAt.Add(time.Second), ProbeLease: &lease, WriterGeneration: 1,
	})
	cooldown, ok := recorder.Health.HardCooldown("acct", renewedAt.Add(time.Second))
	if !ok || cooldown.Source != CooldownSourceRetryAfter || cooldown.Generation != 2 || cooldown.ProbeLeaseID != "" {
		t.Fatalf("cooldown=%#v ok=%v", cooldown, ok)
	}
}

func TestOutcomeRecorderCallerNeutralAndWorkspaceProbeOwnership(t *testing.T) {
	for _, tc := range []struct {
		outcome UpstreamOutcome
		denial  OutcomeDenial
		class   OutcomeClass
	}{
		{HTTPOutcome(400), "", OutcomeCaller},
		{HTTPOutcome(302), "", OutcomeNeutral},
		{UpstreamOutcome{Kind: OutcomeConnectNeutral}, "", OutcomeNeutral},
		{HTTPOutcome(403), DenialWorkspace, OutcomeWorkspace},
	} {
		now := time.Unix(1_700_000_000, 0)
		recorder := NewOutcomeRecorder(OutcomeRecorderDependencies{})
		recorder.Health.SetHardCooldownAt("acct", now, now.Add(time.Hour), CooldownSourceResetDerived)
		lease, ok, _ := recorder.Health.TryAcquireHardCooldownProbe("acct", now.Add(QuotaProbeInterval))
		if !ok {
			t.Fatal("probe lease not granted")
		}
		if got := recorder.Record("acct", tc.outcome, OutcomeMeta{
			Now: now.Add(QuotaProbeInterval + time.Second), Denial: tc.denial, ProbeLease: &lease, WriterGeneration: 1,
		}); got != tc.class {
			t.Fatalf("class=%q want=%q", got, tc.class)
		}
		cooldown, ok := recorder.Health.HardCooldown("acct", now.Add(QuotaProbeInterval+time.Second))
		if !ok || cooldown.ProbeLeaseID != "" || cooldown.Source != CooldownSourceResetDerived {
			t.Fatalf("cooldown=%#v ok=%v", cooldown, ok)
		}
		if recorder.Reauth.Needs("acct") {
			t.Fatal("non-credential outcome marked reauth")
		}
	}
}

func TestOutcomeRecorderCredentialAndWorkspaceRoutingState(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	credentials := outcomeTestCredentials("acct")

	credential := NewOutcomeRecorder(OutcomeRecorderDependencies{})
	credential.Health.SetHardCooldownAt("acct", now, now.Add(time.Hour), CooldownSourceRetryAfter)
	credential.Health.SetScopedHardCooldownAt("acct", QuotaScopeSpark, now, now.Add(time.Hour), CooldownSourceResetDerived)
	credential.Affinity.Bind("thread", "acct", AffinityScopeShared, now, credentials)
	credential.Record("acct", HTTPOutcome(401), OutcomeMeta{Now: now.Add(time.Second), WriterGeneration: 7})
	if !credential.Reauth.Needs("acct") {
		t.Fatal("credential failure did not mark reauth")
	}
	if _, ok := credential.Health.HardCooldown("acct", now.Add(time.Second)); ok {
		t.Fatal("credential quarantine kept account-wide cooldown")
	}
	if _, ok := credential.Health.ScopedHardCooldown("acct", QuotaScopeSpark, now.Add(time.Second)); ok {
		t.Fatal("credential quarantine kept scoped cooldown")
	}
	if got := credential.Affinity.Resolve("thread", AffinityScopeShared, now.Add(time.Second), credentials); got.Status != AffinityNone {
		t.Fatalf("credential quarantine kept affinity=%#v", got)
	}

	workspace := NewOutcomeRecorder(OutcomeRecorderDependencies{})
	workspace.Affinity.Bind("thread", "acct", AffinityScopeShared, now, credentials)
	workspace.Record("acct", HTTPOutcome(403), OutcomeMeta{Now: now.Add(time.Second), Denial: DenialWorkspace, WriterGeneration: 1})
	if workspace.Reauth.Needs("acct") {
		t.Fatal("workspace denial marked reauth")
	}
	if got := workspace.Affinity.Resolve("thread", AffinityScopeShared, now.Add(time.Second), credentials); got.Status != AffinitySelected {
		t.Fatalf("workspace denial cleared affinity=%#v", got)
	}
	failure, ok := workspace.Failures.Snapshot("acct")
	if !ok || failure.ConsecutiveFailures != 1 || failure.LastFailureStatus != 403 {
		t.Fatalf("workspace failure=%#v ok=%v", failure, ok)
	}
}

func TestOutcomeRecorderQuotaScopeAndRotationSemantics(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	credentials := outcomeTestCredentials("acct")

	spark := NewOutcomeRecorder(OutcomeRecorderDependencies{})
	spark.Affinity.Bind("thread", "acct", AffinityScopeShared, now, credentials)
	spark.Record("acct", HTTPOutcome(429), OutcomeMeta{
		Now: now, Scope: QuotaScopeSpark, ResetAt: []any{now.Add(10 * time.Minute).Unix()}, WriterGeneration: 1,
	})
	if _, ok := spark.Health.ScopedHardCooldown("acct", QuotaScopeSpark, now); !ok {
		t.Fatal("spark reset did not create scoped cooldown")
	}
	if _, ok := spark.Health.HardCooldown("acct", now); ok {
		t.Fatal("spark reset created account-wide cooldown")
	}
	if got := spark.Affinity.Resolve("thread", AffinityScopeShared, now, credentials); got.Status != AffinitySelected {
		t.Fatalf("spark reset cleared shared affinity=%#v", got)
	}

	shared := NewOutcomeRecorder(OutcomeRecorderDependencies{})
	shared.Affinity.Bind("thread", "acct", AffinityScopeShared, now, credentials)
	shared.Rotation.Seed("codex", "acct")
	shared.Record("acct", HTTPOutcome(429), OutcomeMeta{
		Now: now, Scope: QuotaScopeShared, ResetAt: []any{now.Add(10 * time.Minute).Unix()}, WriterGeneration: 1,
	})
	if _, ok := shared.Health.ScopedHardCooldown("acct", QuotaScopeShared, now); !ok {
		t.Fatal("shared reset did not create scoped cooldown")
	}
	if got := shared.Affinity.Resolve("thread", AffinityScopeShared, now, credentials); got.Status != AffinityNone {
		t.Fatalf("shared reset kept affinity=%#v", got)
	}
	if got := shared.Rotation.PickRoundRobin("codex", []string{"other", "acct"}, 3); got != "other" {
		t.Fatalf("shared reset kept failed RR sticky account=%q", got)
	}

	accountWide := NewOutcomeRecorder(OutcomeRecorderDependencies{})
	accountWide.Health.SetScopedHardCooldownAt("acct", QuotaScopeSpark, now.Add(-QuotaProbeInterval), now.Add(time.Hour), CooldownSourceResetDerived)
	lease, ok, _ := accountWide.Health.TryAcquireScopedHardCooldownProbe("acct", QuotaScopeSpark, now)
	if !ok {
		t.Fatal("scoped probe lease not granted")
	}
	accountWide.Record("acct", HTTPOutcome(429), OutcomeMeta{
		Now: now.Add(time.Second), Scope: QuotaScopeSpark, RetryAfter: "120", ProbeLease: &lease, WriterGeneration: 1,
	})
	if cooldown, ok := accountWide.Health.HardCooldown("acct", now.Add(time.Second)); !ok || cooldown.Source != CooldownSourceRetryAfter {
		t.Fatalf("account cooldown=%#v ok=%v", cooldown, ok)
	}
	if scoped, ok := accountWide.Health.ScopedHardCooldown("acct", QuotaScopeSpark, now.Add(time.Second)); !ok || scoped.ProbeLeaseID != "" {
		t.Fatalf("scoped probe not released=%#v ok=%v", scoped, ok)
	}
}

func TestOutcomeRecorderTransientEscalationFixedGuardAndProbeRelease(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	credentials := outcomeTestCredentials("acct")
	threshold := 3

	recorder := NewOutcomeRecorder(OutcomeRecorderDependencies{})
	recorder.Affinity.Bind("thread", "acct", AffinityScopeShared, now, credentials)
	for i, wantAvoid := range []time.Duration{0, 0, 30 * time.Second, 2 * time.Minute, 10 * time.Minute, 30 * time.Minute} {
		at := now.Add(time.Duration(i) * time.Second)
		recorder.Record("acct", HTTPOutcome(503), OutcomeMeta{Now: at, WriterGeneration: 1, FailoverThreshold: &threshold})
		until, avoided := recorder.Health.SoftAvoidUntil("acct", at)
		if wantAvoid == 0 && avoided {
			t.Fatalf("failure %d unexpectedly soft-avoided until %v", i+1, until)
		}
		if wantAvoid > 0 && (!avoided || until.Before(at.Add(wantAvoid))) {
			t.Fatalf("failure %d avoid=%v ok=%v", i+1, until, avoided)
		}
	}
	if got := recorder.Affinity.Resolve("thread", AffinityScopeShared, now.Add(10*time.Second), credentials); got.Status != AffinityNone {
		t.Fatalf("failover-ready transient kept affinity=%#v", got)
	}

	fixed := NewOutcomeRecorder(OutcomeRecorderDependencies{})
	fixed.Affinity.Bind("thread", "acct", AffinityScopeShared, now, credentials)
	for i := 0; i < 3; i++ {
		fixed.Record("acct", HTTPOutcome(503), OutcomeMeta{
			Now: now.Add(time.Duration(i) * time.Second), WriterGeneration: 1, FailoverThreshold: &threshold, FixedAccount: true,
		})
	}
	if got := fixed.Affinity.Resolve("thread", AffinityScopeShared, now.Add(3*time.Second), credentials); got.Status != AffinitySelected {
		t.Fatalf("fixed-account transient cleared affinity=%#v", got)
	}

	probe := NewOutcomeRecorder(OutcomeRecorderDependencies{})
	probe.Health.SetHardCooldownAt("acct", now, now.Add(time.Hour), CooldownSourceResetDerived)
	lease, ok, _ := probe.Health.TryAcquireHardCooldownProbe("acct", now.Add(QuotaProbeInterval))
	if !ok {
		t.Fatal("probe lease not granted")
	}
	probe.Record("acct", UpstreamOutcome{Kind: OutcomeTimeout}, OutcomeMeta{
		Now: now.Add(QuotaProbeInterval + time.Second), ProbeLease: &lease, WriterGeneration: 1,
	})
	cooldown, ok := probe.Health.HardCooldown("acct", now.Add(QuotaProbeInterval+time.Second))
	if !ok || cooldown.ProbeLeaseID != "" || cooldown.Source != CooldownSourceResetDerived {
		t.Fatalf("transient probe cooldown=%#v ok=%v", cooldown, ok)
	}
}

func TestOutcomeRecorderGenerationFenceRejectsDeletedButAcceptsLiveLateWriter(t *testing.T) {
	recorder := NewOutcomeRecorder(OutcomeRecorderDependencies{})
	recorder.Reconcile(10, map[string]struct{}{"live": {}})
	now := time.Unix(1_700_000_000, 0)
	recorder.Record("deleted", HTTPOutcome(429), OutcomeMeta{Now: now, RetryAfter: "60", WriterGeneration: 9})
	if _, ok := recorder.Health.HardCooldown("deleted", now); ok {
		t.Fatal("stale writer resurrected deleted account health")
	}
	recorder.Record("live", HTTPOutcome(429), OutcomeMeta{Now: now, RetryAfter: "60", WriterGeneration: 9})
	if _, ok := recorder.Health.HardCooldown("live", now); !ok {
		t.Fatal("late writer for still-live account was rejected")
	}
}

func outcomeTestCredentials(ids ...string) ManagedCredentialSnapshot {
	records := make(map[string]ManagedCredentialRecord, len(ids))
	for _, id := range ids {
		records[id] = ManagedCredentialRecord{Generation: 1, Credential: &ManagedCredential{AccessToken: "access-" + id}}
	}
	return ManagedCredentialSnapshot{Status: ManagedCredentialStoreOK, Records: records}
}
