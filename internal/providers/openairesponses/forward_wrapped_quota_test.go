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

func TestForwardWrappedQuota5xxRetriesAlternateAccount(t *testing.T) {
	first := &quotaForwardObserver{}
	second := &forwardOutcomeCollector{}
	retryCalls := 0
	authority := forwardAttemptAuthorityFunc(func(context.Context, providers.DispatchRequest) (ForwardAttempt, error) {
		return ForwardAttempt{
			Credential: ForwardCredential{Authorization: "Bearer first", ChatGPTAccountID: "chat-first"},
			Observer:   first,
			RetryQuota: func(context.Context) (ForwardAttempt, bool, error) {
				retryCalls++
				return ForwardAttempt{
					Credential: ForwardCredential{Authorization: "Bearer second", ChatGPTAccountID: "chat-second"},
					Observer:   second,
				}, true, nil
			},
		}, nil
	})

	calls := 0
	var authorizations []string
	client, err := NewForward(ForwardConfig{
		Endpoint: testCanonicalForwardResponsesEndpoint,
		HTTPClient: &http.Client{Transport: forwardRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			authorizations = append(authorizations, req.Header.Get("Authorization"))
			calls++
			if calls == 1 {
				return wrappedQuotaResponse(http.StatusBadGateway, `{"error":{"message":"The usage limit has been reached"}}`), nil
			}
			return successfulForwardResponse(), nil
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
	if calls != 2 || retryCalls != 1 {
		t.Fatalf("calls=%d retryCalls=%d", calls, retryCalls)
	}
	if len(authorizations) != 2 || authorizations[0] != "Bearer first" || authorizations[1] != "Bearer second" {
		t.Fatalf("authorizations=%#v", authorizations)
	}
	assertWrappedQuotaOutcome(t, first.outcomes)
	event, err := stream.Next()
	if err != nil || event.Type != protocol.EventDone {
		t.Fatalf("event=%#v err=%v", event, err)
	}
}

func TestForwardWrappedQuota5xxCoolsSoleAccountWithoutRewritingClientStatus(t *testing.T) {
	observer := &quotaForwardObserver{}
	retryCalls := 0
	calls := 0
	authority := forwardAttemptAuthorityFunc(func(context.Context, providers.DispatchRequest) (ForwardAttempt, error) {
		return ForwardAttempt{
			Credential: ForwardCredential{Authorization: "Bearer first"},
			Observer:   observer,
			RetryQuota: func(context.Context) (ForwardAttempt, bool, error) {
				retryCalls++
				return ForwardAttempt{}, false, nil
			},
		}, nil
	})
	client, err := NewForward(ForwardConfig{
		Endpoint: testCanonicalForwardResponsesEndpoint,
		HTTPClient: &http.Client{Transport: forwardRoundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			return wrappedQuotaResponse(http.StatusBadGateway, `{"error":{"message":"The usage limit has been reached"}}`), nil
		})},
		CredentialAuthority: authority,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.Open(context.Background(), observedForwardDispatch(t))
	if err == nil || !strings.Contains(err.Error(), "HTTP 502") {
		t.Fatalf("err=%v", err)
	}
	if calls != 1 || retryCalls != 1 {
		t.Fatalf("calls=%d retryCalls=%d", calls, retryCalls)
	}
	assertWrappedQuotaOutcome(t, observer.outcomes)
}

func TestForwardWrappedQuota5xxRetryRemainsOneShot(t *testing.T) {
	first := &quotaForwardObserver{}
	second := &quotaForwardObserver{}
	firstRetryCalls := 0
	secondRetryCalls := 0
	calls := 0
	authority := forwardAttemptAuthorityFunc(func(context.Context, providers.DispatchRequest) (ForwardAttempt, error) {
		return ForwardAttempt{
			Credential: ForwardCredential{Authorization: "Bearer first"},
			Observer:   first,
			RetryQuota: func(context.Context) (ForwardAttempt, bool, error) {
				firstRetryCalls++
				return ForwardAttempt{
					Credential: ForwardCredential{Authorization: "Bearer second"},
					Observer:   second,
					RetryQuota: func(context.Context) (ForwardAttempt, bool, error) {
						secondRetryCalls++
						return ForwardAttempt{Credential: ForwardCredential{Authorization: "Bearer third"}}, true, nil
					},
				}, true, nil
			},
		}, nil
	})
	client, err := NewForward(ForwardConfig{
		Endpoint: testCanonicalForwardResponsesEndpoint,
		HTTPClient: &http.Client{Transport: forwardRoundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			return wrappedQuotaResponse(http.StatusBadGateway, `{"error":{"message":"The usage limit has been reached"}}`), nil
		})},
		CredentialAuthority: authority,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.Open(context.Background(), observedForwardDispatch(t))
	if err == nil || !strings.Contains(err.Error(), "HTTP 502") {
		t.Fatalf("err=%v", err)
	}
	if calls != 2 || firstRetryCalls != 1 || secondRetryCalls != 0 {
		t.Fatalf("calls=%d firstRetryCalls=%d secondRetryCalls=%d", calls, firstRetryCalls, secondRetryCalls)
	}
	assertWrappedQuotaOutcome(t, first.outcomes)
	assertWrappedQuotaOutcome(t, second.outcomes)
}

func TestForwardWrappedQuota5xxRecognizesBoundedMessageShapes(t *testing.T) {
	bodies := []struct {
		name string
		body string
	}{
		{name: "error message", body: `{"error":{"message":"The usage limit has been reached"}}`},
		{name: "last error message", body: `{"last_error":{"message":"quota exhausted"}}`},
		{name: "response error message", body: `{"response":{"error":{"message":"exceeded your current quota"}}}`},
		{name: "response incomplete message", body: `{"response":{"incomplete_details":{"message":"account quota exceeded"}}}`},
		{name: "top level message", body: `{"message":"monthly quota exceeded"}`},
		{name: "string error", body: `{"error":"daily quota exceeded"}`},
		{name: "json string", body: `"The usage limit has been reached"`},
		{name: "plain text", body: `The usage limit has been reached`},
	}
	statuses := []int{http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout}
	for _, tc := range bodies {
		for _, status := range statuses {
			t.Run(tc.name+"/"+http.StatusText(status), func(t *testing.T) {
				retryCalls, calls, err := openForwardWrappedQuotaFixture(t, status, tc.body)
				if err != nil {
					t.Fatalf("expected wrapped quota recovery, got %v", err)
				}
				if retryCalls != 1 || calls != 2 {
					t.Fatalf("retryCalls=%d calls=%d", retryCalls, calls)
				}
			})
		}
	}
}

func TestForwardWrappedQuota5xxRejectsUntrustedOrUnboundedMatches(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
	}{
		{
			name:   "quota words only in echoed request",
			status: http.StatusBadGateway,
			body:   `{"error":{"message":"upstream server error"},"request":{"input":"explain the usage limit"}}`,
		},
		{name: "malformed json", status: http.StatusBadGateway, body: `{"error":{"message":"usage limit"}`},
		{name: "non 5xx", status: http.StatusBadRequest, body: `{"error":{"message":"The usage limit has been reached"}}`},
		{name: "invalid utf8", status: http.StatusBadGateway, body: string([]byte{0xff}) + " usage limit"},
		{name: "oversized body", status: http.StatusBadGateway, body: strings.Repeat("x", forwardModel400BodyMaxBytes+1) + " usage limit"},
		{name: "ambiguous rate limit wording", status: http.StatusBadGateway, body: `{"error":{"message":"rate limit subsystem unavailable"}}`},
	}
	for _, status := range []int{http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout} {
		cases = append(cases, struct {
			name   string
			status int
			body   string
		}{name: "ordinary " + http.StatusText(status), status: status, body: `{"error":{"message":"upstream server error"}}`})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			retryCalls, calls, err := openForwardWrappedQuotaFixture(t, tc.status, tc.body)
			if err == nil {
				t.Fatal("expected original upstream failure")
			}
			if retryCalls != 0 || calls != 1 {
				t.Fatalf("retryCalls=%d calls=%d", retryCalls, calls)
			}
		})
	}
}

func TestForwardWrappedQuota5xxCallerCancellationWinsDuringBodyRead(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	observer := &forwardOutcomeCollector{}
	retryCalls := 0
	authority := forwardAttemptAuthorityFunc(func(context.Context, providers.DispatchRequest) (ForwardAttempt, error) {
		return ForwardAttempt{
			Credential: ForwardCredential{Authorization: "Bearer first"},
			Observer:   observer,
			RetryQuota: func(context.Context) (ForwardAttempt, bool, error) {
				retryCalls++
				return ForwardAttempt{}, false, nil
			},
		}, nil
	})
	client, err := NewForward(ForwardConfig{
		Endpoint: testCanonicalForwardResponsesEndpoint,
		HTTPClient: &http.Client{Transport: forwardRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusBadGateway,
				Header:     make(http.Header),
				Body: &cancelOnReadBody{
					cancel: cancel,
					reader: strings.NewReader(`{"error":{"message":"The usage limit has been reached"}}`),
				},
			}, nil
		})},
		CredentialAuthority: authority,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.Open(ctx, observedForwardDispatch(t))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	if retryCalls != 0 {
		t.Fatalf("retryCalls=%d", retryCalls)
	}
	if len(observer.outcomes) != 0 {
		t.Fatalf("cancellation recorded provider outcome=%#v", observer.outcomes)
	}
}

func openForwardWrappedQuotaFixture(t *testing.T, status int, body string) (retryCalls, calls int, openErr error) {
	t.Helper()
	authority := forwardAttemptAuthorityFunc(func(context.Context, providers.DispatchRequest) (ForwardAttempt, error) {
		return ForwardAttempt{
			Credential: ForwardCredential{Authorization: "Bearer first"},
			RetryQuota: func(context.Context) (ForwardAttempt, bool, error) {
				retryCalls++
				return ForwardAttempt{Credential: ForwardCredential{Authorization: "Bearer second"}}, true, nil
			},
		}, nil
	})
	client, err := NewForward(ForwardConfig{
		Endpoint: testCanonicalForwardResponsesEndpoint,
		HTTPClient: &http.Client{Transport: forwardRoundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				return wrappedQuotaResponse(status, body), nil
			}
			return successfulForwardResponse(), nil
		})},
		CredentialAuthority: authority,
	})
	if err != nil {
		t.Fatal(err)
	}
	stream, openErr := client.Open(context.Background(), observedForwardDispatch(t))
	if stream != nil {
		_ = stream.Close()
	}
	return retryCalls, calls, openErr
}

func wrappedQuotaResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func successfulForwardResponse() *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader("data: {\"type\":\"response.completed\",\"response\":{}}\n\n")),
	}
}

func assertWrappedQuotaOutcome(t *testing.T, outcomes []ForwardOutcome) {
	t.Helper()
	if len(outcomes) != 1 || outcomes[0].Kind != ForwardOutcomeHTTP || outcomes[0].StatusCode != http.StatusTooManyRequests {
		t.Fatalf("wrapped quota outcome=%#v", outcomes)
	}
}

type cancelOnReadBody struct {
	cancel    context.CancelFunc
	reader    *strings.Reader
	cancelled bool
}

func (b *cancelOnReadBody) Read(buffer []byte) (int, error) {
	if !b.cancelled {
		b.cancelled = true
		b.cancel()
	}
	return b.reader.Read(buffer)
}

func (*cancelOnReadBody) Close() error { return nil }
