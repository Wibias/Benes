package openairesponses

import (
	"context"
	"testing"

	"github.com/Wibias/Benes/internal/codexauth"
	"github.com/Wibias/Benes/internal/providers"
)

func TestCodexPoolAuthorityExposesSameOneShotAlternateForQuotaAndModel400(t *testing.T) {
	snapshot := &fakePoolSnapshotSource{request: codexauth.PoolCredentialRequest{Selection: codexauth.PoolSelectionInput{}}}
	resolver := &quotaRetryPoolResolver{credentials: []codexauth.PoolCredential{
		{AccountID: "first", AccessToken: "first-token"},
		{AccountID: "second", AccessToken: "second-token"},
		{AccountID: "first", AccessToken: "first-token"},
		{AccountID: "second", AccessToken: "second-token"},
	}}
	authority, err := NewCodexPoolAuthorityWithOutcomes(snapshot, resolver, quotaRetryOutcomeRecorder{})
	if err != nil {
		t.Fatal(err)
	}

	quotaAttempt, err := authority.ResolveAttempt(context.Background(), providers.DispatchRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if quotaAttempt.RetryQuota == nil || quotaAttempt.RetryModel400 == nil {
		t.Fatalf("first attempt retry seams quota=%v model400=%v", quotaAttempt.RetryQuota != nil, quotaAttempt.RetryModel400 != nil)
	}
	quotaRetry, ok, err := quotaAttempt.RetryQuota(context.Background())
	if err != nil || !ok {
		t.Fatalf("quota retry ok=%v err=%v", ok, err)
	}
	if quotaRetry.RetryQuota != nil || quotaRetry.RetryModel400 != nil {
		t.Fatal("quota alternate exposed another account retry")
	}
	if quotaRetry.CommitQuotaRetry == nil {
		t.Fatal("quota alternate lost deferred quota promotion")
	}

	modelAttempt, err := authority.ResolveAttempt(context.Background(), providers.DispatchRequest{})
	if err != nil {
		t.Fatal(err)
	}
	modelRetry, ok, err := modelAttempt.RetryModel400(context.Background())
	if err != nil || !ok {
		t.Fatalf("model retry ok=%v err=%v", ok, err)
	}
	if modelRetry.RetryQuota != nil || modelRetry.RetryModel400 != nil {
		t.Fatal("model alternate exposed another account retry")
	}
	if modelRetry.CommitQuotaRetry != nil {
		t.Fatal("model-400 alternate must not promote active Pool account")
	}
	if len(resolver.seen) != 4 {
		t.Fatalf("resolver calls=%d", len(resolver.seen))
	}
	if !resolver.seen[1].Alternate || !resolver.seen[3].Alternate || resolver.seen[1].Selection.ExcludeAccountID != "first" || resolver.seen[3].Selection.ExcludeAccountID != "first" {
		t.Fatalf("retry requests=%#v", resolver.seen)
	}
}
