package openairesponses

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/providers"
)

type capturingAccountObserver struct {
	labels []providers.CommittedUsageAccount
}

func (c *capturingAccountObserver) NoteCommittedUsageAccount(account providers.CommittedUsageAccount) {
	c.labels = append(c.labels, account)
}

func TestForwardNotesCommittedAccountOnSuccessfulStream(t *testing.T) {
	observer := &capturingAccountObserver{}
	client, err := NewForward(ForwardConfig{
		Endpoint: testCanonicalForwardResponsesEndpoint,
		HTTPClient: &http.Client{Transport: forwardRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.Header.Get("X-Benes-Surface") != "" {
				t.Fatalf("surface header forwarded upstream: %q", req.Header.Get("X-Benes-Surface"))
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader("data: {\"type\":\"response.completed\",\"response\":{}}\n\n")),
			}, nil
		})},
		CredentialAuthority: forwardAttemptAuthorityFunc(func(context.Context, providers.DispatchRequest) (ForwardAttempt, error) {
			return ForwardAttempt{Credential: ForwardCredential{
				Authorization:    "Bearer live-token",
				TrustedAccountID: "acct-uuid",
				UsageAccount:     "alpha",
			}}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := providers.WithCommittedUsageAccountObserver(context.Background(), observer)
	stream, err := client.Open(ctx, observedForwardDispatch(t))
	if err != nil {
		t.Fatal(err)
	}
	drainStream(t, stream)
	if len(observer.labels) != 1 || observer.labels[0] != "alpha" {
		t.Fatalf("committed=%v", observer.labels)
	}
}

func TestForwardRetryNotesOnlyCommittedAccount(t *testing.T) {
	observer := &capturingAccountObserver{}
	client, err := NewForward(ForwardConfig{
		Endpoint: testCanonicalForwardResponsesEndpoint,
		HTTPClient: &http.Client{Transport: forwardRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.Header.Get("Authorization") == "Bearer first" {
				return &http.Response{
					StatusCode: http.StatusTooManyRequests,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(`{"error":"quota"}`)),
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader("data: {\"type\":\"response.completed\",\"response\":{}}\n\n")),
			}, nil
		})},
		CredentialAuthority: forwardAttemptAuthorityFunc(func(context.Context, providers.DispatchRequest) (ForwardAttempt, error) {
			return ForwardAttempt{
				Credential: ForwardCredential{Authorization: "Bearer first", TrustedAccountID: "acct-a", UsageAccount: "alpha"},
				RetryQuota: func(context.Context) (ForwardAttempt, bool, error) {
					return ForwardAttempt{Credential: ForwardCredential{
						Authorization:    "Bearer second",
						TrustedAccountID: "acct-b",
						UsageAccount:     "bravo",
					}}, true, nil
				},
			}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := providers.WithCommittedUsageAccountObserver(context.Background(), observer)
	stream, err := client.Open(ctx, observedForwardDispatch(t))
	if err != nil {
		t.Fatal(err)
	}
	drainStream(t, stream)
	if len(observer.labels) != 1 || observer.labels[0] != "bravo" {
		t.Fatalf("retry committed=%v", observer.labels)
	}
}

func TestForwardDoesNotNoteRawAccountIDWhenUsageAccountMissing(t *testing.T) {
	observer := &capturingAccountObserver{}
	client, err := NewForward(ForwardConfig{
		Endpoint: testCanonicalForwardResponsesEndpoint,
		HTTPClient: &http.Client{Transport: forwardRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader("data: {\"type\":\"response.completed\",\"response\":{}}\n\n")),
			}, nil
		})},
		CredentialAuthority: forwardAttemptAuthorityFunc(func(context.Context, providers.DispatchRequest) (ForwardAttempt, error) {
			return ForwardAttempt{Credential: ForwardCredential{
				Authorization:    "Bearer live-token",
				TrustedAccountID: "acct-uuid",
			}}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := providers.WithCommittedUsageAccountObserver(context.Background(), observer)
	stream, err := client.Open(ctx, observedForwardDispatch(t))
	if err != nil {
		t.Fatal(err)
	}
	drainStream(t, stream)
	if len(observer.labels) != 0 {
		t.Fatalf("raw account id noted as usage identity: %v", observer.labels)
	}
}

func TestForwardFailedOpenDoesNotNoteAccount(t *testing.T) {
	observer := &capturingAccountObserver{}
	client, err := NewForward(ForwardConfig{
		Endpoint: testCanonicalForwardResponsesEndpoint,
		HTTPClient: &http.Client{Transport: forwardRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusUnauthorized,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"error":"no"}`)),
			}, nil
		})},
		CredentialAuthority: forwardAttemptAuthorityFunc(func(context.Context, providers.DispatchRequest) (ForwardAttempt, error) {
			return ForwardAttempt{Credential: ForwardCredential{
				Authorization:    "Bearer first",
				TrustedAccountID: "acct-a",
				UsageAccount:     "alpha",
			}}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := providers.WithCommittedUsageAccountObserver(context.Background(), observer)
	_, err = client.Open(ctx, observedForwardDispatch(t))
	if err == nil {
		t.Fatal("expected failure")
	}
	if len(observer.labels) != 0 {
		t.Fatalf("failed open noted account=%v", observer.labels)
	}
}
