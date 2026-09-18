package openairesponses

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
)

type forwardAttemptAuthorityFunc func(context.Context, providers.DispatchRequest) (ForwardAttempt, error)

func (f forwardAttemptAuthorityFunc) ResolveAttempt(ctx context.Context, dispatch providers.DispatchRequest) (ForwardAttempt, error) {
	return f(ctx, dispatch)
}

func (f forwardAttemptAuthorityFunc) Resolve(ctx context.Context, dispatch providers.DispatchRequest) (ForwardCredential, error) {
	attempt, err := f(ctx, dispatch)
	return attempt.Credential, err
}

type forwardOutcomeCollector struct {
	outcomes []ForwardOutcome
}

func (c *forwardOutcomeCollector) Observe(outcome ForwardOutcome) {
	c.outcomes = append(c.outcomes, outcome)
}

func TestForwardAttemptObservesNon2xxBeforeOpenReturns(t *testing.T) {
	collector := &forwardOutcomeCollector{}
	client := newObservedForwardClient(t, collector, func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header: http.Header{
				"Content-Type": []string{"application/json"},
				"Retry-After": []string{"120"},
			},
			Body: io.NopCloser(strings.NewReader(`{"error":"quota"}`)),
		}, nil
	})

	_, err := client.Open(context.Background(), observedForwardDispatch(t))
	if err == nil {
		t.Fatal("expected upstream status error")
	}
	if len(collector.outcomes) != 1 {
		t.Fatalf("outcomes=%#v", collector.outcomes)
	}
	got := collector.outcomes[0]
	if got.Kind != ForwardOutcomeHTTP || got.StatusCode != http.StatusTooManyRequests || got.RetryAfter != "120" {
		t.Fatalf("outcome=%#v", got)
	}
}

func TestForwardAttemptDefersSSESuccessUntilTerminalEvent(t *testing.T) {
	collector := &forwardOutcomeCollector{}
	client := newObservedForwardClient(t, collector, sseForwardResponse(
		"data: {\"type\":\"response.completed\",\"response\":{}}\n\n",
	))

	stream, err := client.Open(context.Background(), observedForwardDispatch(t))
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if len(collector.outcomes) != 0 {
		t.Fatalf("200 headers recorded before terminal event: %#v", collector.outcomes)
	}
	event, err := stream.Next()
	if err != nil {
		t.Fatal(err)
	}
	if event.Type != protocol.EventDone {
		t.Fatalf("event=%#v", event)
	}
	if len(collector.outcomes) != 1 || collector.outcomes[0].Kind != ForwardOutcomeCompleted {
		t.Fatalf("outcomes=%#v", collector.outcomes)
	}
	if _, err := stream.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("terminal Next err=%v", err)
	}
	if len(collector.outcomes) != 1 {
		t.Fatalf("terminal outcome recorded more than once: %#v", collector.outcomes)
	}
}

func TestForwardAttemptClassifiesFailedIncompleteAndEOFOnce(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want ForwardOutcomeKind
	}{
		{name: "failed", body: "data: {\"type\":\"response.failed\",\"response\":{}}\n\n", want: ForwardOutcomeFailed},
		{name: "incomplete", body: "data: {\"type\":\"response.incomplete\",\"response\":{}}\n\n", want: ForwardOutcomeIncomplete},
		{name: "eof before terminal", body: "", want: ForwardOutcomeIncomplete},
	} {
		t.Run(tc.name, func(t *testing.T) {
			collector := &forwardOutcomeCollector{}
			client := newObservedForwardClient(t, collector, sseForwardResponse(tc.body))
			stream, err := client.Open(context.Background(), observedForwardDispatch(t))
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			_, nextErr := stream.Next()
			if tc.body == "" && !errors.Is(nextErr, io.EOF) {
				t.Fatalf("EOF case err=%v", nextErr)
			}
			if len(collector.outcomes) != 1 || collector.outcomes[0].Kind != tc.want {
				t.Fatalf("outcomes=%#v", collector.outcomes)
			}
			_ = stream.Close()
			if len(collector.outcomes) != 1 {
				t.Fatalf("outcome duplicated on close: %#v", collector.outcomes)
			}
		})
	}
}

func TestForwardAttemptClientCloseBeforeTerminalIsNeutral(t *testing.T) {
	collector := &forwardOutcomeCollector{}
	client := newObservedForwardClient(t, collector, sseForwardResponse(""))
	stream, err := client.Open(context.Background(), observedForwardDispatch(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
	if len(collector.outcomes) != 0 {
		t.Fatalf("client close recorded upstream outcome: %#v", collector.outcomes)
	}
}

func TestForwardCredentialOnlyAuthorityRemainsBackwardCompatible(t *testing.T) {
	client, err := NewForward(ForwardConfig{
		Endpoint: testCanonicalForwardResponsesEndpoint,
		HTTPClient: &http.Client{Transport: forwardRoundTripFunc(sseForwardResponse(
			"data: {\"type\":\"response.completed\",\"response\":{}}\n\n",
		))},
		CredentialAuthority: forwardCredentialAuthorityFunc(func(context.Context, providers.DispatchRequest) (ForwardCredential, error) {
			return ForwardCredential{Authorization: "Bearer caller"}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := client.Open(context.Background(), observedForwardDispatch(t))
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if event, err := stream.Next(); err != nil || event.Type != protocol.EventDone {
		t.Fatalf("event=%#v err=%v", event, err)
	}
}

func newObservedForwardClient(t *testing.T, collector *forwardOutcomeCollector, roundTrip forwardRoundTripFunc) *ForwardClient {
	t.Helper()
	authority := forwardAttemptAuthorityFunc(func(context.Context, providers.DispatchRequest) (ForwardAttempt, error) {
		return ForwardAttempt{
			Credential: ForwardCredential{Authorization: "Bearer selected", ChatGPTAccountID: "acct-selected"},
			Observer: collector,
		}, nil
	})
	client, err := NewForward(ForwardConfig{
		Endpoint: testCanonicalForwardResponsesEndpoint,
		HTTPClient: &http.Client{Transport: roundTrip},
		CredentialAuthority: authority,
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func observedForwardDispatch(t *testing.T) providers.DispatchRequest {
	t.Helper()
	dispatch := canonicalRequest(t, `{"model":"openai/gpt-5.6","store":false,"input":"hi"}`, "gpt-5.6")
	dispatch.ForwardHeaders = providers.NewForwardHeaders(nil)
	return dispatch
}

func sseForwardResponse(body string) forwardRoundTripFunc {
	return func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{"Content-Type": []string{"text/event-stream"}},
			Body: io.NopCloser(strings.NewReader(body)),
		}, nil
	}
}
