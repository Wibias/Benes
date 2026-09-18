package openairesponses

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/codexauth"
	"github.com/Wibias/Benes/internal/providers"
)

type quotaRetryPromotion struct {
	input     codexauth.PoolSelectionInput
	rejected  string
	alternate string
}

type quotaRetryPoolResolver struct {
	credentials []codexauth.PoolCredential
	errors      []error
	seen        []codexauth.PoolCredentialRequest
	promotions  []quotaRetryPromotion
}

func (r *quotaRetryPoolResolver) Resolve(_ context.Context, request codexauth.PoolCredentialRequest) (codexauth.PoolCredential, error) {
	r.seen = append(r.seen, request)
	index := len(r.seen) - 1
	if index < len(r.errors) && r.errors[index] != nil {
		return codexauth.PoolCredential{}, r.errors[index]
	}
	if index >= len(r.credentials) {
		return codexauth.PoolCredential{}, codexauth.ErrPoolNoUsableAccount
	}
	return r.credentials[index], nil
}

func (r *quotaRetryPoolResolver) PromoteQuotaAlternate(input codexauth.PoolSelectionInput, rejected, alternate string) bool {
	r.promotions = append(r.promotions, quotaRetryPromotion{input: input, rejected: rejected, alternate: alternate})
	return true
}

type quotaRetryOutcomeRecorder struct{}

func (quotaRetryOutcomeRecorder) Record(_ string, outcome codexauth.UpstreamOutcome, meta codexauth.OutcomeMeta) codexauth.OutcomeClass {
	return codexauth.ClassifyUpstreamOutcome(outcome, meta.Denial)
}

func TestCodexPoolAuthorityQuotaRetryExcludesSelectedAccountAndIsOneShot(t *testing.T) {
	snapshot := &fakePoolSnapshotSource{request: codexauth.PoolCredentialRequest{Selection: codexauth.PoolSelectionInput{
		ThreadID: "snapshot-thread",
		Now:      time.Unix(1_700_000_000, 0),
	}}}
	resolver := &quotaRetryPoolResolver{credentials: []codexauth.PoolCredential{
		{AccountID: "first", AccessToken: "first-token", ChatGPTAccountID: "first-chat", Generation: 1, WriterGeneration: 7},
		{AccountID: "second", AccessToken: "second-token", ChatGPTAccountID: "second-chat", Generation: 1, WriterGeneration: 7},
	}}
	authority, err := NewCodexPoolAuthorityWithOutcomes(snapshot, resolver, quotaRetryOutcomeRecorder{})
	if err != nil {
		t.Fatal(err)
	}
	dispatch := providers.DispatchRequest{ForwardHeaders: providers.NewForwardHeaders(map[string]string{
		"x-codex-parent-thread-id": "live-thread",
	})}

	first, err := authority.ResolveAttempt(context.Background(), dispatch)
	if err != nil {
		t.Fatal(err)
	}
	if first.RetryQuota == nil {
		t.Fatal("Pool attempt did not expose quota retry resolver")
	}
	first.Observer.Observe(ForwardOutcome{Kind: ForwardOutcomeHTTP, StatusCode: 429})
	second, ok, err := first.RetryQuota(context.Background())
	if err != nil || !ok {
		t.Fatalf("retry ok=%v err=%v", ok, err)
	}
	if second.Credential.Authorization != "Bearer second-token" || second.Credential.ChatGPTAccountID != "second-chat" {
		t.Fatalf("second credential=%#v", second.Credential)
	}
	if second.RetryQuota != nil {
		t.Fatal("alternate Pool attempt must not expose a second quota retry")
	}
	if second.CommitQuotaRetry == nil {
		t.Fatal("alternate Pool attempt did not carry deferred quota promotion")
	}
	if len(resolver.seen) != 2 {
		t.Fatalf("resolver calls=%d", len(resolver.seen))
	}
	if resolver.seen[1].Selection.ExcludeAccountID != "first" || resolver.seen[1].Selection.ThreadID != "live-thread" || !resolver.seen[1].OutcomeAware || !resolver.seen[1].Alternate {
		t.Fatalf("alternate request=%#v", resolver.seen[1])
	}
	if len(resolver.promotions) != 0 {
		t.Fatalf("quota promotion ran during alternate resolution: %#v", resolver.promotions)
	}
	second.CommitQuotaRetry()
	if len(resolver.promotions) != 1 || resolver.promotions[0].rejected != "first" || resolver.promotions[0].alternate != "second" || resolver.promotions[0].input.ThreadID != "live-thread" {
		t.Fatalf("quota promotions=%#v", resolver.promotions)
	}
}

func TestCodexPoolAuthorityQuotaRetryTreatsUnavailableAlternateAsNoRetry(t *testing.T) {
	snapshot := &fakePoolSnapshotSource{request: codexauth.PoolCredentialRequest{Selection: codexauth.PoolSelectionInput{}}}
	resolver := &quotaRetryPoolResolver{
		credentials: []codexauth.PoolCredential{{AccountID: "first", AccessToken: "first-token"}},
		errors:      []error{nil, codexauth.ErrPoolNoUsableAccount},
	}
	authority, err := NewCodexPoolAuthorityWithOutcomes(snapshot, resolver, quotaRetryOutcomeRecorder{})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := authority.ResolveAttempt(context.Background(), providers.DispatchRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if attempt.RetryQuota == nil {
		t.Fatal("Pool attempt did not expose quota retry resolver")
	}
	_, ok, retryErr := attempt.RetryQuota(context.Background())
	if retryErr != nil || ok {
		t.Fatalf("retry ok=%v err=%v", ok, retryErr)
	}
}

func TestCodexPoolAuthorityQuotaRetryPropagatesUnexpectedResolverFailure(t *testing.T) {
	unexpected := errors.New("unexpected resolver failure")
	snapshot := &fakePoolSnapshotSource{request: codexauth.PoolCredentialRequest{Selection: codexauth.PoolSelectionInput{}}}
	resolver := &quotaRetryPoolResolver{
		credentials: []codexauth.PoolCredential{{AccountID: "first", AccessToken: "first-token"}},
		errors:      []error{nil, unexpected},
	}
	authority, err := NewCodexPoolAuthorityWithOutcomes(snapshot, resolver, quotaRetryOutcomeRecorder{})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := authority.ResolveAttempt(context.Background(), providers.DispatchRequest{})
	if err != nil {
		t.Fatal(err)
	}
	_, ok, retryErr := attempt.RetryQuota(context.Background())
	if ok || !errors.Is(retryErr, unexpected) {
		t.Fatalf("retry ok=%v err=%v", ok, retryErr)
	}
}

func TestCodexPoolAuthorityQuotaRetryPropagatesCancellation(t *testing.T) {
	snapshot := &fakePoolSnapshotSource{request: codexauth.PoolCredentialRequest{Selection: codexauth.PoolSelectionInput{}}}
	resolver := &quotaRetryPoolResolver{credentials: []codexauth.PoolCredential{{AccountID: "first", AccessToken: "first-token"}}}
	authority, err := NewCodexPoolAuthorityWithOutcomes(snapshot, resolver, quotaRetryOutcomeRecorder{})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := authority.ResolveAttempt(context.Background(), providers.DispatchRequest{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, ok, retryErr := attempt.RetryQuota(ctx)
	if ok || !errors.Is(retryErr, context.Canceled) {
		t.Fatalf("retry ok=%v err=%v", ok, retryErr)
	}
}
