package openairesponses

import (
	"context"
	"testing"

	"github.com/Wibias/Benes/internal/codexauth"
	"github.com/Wibias/Benes/internal/providers"
)

type fakePoolQuotaRecorder struct {
	credential codexauth.PoolCredential
	headers    codexauth.UpstreamQuotaHeaders
	calls      int
}

func (f *fakePoolQuotaRecorder) Record(credential codexauth.PoolCredential, headers codexauth.UpstreamQuotaHeaders) bool {
	f.credential = credential
	f.headers = headers
	f.calls++
	return true
}

func TestCodexPoolAttemptObserverPublishesAuditedQuotaHeadersWithSelectedCredential(t *testing.T) {
	snapshot := &fakePoolSnapshotSource{request: codexauth.PoolCredentialRequest{}}
	credential := codexauth.PoolCredential{
		AccountID: "acct", AccessToken: "selected", ChatGPTAccountID: "chat",
		Generation: 3, WriterGeneration: 8,
	}
	resolver := &fakePoolResolver{credential: credential}
	outcomes := &fakePoolOutcomeRecorder{}
	quotas := &fakePoolQuotaRecorder{}
	authority, err := NewCodexPoolAuthorityWithOutcomesAndQuota(snapshot, resolver, outcomes, quotas)
	if err != nil { t.Fatal(err) }
	attempt, err := authority.ResolveAttempt(context.Background(), providers.DispatchRequest{})
	if err != nil { t.Fatal(err) }
	quotaObserver, ok := attempt.Observer.(ForwardQuotaObserver)
	if !ok { t.Fatalf("observer=%T", attempt.Observer) }
	quotaObserver.ObserveQuota(ForwardQuotaHeaders{
		PrimaryUsedPercent: "45", SecondaryUsedPercent: "12", TertiaryUsedPercent: "3",
		PrimaryResetAt: "1700000100", SecondaryResetAt: "1700000200", TertiaryResetAt: "1700000300",
		PrimaryWindowMinutes: "10080", SecondaryWindowMinutes: "40320",
	})
	if quotas.calls != 1 || quotas.credential.AccountID != "acct" || quotas.credential.Generation != 3 || quotas.credential.WriterGeneration != 8 {
		t.Fatalf("quota recorder=%#v", quotas)
	}
	if quotas.headers.PrimaryUsedPercent != "45" || quotas.headers.SecondaryWindowMinutes != "40320" {
		t.Fatalf("headers=%#v", quotas.headers)
	}
	if len(outcomes.calls) != 0 { t.Fatalf("quota observation mutated health outcomes: %#v", outcomes.calls) }
}

func TestCodexPoolAuthorityQuotaConstructorRequiresQuotaRecorder(t *testing.T) {
	if _, err := NewCodexPoolAuthorityWithOutcomesAndQuota(&fakePoolSnapshotSource{}, &fakePoolResolver{}, &fakePoolOutcomeRecorder{}, nil); err == nil {
		t.Fatal("expected missing quota recorder error")
	}
}
