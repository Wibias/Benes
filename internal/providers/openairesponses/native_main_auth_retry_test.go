package openairesponses

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
)

func TestForwardAuthRetryReplaysNativeMainOnceAfter401(t *testing.T) {
	first := &forwardOutcomeCollector{}
	second := &forwardOutcomeCollector{}
	authRetries := 0
	authority := forwardAttemptAuthorityFunc(func(context.Context, providers.DispatchRequest) (ForwardAttempt, error) {
		return ForwardAttempt{
			Credential: ForwardCredential{Authorization: "Bearer expired-main", ChatGPTAccountID: "main-chat", TrustedAccountID: "__main__"},
			Observer:   first,
			RetryAuth: func(context.Context) (ForwardAttempt, bool, error) {
				authRetries++
				return ForwardAttempt{
					Credential: ForwardCredential{Authorization: "Bearer refreshed-main", ChatGPTAccountID: "main-chat", TrustedAccountID: "__main__"},
					Observer:   second,
				}, true, nil
			},
		}, nil
	})

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
	defer stream.Close()
	if _, err := stream.Next(); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || authRetries != 1 {
		t.Fatalf("calls=%d authRetries=%d authorizations=%v", calls, authRetries, authorizations)
	}
	if authorizations[0] != "Bearer expired-main" || authorizations[1] != "Bearer refreshed-main" {
		t.Fatalf("authorizations=%v", authorizations)
	}
}

func TestForwardAuthRetryDoesNotReplayASecond401(t *testing.T) {
	authRetries := 0
	authority := forwardAttemptAuthorityFunc(func(context.Context, providers.DispatchRequest) (ForwardAttempt, error) {
		return ForwardAttempt{
			Credential: ForwardCredential{Authorization: "Bearer first", TrustedAccountID: "__main__"},
			RetryAuth: func(context.Context) (ForwardAttempt, bool, error) {
				authRetries++
				return ForwardAttempt{
					Credential: ForwardCredential{Authorization: "Bearer second", TrustedAccountID: "__main__"},
					RetryAuth: func(context.Context) (ForwardAttempt, bool, error) {
						t.Fatal("second RetryAuth must not run")
						return ForwardAttempt{}, false, nil
					},
				}, true, nil
			},
		}, nil
	})

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
		t.Fatal("expected 401 after the single replay")
	}
	if calls != 2 || authRetries != 1 {
		t.Fatalf("calls=%d authRetries=%d", calls, authRetries)
	}
}

func TestForwardCompactAuthRetryReplaysNativeMainOnceAfter401(t *testing.T) {
	authRetries := 0
	authority := forwardAttemptAuthorityFunc(func(context.Context, providers.DispatchRequest) (ForwardAttempt, error) {
		return ForwardAttempt{
			Credential: ForwardCredential{Authorization: "Bearer expired-main", ChatGPTAccountID: "main-chat"},
			RetryAuth: func(context.Context) (ForwardAttempt, bool, error) {
				authRetries++
				return ForwardAttempt{
					Credential: ForwardCredential{Authorization: "Bearer refreshed-main", ChatGPTAccountID: "main-chat"},
				}, true, nil
			},
		}, nil
	})

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

	status, _, payload, err := client.Compact(context.Background(), providers.DispatchRequest{
		Parsed: protocol.ParsedRequest{UpstreamModelID: "gpt-5"},
	}, []byte(`{"model":"gpt-5","input":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || string(payload) != `{"ok":true}` {
		t.Fatalf("status=%d payload=%s", status, payload)
	}
	if calls != 2 || authRetries != 1 {
		t.Fatalf("calls=%d authRetries=%d authorizations=%v", calls, authRetries, authorizations)
	}
}
