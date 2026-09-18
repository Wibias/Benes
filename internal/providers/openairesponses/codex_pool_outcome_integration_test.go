package openairesponses

import (
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/codexauth"
)

func TestCodexPoolAttemptObserverCompletedClearsOwnedProbeCooldown(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	health := codexauth.NewHealthState()
	health.SetHardCooldownAt("acct", now.Add(-6*time.Minute), now.Add(10*time.Minute), codexauth.CooldownSourceResetDerived)
	lease, ok, err := health.TryAcquireHardCooldownProbe("acct", now)
	if err != nil || !ok {
		t.Fatalf("lease=%#v ok=%v err=%v", lease, ok, err)
	}
	recorder := codexauth.NewOutcomeRecorder(codexauth.OutcomeRecorderDependencies{Health: health})
	observer := newCodexPoolAttemptObserver(recorder, codexauth.PoolCredential{
		AccountID: "acct", WriterGeneration: 1, ProbeLease: &lease,
	})

	observer.Observe(ForwardOutcome{Kind: ForwardOutcomeCompleted})
	if _, exists := health.HardCooldown("acct", now); exists {
		t.Fatal("completed probe did not clear cooldown")
	}
}

func TestCodexPoolAttemptObserverAbandonKeepsCooldownAndReleasesLease(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	health := codexauth.NewHealthState()
	health.SetHardCooldownAt("acct", now.Add(-6*time.Minute), now.Add(10*time.Minute), codexauth.CooldownSourceResetDerived)
	lease, ok, err := health.TryAcquireHardCooldownProbe("acct", now)
	if err != nil || !ok {
		t.Fatalf("lease=%#v ok=%v err=%v", lease, ok, err)
	}
	recorder := codexauth.NewOutcomeRecorder(codexauth.OutcomeRecorderDependencies{Health: health})
	observer := newCodexPoolAttemptObserver(recorder, codexauth.PoolCredential{
		AccountID: "acct", WriterGeneration: 1, ProbeLease: &lease,
	})

	observer.Abandon()
	cooldown, exists := health.HardCooldown("acct", now)
	if !exists || cooldown.ProbeLeaseID != "" {
		t.Fatalf("cooldown=%#v exists=%v", cooldown, exists)
	}
}
