package openairesponses

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/providers"
)

type quotaForwardObserver struct {
	quotas   []ForwardQuotaHeaders
	outcomes []ForwardOutcome
	order    []string
}

func (o *quotaForwardObserver) ObserveQuota(headers ForwardQuotaHeaders) {
	o.quotas = append(o.quotas, headers)
	o.order = append(o.order, "quota")
}

func (o *quotaForwardObserver) Observe(outcome ForwardOutcome) {
	o.outcomes = append(o.outcomes, outcome)
	o.order = append(o.order, "outcome")
}

type quotaAttemptAuthority struct{ observer *quotaForwardObserver }

func (a quotaAttemptAuthority) Resolve(context.Context, providers.DispatchRequest) (ForwardCredential, error) {
	return ForwardCredential{Authorization: "Bearer selected", ChatGPTAccountID: "acct"}, nil
}

func (a quotaAttemptAuthority) ResolveAttempt(context.Context, providers.DispatchRequest) (ForwardAttempt, error) {
	credential, _ := a.Resolve(context.Background(), providers.DispatchRequest{})
	return ForwardAttempt{Credential: credential, Observer: a.observer}, nil
}

func TestForwardQuotaObserverRunsAtHTTPResponseBeforeStreamTerminal(t *testing.T) {
	observer := &quotaForwardObserver{}
	client, err := NewForward(ForwardConfig{
		Endpoint: testCanonicalForwardResponsesEndpoint,
		HTTPClient: &http.Client{Transport: forwardRoundTripFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type":                     []string{"text/event-stream"},
					"X-Codex-Primary-Used-Percent":     []string{"45"},
					"X-Codex-Secondary-Used-Percent":   []string{"12"},
					"X-Codex-Tertiary-Used-Percent":    []string{"3"},
					"X-Codex-Primary-Reset-At":         []string{"1700000100"},
					"X-Codex-Secondary-Reset-At":       []string{"1700000200"},
					"X-Codex-Tertiary-Reset-At":        []string{"1700000300"},
					"X-Codex-Primary-Window-Minutes":   []string{"10080"},
					"X-Codex-Secondary-Window-Minutes": []string{"40320"},
					"Authorization":                    []string{"Bearer upstream-secret"},
					"Set-Cookie":                       []string{"session=secret"},
				},
				Body:    io.NopCloser(strings.NewReader("data: {\"type\":\"response.completed\",\"response\":{}}\n\n")),
				Request: request,
			}, nil
		})},
		CredentialAuthority: quotaAttemptAuthority{observer: observer},
	})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := client.Open(context.Background(), canonicalRequest(t, `{"model":"openai/gpt-5.6","store":false}`, "gpt-5.6"))
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if len(observer.quotas) != 1 {
		t.Fatalf("quota observations=%d", len(observer.quotas))
	}
	quota := observer.quotas[0]
	if quota.PrimaryUsedPercent != "45" || quota.SecondaryUsedPercent != "12" || quota.TertiaryUsedPercent != "3" || quota.PrimaryResetAt != "1700000100" || quota.SecondaryResetAt != "1700000200" || quota.TertiaryResetAt != "1700000300" || quota.PrimaryWindowMinutes != "10080" || quota.SecondaryWindowMinutes != "40320" {
		t.Fatalf("quota=%#v", quota)
	}
	if len(observer.outcomes) != 0 || len(observer.order) != 1 || observer.order[0] != "quota" {
		t.Fatalf("observation order=%#v outcomes=%#v", observer.order, observer.outcomes)
	}
	if _, err := stream.Next(); err != nil {
		t.Fatal(err)
	}
	if len(observer.outcomes) != 1 || observer.outcomes[0].Kind != ForwardOutcomeCompleted || len(observer.order) != 2 || observer.order[1] != "outcome" {
		t.Fatalf("order=%#v outcomes=%#v", observer.order, observer.outcomes)
	}
}

func TestForwardQuotaObserverRunsOnNonSuccessBeforeHTTPOutcome(t *testing.T) {
	observer := &quotaForwardObserver{}
	client, err := NewForward(ForwardConfig{
		Endpoint: testCanonicalForwardResponsesEndpoint,
		HTTPClient: &http.Client{Transport: forwardRoundTripFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Header: http.Header{
					"X-Codex-Primary-Used-Percent": []string{"100"},
					"X-Codex-Primary-Reset-At":     []string{"1700000100"},
				},
				Body:    io.NopCloser(strings.NewReader("rate limited")),
				Request: request,
			}, nil
		})},
		CredentialAuthority: quotaAttemptAuthority{observer: observer},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Open(context.Background(), canonicalRequest(t, `{"model":"openai/gpt-5.6","store":false}`, "gpt-5.6"))
	if err == nil {
		t.Fatal("expected upstream status error")
	}
	if len(observer.quotas) != 1 || observer.quotas[0].PrimaryUsedPercent != "100" {
		t.Fatalf("quotas=%#v", observer.quotas)
	}
	if len(observer.outcomes) != 1 || observer.outcomes[0].Kind != ForwardOutcomeHTTP || observer.outcomes[0].StatusCode != http.StatusTooManyRequests {
		t.Fatalf("outcomes=%#v", observer.outcomes)
	}
	if len(observer.order) != 2 || observer.order[0] != "quota" || observer.order[1] != "outcome" {
		t.Fatalf("observation order=%#v", observer.order)
	}
}
