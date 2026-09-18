package openairesponses

import (
	"net/http"
	"testing"

	"github.com/Wibias/Benes/internal/codexauth"
)

func TestCodexWorkspace403DoesNotQuarantineValidCredential(t *testing.T) {
	reauth := codexauth.NewReauthState()
	failures := codexauth.NewFailureState()
	recorder := codexauth.NewOutcomeRecorder(codexauth.OutcomeRecorderDependencies{
		Reauth:   reauth,
		Failures: failures,
	})
	observer := newCodexPoolAttemptObserver(recorder, codexauth.PoolCredential{
		AccountID:        "acct",
		WriterGeneration: 4,
	})

	observer.Observe(ForwardOutcome{
		Kind:       ForwardOutcomeHTTP,
		StatusCode: http.StatusForbidden,
		Denial:     ForwardDenialWorkspace,
	})
	if reauth.Needs("acct") {
		t.Fatal("workspace denial quarantined a valid credential")
	}
	failure, ok := failures.Snapshot("acct")
	if !ok || failure.ConsecutiveFailures != 1 || failure.LastFailureStatus != http.StatusForbidden {
		t.Fatalf("failure=%#v ok=%v", failure, ok)
	}
}

func TestUnverified403StillQuarantinesCredential(t *testing.T) {
	reauth := codexauth.NewReauthState()
	recorder := codexauth.NewOutcomeRecorder(codexauth.OutcomeRecorderDependencies{Reauth: reauth})
	observer := newCodexPoolAttemptObserver(recorder, codexauth.PoolCredential{
		AccountID:        "acct",
		WriterGeneration: 4,
	})

	observer.Observe(ForwardOutcome{Kind: ForwardOutcomeHTTP, StatusCode: http.StatusForbidden})
	if !reauth.Needs("acct") {
		t.Fatal("unverified 403 did not quarantine credential")
	}
}
