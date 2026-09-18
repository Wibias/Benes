package codexauth

import (
	"sync"
	"testing"
	"time"
)

func TestHealthStateAccountWideCooldownAndSoftAvoid(t *testing.T) {
	state := NewHealthState()
	now := time.Unix(1_700_000_000, 0)
	cooldownUntil := now.Add(5 * time.Minute)
	softUntil := now.Add(30 * time.Second)

	state.SetHardCooldown("acct", cooldownUntil, CooldownSourceRetryAfter)
	state.SetSoftAvoid("acct", softUntil)

	cooldown, ok := state.HardCooldown("acct", now)
	if !ok || !cooldown.Until.Equal(cooldownUntil) || cooldown.Source != CooldownSourceRetryAfter {
		t.Fatalf("cooldown=%#v ok=%v", cooldown, ok)
	}
	if until, ok := state.SoftAvoidUntil("acct", now); !ok || !until.Equal(softUntil) {
		t.Fatalf("soft=%v ok=%v", until, ok)
	}

	if _, ok := state.HardCooldown("acct", cooldownUntil); ok {
		t.Fatal("cooldown remained active at deadline")
	}
	if _, ok := state.SoftAvoidUntil("acct", softUntil); ok {
		t.Fatal("soft avoid remained active at deadline")
	}
}

func TestHealthStateScopedCooldownIsIndependentFromAccountWideState(t *testing.T) {
	state := NewHealthState()
	now := time.Unix(1_700_000_000, 0)
	state.SetHardCooldown("acct", now.Add(time.Minute), CooldownSourceDefault)
	state.SetScopedHardCooldown("acct", QuotaScopeSpark, now.Add(2*time.Minute), CooldownSourceResetDerived)

	shared, ok := state.HardCooldown("acct", now)
	if !ok || shared.Source != CooldownSourceDefault {
		t.Fatalf("shared=%#v ok=%v", shared, ok)
	}
	spark, ok := state.ScopedHardCooldown("acct", QuotaScopeSpark, now)
	if !ok || spark.Source != CooldownSourceResetDerived {
		t.Fatalf("spark=%#v ok=%v", spark, ok)
	}
	if _, ok := state.ScopedHardCooldown("acct", QuotaScopeShared, now); ok {
		t.Fatal("spark cooldown leaked into shared quota scope")
	}

	state.ClearHardCooldown("acct")
	if _, ok := state.HardCooldown("acct", now); ok {
		t.Fatal("account-wide cooldown survived clear")
	}
	if _, ok := state.ScopedHardCooldown("acct", QuotaScopeSpark, now); !ok {
		t.Fatal("account-wide clear removed scoped cooldown")
	}
	state.ClearScopedHardCooldown("acct", QuotaScopeSpark)
	if _, ok := state.ScopedHardCooldown("acct", QuotaScopeSpark, now); ok {
		t.Fatal("scoped cooldown survived clear")
	}
}

func TestHealthStateSoftAvoidCanBeClearedWithoutBypassingHardCooldown(t *testing.T) {
	state := NewHealthState()
	now := time.Unix(1_700_000_000, 0)
	state.SetHardCooldown("acct", now.Add(time.Minute), CooldownSourceRetryAfter)
	state.SetSoftAvoid("acct", now.Add(time.Minute))
	state.ClearSoftAvoid("acct")
	if _, ok := state.SoftAvoidUntil("acct", now); ok {
		t.Fatal("soft avoid survived clear")
	}
	if _, ok := state.HardCooldown("acct", now); !ok {
		t.Fatal("soft clear bypassed hard cooldown")
	}
}

func TestHealthStateReconcileRemovesDeletedAccountsAcrossScopes(t *testing.T) {
	state := NewHealthState()
	now := time.Unix(1_700_000_000, 0)
	state.SetHardCooldown("deleted", now.Add(time.Minute), CooldownSourceDefault)
	state.SetSoftAvoid("deleted", now.Add(time.Minute))
	state.SetScopedHardCooldown("deleted", QuotaScopeSpark, now.Add(time.Minute), CooldownSourceResetDerived)
	state.SetHardCooldown("live", now.Add(time.Minute), CooldownSourceDefault)

	removed := state.Reconcile(2, map[string]struct{}{"live": {}})
	if removed != 2 {
		t.Fatalf("removed=%d", removed)
	}
	if _, ok := state.HardCooldown("deleted", now); ok {
		t.Fatal("deleted account-wide health survived")
	}
	if _, ok := state.ScopedHardCooldown("deleted", QuotaScopeSpark, now); ok {
		t.Fatal("deleted scoped health survived")
	}
	if _, ok := state.HardCooldown("live", now); !ok {
		t.Fatal("live health removed")
	}
	if removed := state.Reconcile(1, map[string]struct{}{}); removed != 0 {
		t.Fatalf("older reconcile removed=%d", removed)
	}
}

func TestHealthStateClearAccountRemovesEveryFence(t *testing.T) {
	state := NewHealthState()
	now := time.Unix(1_700_000_000, 0)
	state.SetHardCooldown("acct", now.Add(time.Minute), CooldownSourceDefault)
	state.SetSoftAvoid("acct", now.Add(time.Minute))
	state.SetScopedHardCooldown("acct", QuotaScopeSpark, now.Add(time.Minute), CooldownSourceResetDerived)
	state.ClearAccount("acct")
	if _, ok := state.HardCooldown("acct", now); ok {
		t.Fatal("hard cooldown survived ClearAccount")
	}
	if _, ok := state.SoftAvoidUntil("acct", now); ok {
		t.Fatal("soft avoid survived ClearAccount")
	}
	if _, ok := state.ScopedHardCooldown("acct", QuotaScopeSpark, now); ok {
		t.Fatal("scoped cooldown survived ClearAccount")
	}
}

func TestHealthStateConcurrentAccessIsRaceSafe(t *testing.T) {
	state := NewHealthState()
	now := time.Unix(1_700_000_000, 0)
	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < 300; i++ {
				id := "a"
				if (worker+i)%2 == 0 {
					id = "b"
				}
				state.SetHardCooldown(id, now.Add(time.Duration(i+1)*time.Second), CooldownSourceDefault)
				state.SetSoftAvoid(id, now.Add(time.Second))
				state.SetScopedHardCooldown(id, QuotaScopeSpark, now.Add(time.Minute), CooldownSourceResetDerived)
				_, _ = state.HardCooldown(id, now)
				_, _ = state.SoftAvoidUntil(id, now)
				_, _ = state.ScopedHardCooldown(id, QuotaScopeSpark, now)
				if i%19 == 0 {
					state.ClearSoftAvoid(id)
				}
			}
		}(worker)
	}
	wg.Wait()
}
