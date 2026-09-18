package codexauth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPoolCredentialResolverOutcomeAwareAcquiresAccountProbe(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	selector := NewPoolSelector(PoolSelectorDependencies{})
	selector.Health.SetHardCooldownAt("acct", now.Add(-6*time.Minute), now.Add(10*time.Minute), CooldownSourceResetDerived)
	managed := &fakeManagedTokenGetter{token: ManagedToken{AccessToken: "selected", ChatGPTAccountID: "chat", Generation: 1}}
	resolver := newResolverHarness(t, selector, managed, managedCredentialSnapshotForTest(1, "acct"), MainCredentialResult{Status: MainCredentialMissing})

	credential, err := resolver.Resolve(context.Background(), PoolCredentialRequest{
		Selection: managedPoolSelectionForTest(now, 1, "acct"),
		WriterGeneration: 7,
		OutcomeAware: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if credential.ProbeLease == nil || credential.ProbeLease.AccountID != "acct" || credential.ProbeLease.Scope != "" {
		t.Fatalf("credential=%#v", credential)
	}
	cooldown, ok := selector.Health.HardCooldown("acct", now)
	if !ok || cooldown.ProbeLeaseID != credential.ProbeLease.LeaseID || cooldown.ProbeLeaseGeneration != cooldown.Generation {
		t.Fatalf("cooldown=%#v ok=%v lease=%#v", cooldown, ok, credential.ProbeLease)
	}
}

func TestPoolCredentialResolverOutcomeAwareStillHonorsRetryAfter(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	selector := NewPoolSelector(PoolSelectorDependencies{})
	selector.Health.SetHardCooldownAt("acct", now.Add(-10*time.Minute), now.Add(time.Hour), CooldownSourceRetryAfter)
	managed := &fakeManagedTokenGetter{token: ManagedToken{AccessToken: "must-not-run", Generation: 1}}
	resolver := newResolverHarness(t, selector, managed, managedCredentialSnapshotForTest(1, "acct"), MainCredentialResult{Status: MainCredentialMissing})

	_, err := resolver.Resolve(context.Background(), PoolCredentialRequest{
		Selection: managedPoolSelectionForTest(now, 1, "acct"),
		OutcomeAware: true,
	})
	var cooldownErr *PoolCooldownError
	if !errors.As(err, &cooldownErr) || cooldownErr.Source != CooldownSourceRetryAfter {
		t.Fatalf("err=%v", err)
	}
	if len(managed.calls) != 0 {
		t.Fatalf("managed calls=%v", managed.calls)
	}
	cooldown, _ := selector.Health.HardCooldown("acct", now)
	if cooldown.ProbeLeaseID != "" {
		t.Fatalf("Retry-After acquired lease: %#v", cooldown)
	}
}

func TestPoolCredentialResolverReleasesProbeWhenMaterializationFailsBeforeNetwork(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	selector := NewPoolSelector(PoolSelectorDependencies{})
	selector.Health.SetHardCooldownAt("acct", now.Add(-6*time.Minute), now.Add(10*time.Minute), CooldownSourceResetDerived)
	managed := &fakeManagedTokenGetter{err: ErrManagedCredentialRefreshBusy}
	resolver := newResolverHarness(t, selector, managed, managedCredentialSnapshotForTest(1, "acct"), MainCredentialResult{Status: MainCredentialMissing})

	_, err := resolver.Resolve(context.Background(), PoolCredentialRequest{
		Selection: managedPoolSelectionForTest(now, 1, "acct"),
		OutcomeAware: true,
	})
	if !errors.Is(err, ErrManagedCredentialRefreshBusy) {
		t.Fatalf("err=%v", err)
	}
	cooldown, ok := selector.Health.HardCooldown("acct", now)
	if !ok || cooldown.ProbeLeaseID != "" || !cooldown.LastProbeAt.Equal(now) {
		t.Fatalf("cooldown=%#v ok=%v", cooldown, ok)
	}
}

func TestPoolCredentialResolverAccountCooldownWinsOverScopedProbe(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	selector := NewPoolSelector(PoolSelectorDependencies{})
	selector.Health.SetHardCooldownAt("acct", now.Add(-6*time.Minute), now.Add(10*time.Minute), CooldownSourceResetDerived)
	selector.Health.SetScopedHardCooldownAt("acct", QuotaScopeSpark, now.Add(-6*time.Minute), now.Add(10*time.Minute), CooldownSourceResetDerived)
	managed := &fakeManagedTokenGetter{token: ManagedToken{AccessToken: "selected", Generation: 1}}
	resolver := newResolverHarness(t, selector, managed, managedCredentialSnapshotForTest(1, "acct"), MainCredentialResult{Status: MainCredentialMissing})
	selection := managedPoolSelectionForTest(now, 1, "acct")
	selection.Scope = QuotaScopeSpark

	credential, err := resolver.Resolve(context.Background(), PoolCredentialRequest{Selection: selection, OutcomeAware: true})
	if err != nil {
		t.Fatal(err)
	}
	if credential.ProbeLease == nil || credential.ProbeLease.Scope != "" {
		t.Fatalf("lease=%#v", credential.ProbeLease)
	}
	scoped, _ := selector.Health.ScopedHardCooldown("acct", QuotaScopeSpark, now)
	if scoped.ProbeLeaseID != "" {
		t.Fatalf("scoped cooldown was probed while account cooldown existed: %#v", scoped)
	}
}

func TestPoolCredentialResolverRejectsUnsafeTokenAndReleasesProbe(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	selector := NewPoolSelector(PoolSelectorDependencies{})
	selector.Health.SetHardCooldownAt("acct", now.Add(-6*time.Minute), now.Add(10*time.Minute), CooldownSourceResetDerived)
	managed := &fakeManagedTokenGetter{token: ManagedToken{AccessToken: "bad token", Generation: 1}}
	resolver := newResolverHarness(t, selector, managed, managedCredentialSnapshotForTest(1, "acct"), MainCredentialResult{Status: MainCredentialMissing})

	_, err := resolver.Resolve(context.Background(), PoolCredentialRequest{
		Selection: managedPoolSelectionForTest(now, 1, "acct"),
		WriterGeneration: 9,
		OutcomeAware: true,
	})
	if !errors.Is(err, ErrPoolSelectedCredentialUnavailable) {
		t.Fatalf("err=%v", err)
	}
	if !selector.Reauth.Needs("acct") {
		t.Fatal("unsafe managed credential did not mark reauth")
	}
	cooldown, _ := selector.Health.HardCooldown("acct", now)
	if cooldown.ProbeLeaseID != "" {
		t.Fatalf("unsafe credential leaked probe lease: %#v", cooldown)
	}
}
