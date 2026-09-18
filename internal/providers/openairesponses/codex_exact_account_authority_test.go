package openairesponses

import (
	"context"
	"testing"

	"github.com/Wibias/Benes/internal/providers"
)

type exactAccountPoolAuthority struct {
	attempt ForwardAttempt
	calls   []providers.DispatchRequest
}

func (a *exactAccountPoolAuthority) Resolve(ctx context.Context, dispatch providers.DispatchRequest) (ForwardCredential, error) {
	attempt, err := a.ResolveAttempt(ctx, dispatch)
	return attempt.Credential, err
}

func (a *exactAccountPoolAuthority) ResolveAttempt(_ context.Context, dispatch providers.DispatchRequest) (ForwardAttempt, error) {
	a.calls = append(a.calls, dispatch)
	return a.attempt, nil
}

func TestCodexAccountAuthorityKeepsOrdinaryDirectCallerCredentials(t *testing.T) {
	pool := &exactAccountPoolAuthority{attempt: ForwardAttempt{Credential: ForwardCredential{Authorization: "Bearer stored"}}}
	authority := NewCodexAccountAuthority(DirectForwardAuthority{}, pool)
	dispatch := providers.DispatchRequest{ForwardHeaders: providers.NewForwardHeaders(map[string]string{
		"authorization":      "Bearer caller",
		"chatgpt-account-id": "caller-chat",
	})}
	attempt, err := authority.ResolveAttempt(context.Background(), dispatch)
	if err != nil {
		t.Fatal(err)
	}
	if attempt.Credential.Authorization != "Bearer caller" || attempt.Credential.ChatGPTAccountID != "caller-chat" || len(pool.calls) != 0 {
		t.Fatalf("attempt=%#v poolCalls=%d", attempt, len(pool.calls))
	}
}

func TestCodexAccountAuthorityExactSelectorOverridesDirectAndDisablesPoolRetries(t *testing.T) {
	authRetry := func(context.Context) (ForwardAttempt, bool, error) { return ForwardAttempt{}, false, nil }
	pool := &exactAccountPoolAuthority{attempt: ForwardAttempt{
		Credential:    ForwardCredential{Authorization: "Bearer stored", ChatGPTAccountID: "stored-chat"},
		RetryAuth:     authRetry,
		RetryQuota:    func(context.Context) (ForwardAttempt, bool, error) { return ForwardAttempt{}, false, nil },
		RetryModel400: func(context.Context) (ForwardAttempt, bool, error) { return ForwardAttempt{}, false, nil },
	}}
	authority := NewCodexAccountAuthority(DirectForwardAuthority{}, pool)
	dispatch := providers.DispatchRequest{
		CodexAccountID: "acct-b",
		ForwardHeaders: providers.NewForwardHeadersWithBlockedAuthorization(map[string]string{
			"authorization": "Bearer local-admission",
		}, true),
	}
	attempt, err := authority.ResolveAttempt(context.Background(), dispatch)
	if err != nil {
		t.Fatal(err)
	}
	if attempt.Credential.Authorization != "Bearer stored" || attempt.Credential.ChatGPTAccountID != "stored-chat" {
		t.Fatalf("attempt=%#v", attempt)
	}
	if attempt.RetryQuota != nil || attempt.RetryModel400 != nil || attempt.CommitQuotaRetry != nil {
		t.Fatalf("fixed attempt exposed pool failover: %#v", attempt)
	}
	if attempt.RetryAuth == nil {
		t.Fatal("exact selector stripped same-account 401 recovery")
	}
	if len(pool.calls) != 1 || pool.calls[0].CodexAccountID != "acct-b" {
		t.Fatalf("pool calls=%#v", pool.calls)
	}
}
