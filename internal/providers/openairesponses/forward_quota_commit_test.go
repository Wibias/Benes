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

func TestForwardQuotaRetryCommitsSelectedAlternateOnlyAfterRejectedOutcome(t *testing.T) {
	first := &quotaForwardObserver{}
	second := &forwardOutcomeCollector{}
	committed := false
	authority := forwardAttemptAuthorityFunc(func(context.Context, providers.DispatchRequest) (ForwardAttempt, error) {
		return ForwardAttempt{
			Credential: ForwardCredential{Authorization: "Bearer first"},
			Observer:   first,
			RetryQuota: func(context.Context) (ForwardAttempt, bool, error) {
				if committed || len(first.quotas) != 0 || len(first.outcomes) != 0 {
					t.Fatalf("retry resolved after state mutation: committed=%v quotas=%#v outcomes=%#v", committed, first.quotas, first.outcomes)
				}
				return ForwardAttempt{
					Credential: ForwardCredential{Authorization: "Bearer second"},
					Observer:   second,
					CommitQuotaRetry: func() {
						if len(first.quotas) != 1 || len(first.outcomes) != 1 {
							t.Fatalf("alternate committed before rejected evidence: quotas=%#v outcomes=%#v", first.quotas, first.outcomes)
						}
						committed = true
					},
				}, true, nil
			},
		}, nil
	})

	calls := 0
	client, err := NewForward(ForwardConfig{
		Endpoint: testCanonicalForwardResponsesEndpoint,
		HTTPClient: &http.Client{Transport: forwardRoundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				return &http.Response{
					StatusCode: http.StatusTooManyRequests,
					Header: http.Header{
						"Retry-After":                  []string{"60"},
						"X-Codex-Primary-Used-Percent": []string{"100"},
					},
					Body: io.NopCloser(strings.NewReader(`{"error":"quota"}`)),
				}, nil
			}
			if !committed {
				t.Fatal("alternate request sent before selected account promotion")
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
	if !committed || calls != 2 {
		t.Fatalf("committed=%v calls=%d", committed, calls)
	}
	event, err := stream.Next()
	if err != nil || event.Type != protocol.EventDone {
		t.Fatalf("event=%#v err=%v", event, err)
	}
}
