package openairesponses

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/codexauth"
	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
)

type fakePoolSnapshotSource struct {
	request codexauth.PoolCredentialRequest
	err     error
	seen    []CodexPoolDispatchIdentity
}

func (f *fakePoolSnapshotSource) Snapshot(_ context.Context, identity CodexPoolDispatchIdentity) (codexauth.PoolCredentialRequest, error) {
	f.seen = append(f.seen, identity)
	return f.request, f.err
}

type fakePoolResolver struct {
	credential codexauth.PoolCredential
	err        error
	seen       []codexauth.PoolCredentialRequest
}

func (f *fakePoolResolver) Resolve(_ context.Context, request codexauth.PoolCredentialRequest) (codexauth.PoolCredential, error) {
	f.seen = append(f.seen, request)
	return f.credential, f.err
}

func TestCodexPoolAuthorityIgnoresCallerCredentialsAndUsesEffectiveRoutingIdentity(t *testing.T) {
	snapshot := &fakePoolSnapshotSource{request: codexauth.PoolCredentialRequest{Selection: codexauth.PoolSelectionInput{
		ThreadID: "snapshot-thread",
		Now:      time.Unix(1_700_000_000, 0),
	}}}
	resolver := &fakePoolResolver{credential: codexauth.PoolCredential{
		AccountID: "selected", AccessToken: "selected-token", ChatGPTAccountID: "selected-chat", Generation: 4,
	}}
	authority, err := NewCodexPoolAuthority(snapshot, resolver)
	if err != nil {
		t.Fatal(err)
	}

	dispatch := providers.DispatchRequest{
		Parsed: protocol.ParsedRequest{
			ModelID:         "friendly-alias",
			UpstreamModelID: "gpt-5.3-codex-spark",
		},
		ForwardHeaders: providers.NewForwardHeadersWithBlockedAuthorization(map[string]string{
			"authorization":            "Bearer attacker",
			"chatgpt-account-id":       "caller-chat",
			"x-codex-parent-thread-id": "thread-live",
		}, true),
	}
	credential, err := authority.Resolve(context.Background(), dispatch)
	if err != nil {
		t.Fatal(err)
	}
	if credential.Authorization != "Bearer selected-token" || credential.ChatGPTAccountID != "selected-chat" {
		t.Fatalf("credential=%#v", credential)
	}
	if len(snapshot.seen) != 1 || snapshot.seen[0].ModelID != "gpt-5.3-codex-spark" || snapshot.seen[0].ThreadID != "thread-live" {
		t.Fatalf("snapshot identity=%#v", snapshot.seen)
	}
	if len(resolver.seen) != 1 || resolver.seen[0].Selection.ThreadID != "thread-live" {
		t.Fatalf("resolver request=%#v", resolver.seen)
	}
}

func TestCodexPoolAuthorityFallsBackToSemanticModelAndDoesNotExposeDispatchHeaders(t *testing.T) {
	snapshot := &fakePoolSnapshotSource{request: codexauth.PoolCredentialRequest{Selection: codexauth.PoolSelectionInput{}}}
	resolver := &fakePoolResolver{credential: codexauth.PoolCredential{AccountID: "acct", AccessToken: "token"}}
	authority, err := NewCodexPoolAuthority(snapshot, resolver)
	if err != nil {
		t.Fatal(err)
	}

	_, err = authority.Resolve(context.Background(), providers.DispatchRequest{
		Parsed: protocol.ParsedRequest{ModelID: "semantic-model"},
		ForwardHeaders: providers.NewForwardHeaders(map[string]string{
			"authorization":      "Bearer caller-secret",
			"chatgpt-account-id": "caller-account-secret",
			"x-codex-window-id":  "window-metadata",
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.seen) != 1 || snapshot.seen[0] != (CodexPoolDispatchIdentity{ModelID: "semantic-model"}) {
		t.Fatalf("snapshot identity leaked extra dispatch data: %#v", snapshot.seen)
	}
}

func TestCodexPoolAuthorityPropagatesSnapshotAndResolverFailuresWithoutFallback(t *testing.T) {
	snapshotErr := errors.New("safe snapshot failure")
	snapshot := &fakePoolSnapshotSource{err: snapshotErr}
	resolver := &fakePoolResolver{}
	authority, err := NewCodexPoolAuthority(snapshot, resolver)
	if err != nil {
		t.Fatal(err)
	}
	_, err = authority.Resolve(context.Background(), providers.DispatchRequest{})
	if !errors.Is(err, snapshotErr) || len(resolver.seen) != 0 {
		t.Fatalf("snapshot err=%v resolver calls=%d", err, len(resolver.seen))
	}

	resolverErr := codexauth.ErrPoolNoUsableAccount
	snapshot.err = nil
	resolver.err = resolverErr
	_, err = authority.Resolve(context.Background(), providers.DispatchRequest{})
	if !errors.Is(err, resolverErr) {
		t.Fatalf("resolver err=%v", err)
	}
}

func TestCodexPoolAuthorityRejectsBlankOrWhitespaceMaterializedTokenBeforeForwardTransport(t *testing.T) {
	for _, token := range []string{"   ", "token with-space", "token\nnewline"} {
		snapshot := &fakePoolSnapshotSource{}
		resolver := &fakePoolResolver{credential: codexauth.PoolCredential{AccountID: "acct", AccessToken: token}}
		authority, err := NewCodexPoolAuthority(snapshot, resolver)
		if err != nil {
			t.Fatal(err)
		}
		_, err = authority.Resolve(context.Background(), providers.DispatchRequest{})
		if !errors.Is(err, ErrCodexPoolCredentialInvalid) {
			t.Fatalf("token=%q err=%v", token, err)
		}
	}
}

func TestNewCodexPoolAuthorityRejectsMissingDependencies(t *testing.T) {
	if _, err := NewCodexPoolAuthority(nil, &fakePoolResolver{}); !errors.Is(err, ErrCodexPoolAuthorityConfiguration) {
		t.Fatalf("nil snapshot err=%v", err)
	}
	if _, err := NewCodexPoolAuthority(&fakePoolSnapshotSource{}, nil); !errors.Is(err, ErrCodexPoolAuthorityConfiguration) {
		t.Fatalf("nil resolver err=%v", err)
	}
}

var _ ForwardCredentialAuthority = (*CodexPoolAuthority)(nil)
