package openairesponses

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/codexauth"
	"github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/resourcebudget"
	"github.com/Wibias/Benes/internal/responses/continuation"
)

type storedAccountAuthRetryResolver struct {
	credentials []codexauth.PoolCredential
	seen        []codexauth.PoolCredentialRequest
}

func (r *storedAccountAuthRetryResolver) Resolve(_ context.Context, request codexauth.PoolCredentialRequest) (codexauth.PoolCredential, error) {
	r.seen = append(r.seen, request)
	if request.ForceStoredAccountRefresh {
		target := strings.TrimSpace(request.RefreshAccountID)
		if target == "" {
			target = strings.TrimSpace(request.FixedAccountID)
		}
		for _, credential := range r.credentials {
			if credential.AccountID == target && credential.Generation > 1 {
				return credential, nil
			}
		}
		for _, credential := range r.credentials {
			if credential.AccountID == target {
				return credential, nil
			}
		}
		return codexauth.PoolCredential{}, codexauth.ErrPoolSelectedCredentialUnavailable
	}
	index := 0
	for _, seen := range r.seen[:len(r.seen)-1] {
		if !seen.ForceStoredAccountRefresh {
			index++
		}
	}
	if index >= len(r.credentials) {
		return codexauth.PoolCredential{}, codexauth.ErrPoolNoUsableAccount
	}
	return r.credentials[index], nil
}

func TestCodexPoolNativeMain401StillForceRefreshesMain(t *testing.T) {
	snapshot := &fakePoolSnapshotSource{}
	resolver := &storedAccountAuthRetryResolver{credentials: []codexauth.PoolCredential{
		{AccountID: codexauth.MainAccountID, AccessToken: "stale-main", ChatGPTAccountID: "chat-main", WriterGeneration: 3},
		{AccountID: codexauth.MainAccountID, AccessToken: "fresh-main", ChatGPTAccountID: "chat-main", WriterGeneration: 3},
		{AccountID: "account-b", AccessToken: "account-b-token", ChatGPTAccountID: "chat-b", Generation: 1},
	}}
	authority, err := NewCodexPoolAuthorityWithOutcomes(snapshot, resolver, quotaRetryOutcomeRecorder{})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := authority.ResolveAttempt(context.Background(), observedForwardDispatch(t))
	if err != nil {
		t.Fatal(err)
	}
	if attempt.RetryAuth == nil {
		t.Fatal("native Main lost same-account RetryAuth")
	}
	retry, ok, err := attempt.RetryAuth(context.Background())
	if err != nil || !ok {
		t.Fatalf("retry ok=%v err=%v", ok, err)
	}
	if retry.Credential.Authorization != "Bearer fresh-main" || retry.Credential.TrustedAccountID != codexauth.MainAccountID {
		t.Fatalf("retry credential=%#v", retry.Credential)
	}
	if len(resolver.seen) != 2 {
		t.Fatalf("resolver calls=%d", len(resolver.seen))
	}
	if !resolver.seen[1].ForceMainRefresh || resolver.seen[1].ForceStoredAccountRefresh {
		t.Fatalf("native Main retry used stored-account recovery: %#v", resolver.seen[1])
	}
	if resolver.seen[1].RefreshAccountID != codexauth.MainAccountID {
		t.Fatalf("refresh account=%q", resolver.seen[1].RefreshAccountID)
	}
}

func TestCodexPoolStoredAccountRefreshesOnceAfterPreOutput401(t *testing.T) {
	snapshot := &fakePoolSnapshotSource{request: codexauth.PoolCredentialRequest{Selection: codexauth.PoolSelectionInput{
		ThreadID: "snapshot-thread",
	}}}
	resolver := &storedAccountAuthRetryResolver{credentials: []codexauth.PoolCredential{
		{AccountID: "account-a", AccessToken: "stale-a", ChatGPTAccountID: "chat-a", Generation: 1, WriterGeneration: 9},
		{AccountID: "account-a", AccessToken: "fresh-a", ChatGPTAccountID: "chat-a", Generation: 2, WriterGeneration: 9},
		{AccountID: "account-b", AccessToken: "account-b-token", ChatGPTAccountID: "chat-b", Generation: 1, WriterGeneration: 9},
	}}
	authority, err := NewCodexPoolAuthorityWithOutcomes(snapshot, resolver, quotaRetryOutcomeRecorder{})
	if err != nil {
		t.Fatal(err)
	}

	var calls int
	var authorizations []string
	var accountIDs []string
	client, err := NewForward(ForwardConfig{
		Endpoint: testCanonicalForwardResponsesEndpoint,
		HTTPClient: &http.Client{Transport: forwardRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			authorizations = append(authorizations, req.Header.Get("Authorization"))
			accountIDs = append(accountIDs, req.Header.Get("ChatGPT-Account-Id"))
			if calls == 1 {
				return &http.Response{
					StatusCode: http.StatusUnauthorized,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(`{"error":"invalid_token"}`)),
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader("data: {\"type\":\"response.completed\",\"response\":{}}\n\n")),
			}, nil
		})},
		CredentialAuthority: authority,
	})
	if err != nil {
		t.Fatal(err)
	}

	dispatch := observedForwardDispatch(t)
	dispatch.ForwardHeaders = providers.NewForwardHeaders(map[string]string{
		"x-codex-parent-thread-id": "live-thread",
	})
	stream, err := client.Open(context.Background(), dispatch)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if _, err := stream.Next(); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("calls=%d authorizations=%v accountIDs=%v resolver=%#v", calls, authorizations, accountIDs, resolver.seen)
	}
	if authorizations[0] != "Bearer stale-a" || authorizations[1] != "Bearer fresh-a" {
		t.Fatalf("authorizations=%v", authorizations)
	}
	if accountIDs[0] != "chat-a" || accountIDs[1] != "chat-a" {
		t.Fatalf("accountIDs=%v", accountIDs)
	}
	if len(resolver.seen) != 2 {
		t.Fatalf("resolver calls=%d seen=%#v", len(resolver.seen), resolver.seen)
	}
	if resolver.seen[1].Selection.ExcludeAccountID != "" || resolver.seen[1].Alternate {
		t.Fatalf("retry request leaked failover identity: %#v", resolver.seen[1])
	}
	if resolver.seen[1].FixedAccountID != "account-a" {
		t.Fatalf("retry did not pin stored account A: %#v", resolver.seen[1])
	}
	if resolver.seen[1].Selection.ThreadID != "live-thread" {
		t.Fatalf("retry thread=%q", resolver.seen[1].Selection.ThreadID)
	}
	if resolver.seen[1].ForceStoredAccountRefresh != true {
		t.Fatalf("retry did not force-refresh stored account A: %#v", resolver.seen[1])
	}
	if resolver.seen[1].ForceMainRefresh {
		t.Fatal("stored-account recovery used native Main force-refresh")
	}
}

func TestCodexPoolStoredAccountSecond401IsTerminal(t *testing.T) {
	snapshot := &fakePoolSnapshotSource{}
	resolver := &storedAccountAuthRetryResolver{credentials: []codexauth.PoolCredential{
		{AccountID: "account-a", AccessToken: "stale-a", ChatGPTAccountID: "chat-a", Generation: 1},
		{AccountID: "account-a", AccessToken: "fresh-a", ChatGPTAccountID: "chat-a", Generation: 2},
	}}
	authority, err := NewCodexPoolAuthorityWithOutcomes(snapshot, resolver, quotaRetryOutcomeRecorder{})
	if err != nil {
		t.Fatal(err)
	}
	var calls int
	client, err := NewForward(ForwardConfig{
		Endpoint: testCanonicalForwardResponsesEndpoint,
		HTTPClient: &http.Client{Transport: forwardRoundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			return &http.Response{
				StatusCode: http.StatusUnauthorized,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"error":"invalid_token"}`)),
			}, nil
		})},
		CredentialAuthority: authority,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Open(context.Background(), observedForwardDispatch(t)); err == nil {
		t.Fatal("expected terminal 401 after the single same-account replay")
	}
	if calls != 2 {
		t.Fatalf("calls=%d resolver=%#v", calls, resolver.seen)
	}
	if len(resolver.seen) != 2 {
		t.Fatalf("resolver calls=%d", len(resolver.seen))
	}
}

func TestCodexPoolStoredAccount401DoesNotSelectAccountB(t *testing.T) {
	snapshot := &fakePoolSnapshotSource{}
	resolver := &storedAccountAuthRetryResolver{credentials: []codexauth.PoolCredential{
		{AccountID: "account-a", AccessToken: "stale-a", ChatGPTAccountID: "chat-a", Generation: 1},
		{AccountID: "account-b", AccessToken: "account-b-token", ChatGPTAccountID: "chat-b", Generation: 1},
	}}
	authority, err := NewCodexPoolAuthorityWithOutcomes(snapshot, resolver, quotaRetryOutcomeRecorder{})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := authority.ResolveAttempt(context.Background(), observedForwardDispatch(t))
	if err != nil {
		t.Fatal(err)
	}
	if attempt.RetryAuth == nil {
		t.Fatal("stored account A did not expose same-account RetryAuth")
	}
	retry, ok, err := attempt.RetryAuth(context.Background())
	if err != nil || !ok {
		t.Fatalf("retry ok=%v err=%v", ok, err)
	}
	if retry.Credential.Authorization == "Bearer account-b-token" {
		t.Fatalf("retry hopped to account B: %#v seen=%#v", retry.Credential, resolver.seen)
	}
	if retry.Credential.TrustedAccountID != "account-a" {
		t.Fatalf("retry hopped accounts: %#v seen=%#v", retry.Credential, resolver.seen)
	}
	if retry.RetryAuth != nil {
		t.Fatal("same-account replay must not expose a second RetryAuth")
	}
}

func TestCodexPoolStoredAccountAuthRetryHonorsCancellation(t *testing.T) {
	snapshot := &fakePoolSnapshotSource{}
	resolver := &storedAccountAuthRetryResolver{credentials: []codexauth.PoolCredential{
		{AccountID: "account-a", AccessToken: "stale-a", ChatGPTAccountID: "chat-a", Generation: 1},
	}}
	authority, err := NewCodexPoolAuthorityWithOutcomes(snapshot, resolver, quotaRetryOutcomeRecorder{})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := authority.ResolveAttempt(context.Background(), observedForwardDispatch(t))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, ok, retryErr := attempt.RetryAuth(ctx)
	if ok || !errors.Is(retryErr, context.Canceled) {
		t.Fatalf("retry ok=%v err=%v", ok, retryErr)
	}
}

func TestCodexPoolStoredAccountAuthRetryRebindsContinuationToRefreshedA(t *testing.T) {
	store := continuation.NewStore(continuation.StoreLimits{TTL: time.Hour}, time.Now)
	continuationAuthority, err := continuation.NewAuthority(store, nil, bytes32('9'))
	if err != nil {
		t.Fatal(err)
	}
	snapshot := &fakePoolSnapshotSource{}
	resolver := &storedAccountAuthRetryResolver{credentials: []codexauth.PoolCredential{
		{AccountID: "account-a", AccessToken: "seed-a", ChatGPTAccountID: "chat-a", Generation: 1},
		{AccountID: "account-a", AccessToken: "stale-a", ChatGPTAccountID: "chat-a", Generation: 1},
		{AccountID: "account-a", AccessToken: "fresh-a", ChatGPTAccountID: "chat-a", Generation: 2},
		{AccountID: "account-b", AccessToken: "account-b-token", ChatGPTAccountID: "chat-b", Generation: 1},
	}}
	authority, err := NewCodexPoolAuthorityWithOutcomes(snapshot, resolver, quotaRetryOutcomeRecorder{})
	if err != nil {
		t.Fatal(err)
	}

	sse := func(id string) *http.Response {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body: io.NopCloser(strings.NewReader(
				`data: {"type":"response.completed","response":{"id":"` + id + `","status":"completed","output":[{"type":"message","id":"msg_` + id + `","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}` + "\n\n",
			)),
		}
	}

	var retryBody map[string]any
	var calls int
	client, err := NewForward(ForwardConfig{
		Endpoint: testCanonicalForwardResponsesEndpoint,
		HTTPClient: &http.Client{Transport: forwardRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			switch calls {
			case 1:
				if got := req.Header.Get("Authorization"); got != "Bearer seed-a" {
					t.Fatalf("seed authorization=%q", got)
				}
				return sse("resp_a"), nil
			case 2:
				if got := req.Header.Get("Authorization"); got != "Bearer stale-a" {
					t.Fatalf("stale authorization=%q", got)
				}
				return &http.Response{
					StatusCode: http.StatusUnauthorized,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(`{"error":"invalid_token"}`)),
				}, nil
			case 3:
				if got := req.Header.Get("Authorization"); got != "Bearer fresh-a" {
					t.Fatalf("retry authorization=%q", got)
				}
				if err := json.NewDecoder(req.Body).Decode(&retryBody); err != nil {
					t.Fatal(err)
				}
				return sse("resp_retry"), nil
			default:
				t.Fatalf("unexpected upstream call %d", calls)
				return nil, nil
			}
		})},
		CredentialAuthority: authority,
		Continuation:        continuationAuthority,
	})
	if err != nil {
		t.Fatal(err)
	}

	budget := resourcebudget.NewManager(resourcebudget.Limits{
		MaxActiveTurns:  2,
		MaxTurnBytes:    1 << 20,
		MaxProcessBytes: 1 << 20,
		ClassBytes: map[resourcebudget.Class]int64{
			resourcebudget.ClassContinuation: 1 << 20,
		},
	})
	seedTurn, err := budget.AcquireTurn(context.Background(), "seed-a")
	if err != nil {
		t.Fatal(err)
	}
	seed := canonicalRequest(t, `{"model":"openai/gpt-5.6","input":"seed"}`, "gpt-5.6")
	seed.Turn = seedTurn
	stream, err := client.Open(context.Background(), seed)
	if err != nil {
		seedTurn.Close()
		t.Fatal(err)
	}
	drainStream(t, stream)
	seedTurn.Close()

	followTurn, err := budget.AcquireTurn(context.Background(), "follow-a")
	if err != nil {
		t.Fatal(err)
	}
	follow := canonicalRequest(t, `{"model":"openai/gpt-5.6","previous_response_id":"resp_a","input":"next"}`, "gpt-5.6")
	follow.Turn = followTurn
	stream, err = client.Open(context.Background(), follow)
	if err != nil {
		followTurn.Close()
		t.Fatal(err)
	}
	drainStream(t, stream)
	followTurn.Close()

	if calls != 3 {
		t.Fatalf("calls=%d", calls)
	}
	if retryBody["previous_response_id"] != "resp_a" {
		t.Fatalf("same-account retry lost continuation owner: %#v", retryBody)
	}
}

func TestCodexPoolStoredAccountCompactRefreshesOnceAfter401(t *testing.T) {
	snapshot := &fakePoolSnapshotSource{}
	resolver := &storedAccountAuthRetryResolver{credentials: []codexauth.PoolCredential{
		{AccountID: "account-a", AccessToken: "stale-a", ChatGPTAccountID: "chat-a", Generation: 1},
		{AccountID: "account-a", AccessToken: "fresh-a", ChatGPTAccountID: "chat-a", Generation: 2},
	}}
	authority, err := NewCodexPoolAuthorityWithOutcomes(snapshot, resolver, quotaRetryOutcomeRecorder{})
	if err != nil {
		t.Fatal(err)
	}
	var calls int
	var authorizations []string
	client, err := NewForward(ForwardConfig{
		Endpoint: testCanonicalForwardResponsesEndpoint,
		HTTPClient: &http.Client{Transport: forwardRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			authorizations = append(authorizations, req.Header.Get("Authorization"))
			if calls == 1 {
				return &http.Response{
					StatusCode: http.StatusUnauthorized,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(`{"error":"invalid_token"}`)),
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			}, nil
		})},
		CredentialAuthority: authority,
	})
	if err != nil {
		t.Fatal(err)
	}
	status, _, payload, err := client.Compact(context.Background(), observedForwardDispatch(t), []byte(`{"model":"gpt-5","input":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || string(payload) != `{"ok":true}` {
		t.Fatalf("status=%d payload=%s", status, payload)
	}
	if calls != 2 || authorizations[0] != "Bearer stale-a" || authorizations[1] != "Bearer fresh-a" {
		t.Fatalf("calls=%d authorizations=%v", calls, authorizations)
	}
}

func TestCodexPoolExactSelectorStillRefreshesSameStoredAccountAfter401(t *testing.T) {
	snapshot := &fakePoolSnapshotSource{}
	resolver := &storedAccountAuthRetryResolver{credentials: []codexauth.PoolCredential{
		{AccountID: "account-a", AccessToken: "stale-a", ChatGPTAccountID: "chat-a", Generation: 1, FixedAccount: true},
		{AccountID: "account-a", AccessToken: "fresh-a", ChatGPTAccountID: "chat-a", Generation: 2, FixedAccount: true},
	}}
	authority, err := NewCodexPoolAuthorityWithOutcomes(snapshot, resolver, quotaRetryOutcomeRecorder{})
	if err != nil {
		t.Fatal(err)
	}
	dispatch := observedForwardDispatch(t)
	dispatch.CodexAccountID = "account-a"
	attempt, err := authority.ResolveAttempt(context.Background(), dispatch)
	if err != nil {
		t.Fatal(err)
	}
	if attempt.RetryQuota != nil || attempt.RetryModel400 != nil || attempt.CommitQuotaRetry != nil {
		t.Fatalf("exact selector exposed failover: %#v", attempt)
	}
	if attempt.RetryAuth == nil {
		t.Fatal("exact selector lost same-account 401 recovery")
	}
	retry, ok, err := attempt.RetryAuth(context.Background())
	if err != nil || !ok {
		t.Fatalf("retry ok=%v err=%v", ok, err)
	}
	if retry.Credential.Authorization != "Bearer fresh-a" || retry.Credential.TrustedAccountID != "account-a" {
		t.Fatalf("retry credential=%#v", retry.Credential)
	}
	if retry.RetryAuth != nil || retry.RetryQuota != nil {
		t.Fatal("exact-selector replay exposed extra retry")
	}
}

func TestCodexPoolStoredAccountPreOutput401ReleasesProbeWithoutCredentialFailure(t *testing.T) {
	snapshot := &fakePoolSnapshotSource{}
	resolver := &storedAccountAuthRetryResolver{credentials: []codexauth.PoolCredential{
		{AccountID: "account-a", AccessToken: "stale-a", ChatGPTAccountID: "chat-a", Generation: 1, ProbeLease: &codexauth.ProbeLease{AccountID: "account-a", LeaseID: "lease-1"}},
		{AccountID: "account-a", AccessToken: "fresh-a", ChatGPTAccountID: "chat-a", Generation: 2},
	}}
	outcomes := &fakePoolOutcomeRecorder{}
	authority, err := NewCodexPoolAuthorityWithOutcomes(snapshot, resolver, outcomes)
	if err != nil {
		t.Fatal(err)
	}
	var calls int
	client, err := NewForward(ForwardConfig{
		Endpoint: testCanonicalForwardResponsesEndpoint,
		HTTPClient: &http.Client{Transport: forwardRoundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				return &http.Response{
					StatusCode: http.StatusUnauthorized,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(`{"error":"invalid_token"}`)),
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader("data: {\"type\":\"response.completed\",\"response\":{}}\n\n")),
			}, nil
		})},
		CredentialAuthority: authority,
	})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := client.Open(context.Background(), observedForwardDispatch(t))
	if err != nil {
		t.Fatal(err)
	}
	drainStream(t, stream)
	if calls != 2 {
		t.Fatalf("calls=%d", calls)
	}
	if len(outcomes.calls) < 2 {
		t.Fatalf("outcomes=%#v", outcomes.calls)
	}
	if outcomes.calls[0].accountID != "account-a" || outcomes.calls[0].outcome.Kind != codexauth.OutcomeConnectNeutral {
		t.Fatalf("first outcome=%#v", outcomes.calls[0])
	}
	if outcomes.calls[0].meta.ProbeLease == nil || outcomes.calls[0].meta.ProbeLease.LeaseID != "lease-1" {
		t.Fatalf("probe lease not released via first outcome: %#v", outcomes.calls[0].meta.ProbeLease)
	}
	last := outcomes.calls[len(outcomes.calls)-1]
	if last.accountID != "account-a" || last.outcome.Kind != codexauth.OutcomeHTTP || last.outcome.StatusCode != 200 {
		t.Fatalf("last outcome=%#v", last)
	}
	for _, call := range outcomes.calls {
		if call.outcome.Kind == codexauth.OutcomeHTTP && call.outcome.StatusCode == http.StatusUnauthorized {
			t.Fatalf("pre-output 401 recorded as credential failure: %#v", outcomes.calls)
		}
	}
}

func TestCodexPoolStoredAccountSecond401RecordsCredentialFailure(t *testing.T) {
	snapshot := &fakePoolSnapshotSource{}
	resolver := &storedAccountAuthRetryResolver{credentials: []codexauth.PoolCredential{
		{AccountID: "account-a", AccessToken: "stale-a", ChatGPTAccountID: "chat-a", Generation: 1},
		{AccountID: "account-a", AccessToken: "fresh-a", ChatGPTAccountID: "chat-a", Generation: 2},
	}}
	outcomes := &fakePoolOutcomeRecorder{}
	authority, err := NewCodexPoolAuthorityWithOutcomes(snapshot, resolver, outcomes)
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewForward(ForwardConfig{
		Endpoint: testCanonicalForwardResponsesEndpoint,
		HTTPClient: &http.Client{Transport: forwardRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusUnauthorized,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"error":"invalid_token"}`)),
			}, nil
		})},
		CredentialAuthority: authority,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Open(context.Background(), observedForwardDispatch(t)); err == nil {
		t.Fatal("expected terminal 401")
	}
	var recorded401 int
	for _, call := range outcomes.calls {
		if call.outcome.Kind == codexauth.OutcomeHTTP && call.outcome.StatusCode == http.StatusUnauthorized {
			recorded401++
			if call.accountID != "account-a" {
				t.Fatalf("401 recorded for %#v", call)
			}
		}
	}
	if recorded401 != 1 {
		t.Fatalf("recorded 401 count=%d outcomes=%#v", recorded401, outcomes.calls)
	}
}
