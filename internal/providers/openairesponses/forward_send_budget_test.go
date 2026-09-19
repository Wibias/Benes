package openairesponses

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/resourcebudget"
)

func TestForwardAccountRetrySharesTurnPhysicalSendBudget(t *testing.T) {
	calls := 0
	authority := forwardAttemptAuthorityFunc(func(context.Context, providers.DispatchRequest) (ForwardAttempt, error) {
		return ForwardAttempt{
			Credential: ForwardCredential{
				Authorization:    "Bearer first",
				ChatGPTAccountID: "chat-first",
				TrustedAccountID: "account-first",
			},
			RetryQuota: func(context.Context) (ForwardAttempt, bool, error) {
				return ForwardAttempt{Credential: ForwardCredential{
					Authorization:    "Bearer second",
					ChatGPTAccountID: "chat-second",
					TrustedAccountID: "account-second",
				}}, true, nil
			},
		}, nil
	})
	client, err := NewForward(ForwardConfig{
		Endpoint: testCanonicalForwardResponsesEndpoint,
		HTTPClient: &http.Client{Transport: forwardRoundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"error":"quota"}`)),
			}, nil
		})},
		CredentialAuthority: authority,
	})
	if err != nil {
		t.Fatal(err)
	}

	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxPhysicalSends: 1})
	turn, err := budget.AcquireTurn(context.Background(), "forward-send-budget")
	if err != nil {
		t.Fatal(err)
	}
	defer turn.Close()

	dispatch := canonicalRequest(t, `{"model":"openai/gpt-5.6","input":"hello"}`, "gpt-5.6")
	dispatch.Turn = turn
	_, err = client.Open(context.Background(), dispatch)
	if !errors.Is(err, resourcebudget.ErrPhysicalSendBudgetExceeded) {
		t.Fatalf("err=%v", err)
	}
	if calls != 1 {
		t.Fatalf("physical calls=%d want=1", calls)
	}
}
