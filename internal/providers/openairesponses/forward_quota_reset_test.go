package openairesponses

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/codexauth"
	"github.com/Wibias/Benes/internal/providers"
)

func TestForwardAttemptObservesCodexQuotaResetHeaders(t *testing.T) {
	collector := &forwardOutcomeCollector{}
	authority := forwardAttemptAuthorityFunc(func(context.Context, providers.DispatchRequest) (ForwardAttempt, error) {
		return ForwardAttempt{
			Credential: ForwardCredential{Authorization: "Bearer selected"},
			Observer: collector,
		}, nil
	})
	client, err := NewForward(ForwardConfig{
		Endpoint: testCanonicalForwardResponsesEndpoint,
		HTTPClient: &http.Client{Transport: forwardRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Header: http.Header{
					"Content-Type": []string{"application/json"},
					"X-Codex-Primary-Reset-At": []string{"1700000100"},
					"X-Codex-Secondary-Reset-At": []string{"1700000200"},
					"X-Codex-Tertiary-Reset-At": []string{"1700000300"},
				},
				Body: io.NopCloser(strings.NewReader(`{"error":"quota"}`)),
			}, nil
		})},
		CredentialAuthority: authority,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.Open(context.Background(), observedForwardDispatch(t))
	if err == nil {
		t.Fatal("expected upstream status error")
	}
	if len(collector.outcomes) != 1 {
		t.Fatalf("outcomes=%#v", collector.outcomes)
	}
	got := collector.outcomes[0]
	if len(got.ResetAt) != 3 || got.ResetAt[0] != "1700000100" || got.ResetAt[1] != "1700000200" || got.ResetAt[2] != "1700000300" {
		t.Fatalf("resetAt=%#v", got.ResetAt)
	}
}

func TestCodexPoolAttemptObserverCarriesResetEvidenceToOutcomeRecorder(t *testing.T) {
	outcomes := &fakePoolOutcomeRecorder{}
	observer := newCodexPoolAttemptObserver(outcomes, codexauth.PoolCredential{AccountID: "acct"})
	observer.Observe(ForwardOutcome{
		Kind: ForwardOutcomeHTTP,
		StatusCode: http.StatusTooManyRequests,
		ResetAt: []string{"1700000100", "1700000200"},
	})
	if len(outcomes.calls) != 1 || len(outcomes.calls[0].meta.ResetAt) != 2 || outcomes.calls[0].meta.ResetAt[0] != "1700000100" || outcomes.calls[0].meta.ResetAt[1] != "1700000200" {
		t.Fatalf("calls=%#v", outcomes.calls)
	}
}
