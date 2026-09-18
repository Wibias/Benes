package codexauth

import (
	"sync"
	"testing"
	"time"
)

func TestRetryAfterCooldownIsNeverProbeable(t *testing.T) {
	state := NewHealthState()
	now := time.Unix(1_700_000_000, 0)
	state.SetHardCooldownAt("acct", now, now.Add(time.Hour), CooldownSourceRetryAfter)

	if lease, ok, err := state.TryAcquireHardCooldownProbe("acct", now.Add(10*time.Minute)); err != nil || ok || lease.LeaseID != "" {
		t.Fatalf("lease=%#v ok=%v err=%v", lease, ok, err)
	}
}

func TestResetDerivedProbeWaitsForIntervalAndOnlyOneLeaseExists(t *testing.T) {
	state := NewHealthState()
	now := time.Unix(1_700_000_000, 0)
	state.SetHardCooldownAt("acct", now, now.Add(time.Hour), CooldownSourceResetDerived)

	if _, ok, err := state.TryAcquireHardCooldownProbe("acct", now.Add(QuotaProbeInterval-time.Nanosecond)); err != nil || ok {
		t.Fatalf("early probe ok=%v err=%v", ok, err)
	}
	lease, ok, err := state.TryAcquireHardCooldownProbe("acct", now.Add(QuotaProbeInterval))
	if err != nil || !ok || lease.LeaseID == "" || lease.CooldownGeneration != 1 {
		t.Fatalf("lease=%#v ok=%v err=%v", lease, ok, err)
	}
	if second, ok, err := state.TryAcquireHardCooldownProbe("acct", now.Add(QuotaProbeInterval+time.Second)); err != nil || ok || second.LeaseID != "" {
		t.Fatalf("second=%#v ok=%v err=%v", second, ok, err)
	}
}

func TestProbeReleaseIsOwnerOnlyAndRestartsInterval(t *testing.T) {
	state := NewHealthState()
	now := time.Unix(1_700_000_000, 0)
	state.SetHardCooldownAt("acct", now, now.Add(time.Hour), CooldownSourceDefault)
	lease, ok, err := state.TryAcquireHardCooldownProbe("acct", now.Add(QuotaProbeInterval))
	if err != nil || !ok {
		t.Fatalf("lease=%#v ok=%v err=%v", lease, ok, err)
	}
	wrong := lease
	wrong.LeaseID = "wrong"
	if state.ReleaseProbe(wrong, now.Add(QuotaProbeInterval+time.Second)) {
		t.Fatal("wrong owner released probe")
	}
	releasedAt := now.Add(QuotaProbeInterval + 2*time.Second)
	if !state.ReleaseProbe(lease, releasedAt) {
		t.Fatal("owner did not release probe")
	}
	if _, ok, err := state.TryAcquireHardCooldownProbe("acct", releasedAt.Add(QuotaProbeInterval-time.Nanosecond)); err != nil || ok {
		t.Fatalf("probe granted before restarted interval ok=%v err=%v", ok, err)
	}
	if next, ok, err := state.TryAcquireHardCooldownProbe("acct", releasedAt.Add(QuotaProbeInterval)); err != nil || !ok || next.LeaseID == lease.LeaseID {
		t.Fatalf("next=%#v ok=%v err=%v", next, ok, err)
	}
}

func TestStaleGenerationProbeCannotClearNewerCooldownButReleasesLease(t *testing.T) {
	state := NewHealthState()
	now := time.Unix(1_700_000_000, 0)
	state.SetHardCooldownAt("acct", now, now.Add(time.Hour), CooldownSourceResetDerived)
	lease, ok, err := state.TryAcquireHardCooldownProbe("acct", now.Add(QuotaProbeInterval))
	if err != nil || !ok {
		t.Fatalf("lease=%#v ok=%v err=%v", lease, ok, err)
	}

	renewedAt := now.Add(QuotaProbeInterval + time.Second)
	state.SetHardCooldownAt("acct", renewedAt, renewedAt.Add(2*time.Hour), CooldownSourceRetryAfter)
	if cleared := state.CompleteProbeSuccess(lease, renewedAt.Add(time.Second)); cleared {
		t.Fatal("stale probe cleared newer cooldown")
	}
	cooldown, ok := state.HardCooldown("acct", renewedAt.Add(time.Second))
	if !ok || cooldown.Generation != 2 || cooldown.Source != CooldownSourceRetryAfter || cooldown.ProbeLeaseID != "" {
		t.Fatalf("cooldown=%#v ok=%v", cooldown, ok)
	}
	if _, ok, err := state.TryAcquireHardCooldownProbe("acct", renewedAt.Add(time.Hour)); err != nil || ok {
		t.Fatalf("retry-after cooldown became probeable ok=%v err=%v", ok, err)
	}
}

func TestCurrentGenerationProbeSuccessClearsCooldown(t *testing.T) {
	state := NewHealthState()
	now := time.Unix(1_700_000_000, 0)
	state.SetHardCooldownAt("acct", now, now.Add(time.Hour), CooldownSourceResetDerived)
	lease, ok, err := state.TryAcquireHardCooldownProbe("acct", now.Add(QuotaProbeInterval))
	if err != nil || !ok {
		t.Fatalf("lease=%#v ok=%v err=%v", lease, ok, err)
	}
	if !state.CompleteProbeSuccess(lease, now.Add(QuotaProbeInterval+time.Second)) {
		t.Fatal("current probe did not clear cooldown")
	}
	if _, ok := state.HardCooldown("acct", now.Add(QuotaProbeInterval+time.Second)); ok {
		t.Fatal("cooldown survived current probe success")
	}
}

func TestScopedProbeLeaseIsIndependentFromAccountWideCooldown(t *testing.T) {
	state := NewHealthState()
	now := time.Unix(1_700_000_000, 0)
	state.SetHardCooldownAt("acct", now, now.Add(time.Hour), CooldownSourceRetryAfter)
	state.SetScopedHardCooldownAt("acct", QuotaScopeSpark, now, now.Add(time.Hour), CooldownSourceResetDerived)

	lease, ok, err := state.TryAcquireScopedHardCooldownProbe("acct", QuotaScopeSpark, now.Add(QuotaProbeInterval))
	if err != nil || !ok || lease.Scope != QuotaScopeSpark {
		t.Fatalf("lease=%#v ok=%v err=%v", lease, ok, err)
	}
	if !state.CompleteProbeSuccess(lease, now.Add(QuotaProbeInterval+time.Second)) {
		t.Fatal("scoped probe did not clear scoped cooldown")
	}
	if _, ok := state.ScopedHardCooldown("acct", QuotaScopeSpark, now.Add(QuotaProbeInterval+time.Second)); ok {
		t.Fatal("scoped cooldown survived")
	}
	if cooldown, ok := state.HardCooldown("acct", now.Add(QuotaProbeInterval+time.Second)); !ok || cooldown.Source != CooldownSourceRetryAfter {
		t.Fatalf("account-wide cooldown changed: %#v ok=%v", cooldown, ok)
	}
}

func TestConcurrentProbeAcquisitionGrantsExactlyOneLease(t *testing.T) {
	state := NewHealthState()
	now := time.Unix(1_700_000_000, 0)
	state.SetHardCooldownAt("acct", now, now.Add(time.Hour), CooldownSourceResetDerived)
	probeAt := now.Add(QuotaProbeInterval)

	var wg sync.WaitGroup
	results := make(chan ProbeLease, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			lease, ok, err := state.TryAcquireHardCooldownProbe("acct", probeAt)
			if err != nil {
				t.Errorf("TryAcquireHardCooldownProbe(): %v", err)
				return
			}
			if ok {
				results <- lease
			}
		}()
	}
	wg.Wait()
	close(results)
	var leases []ProbeLease
	for lease := range results {
		leases = append(leases, lease)
	}
	if len(leases) != 1 || leases[0].LeaseID == "" {
		t.Fatalf("leases=%#v", leases)
	}
}
