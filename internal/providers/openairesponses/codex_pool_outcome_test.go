package openairesponses

import (
	"context"
	"testing"

	"github.com/Wibias/Benes/internal/codexauth"
	"github.com/Wibias/Benes/internal/providers"
)

type fakePoolOutcomeRecorder struct {
	calls []poolOutcomeCall
}

type poolOutcomeCall struct {
	accountID string
	outcome   codexauth.UpstreamOutcome
	meta      codexauth.OutcomeMeta
}

func (f *fakePoolOutcomeRecorder) Record(accountID string, outcome codexauth.UpstreamOutcome, meta codexauth.OutcomeMeta) codexauth.OutcomeClass {
	f.calls = append(f.calls, poolOutcomeCall{accountID: accountID, outcome: outcome, meta: meta})
	return codexauth.ClassifyUpstreamOutcome(outcome, meta.Denial)
}

func TestCodexPoolAuthorityResolveAttemptEnablesOutcomeAwareResolution(t *testing.T) {
	snapshot := &fakePoolSnapshotSource{request: codexauth.PoolCredentialRequest{}}
	lease := &codexauth.ProbeLease{AccountID: "acct", Scope: codexauth.QuotaScopeSpark, LeaseID: "lease", CooldownGeneration: 4}
	threshold := 5
	resolver := &fakePoolResolver{credential: codexauth.PoolCredential{
		AccountID: "acct", AccessToken: "selected", ChatGPTAccountID: "chat",
		Generation: 3, WriterGeneration: 8, Scope: codexauth.QuotaScopeSpark,
		ProbeLease: lease, FailoverThreshold: &threshold,
	}}
	outcomes := &fakePoolOutcomeRecorder{}
	authority, err := NewCodexPoolAuthorityWithOutcomes(snapshot, resolver, outcomes)
	if err != nil {
		t.Fatal(err)
	}

	attempt, err := authority.ResolveAttempt(context.Background(), providers.DispatchRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(resolver.seen) != 1 || !resolver.seen[0].OutcomeAware {
		t.Fatalf("resolver request=%#v", resolver.seen)
	}
	if attempt.Credential.Authorization != "Bearer selected" || attempt.Observer == nil {
		t.Fatalf("attempt=%#v", attempt)
	}

	attempt.Observer.Observe(ForwardOutcome{Kind: ForwardOutcomeHTTP, StatusCode: 429, RetryAfter: "120"})
	if len(outcomes.calls) != 1 {
		t.Fatalf("calls=%#v", outcomes.calls)
	}
	call := outcomes.calls[0]
	if call.accountID != "acct" || call.outcome.Kind != codexauth.OutcomeHTTP || call.outcome.StatusCode != 429 || call.meta.RetryAfter != "120" || call.meta.Scope != codexauth.QuotaScopeSpark || call.meta.ProbeLease != lease || call.meta.WriterGeneration != 8 || call.meta.FailoverThreshold == nil || *call.meta.FailoverThreshold != 5 {
		t.Fatalf("call=%#v", call)
	}
}

func TestCodexPoolAttemptObserverMapsTerminalAndTransportEvidence(t *testing.T) {
	for _, tc := range []struct {
		name string
		forward ForwardOutcome
		wantKind codexauth.OutcomeKind
		wantStatus int
	}{
		{name: "completed", forward: ForwardOutcome{Kind: ForwardOutcomeCompleted}, wantKind: codexauth.OutcomeHTTP, wantStatus: 200},
		{name: "failed", forward: ForwardOutcome{Kind: ForwardOutcomeFailed}, wantKind: codexauth.OutcomeHTTP, wantStatus: 502},
		{name: "incomplete", forward: ForwardOutcome{Kind: ForwardOutcomeIncomplete}, wantKind: codexauth.OutcomeHTTP, wantStatus: 502},
		{name: "connect", forward: ForwardOutcome{Kind: ForwardOutcomeTransportError}, wantKind: codexauth.OutcomeConnectError},
		{name: "timeout", forward: ForwardOutcome{Kind: ForwardOutcomeTransportError, TimedOut: true}, wantKind: codexauth.OutcomeTimeout},
	} {
		t.Run(tc.name, func(t *testing.T) {
			outcomes := &fakePoolOutcomeRecorder{}
			observer := newCodexPoolAttemptObserver(outcomes, codexauth.PoolCredential{AccountID: "acct", WriterGeneration: 2})
			observer.Observe(tc.forward)
			if len(outcomes.calls) != 1 || outcomes.calls[0].outcome.Kind != tc.wantKind || outcomes.calls[0].outcome.StatusCode != tc.wantStatus {
				t.Fatalf("calls=%#v", outcomes.calls)
			}
		})
	}
}

func TestCodexPoolAuthorityWithoutOutcomeRecorderKeepsCooldownFailClosedPath(t *testing.T) {
	snapshot := &fakePoolSnapshotSource{}
	resolver := &fakePoolResolver{err: codexauth.ErrPoolOutcomeAwareTransportRequired}
	authority, err := NewCodexPoolAuthority(snapshot, resolver)
	if err != nil {
		t.Fatal(err)
	}
	_, err = authority.ResolveAttempt(context.Background(), providers.DispatchRequest{})
	if err == nil {
		t.Fatal("expected fail-closed error")
	}
	if len(resolver.seen) != 1 || resolver.seen[0].OutcomeAware {
		t.Fatalf("resolver request=%#v", resolver.seen)
	}
}
