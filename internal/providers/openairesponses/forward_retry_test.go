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

func TestForwardQuotaRetrySelectsAlternateBeforeApplyingRejectedQuotaOrOutcomeAndReplaysPreparedBody(t *testing.T) {
	first := &quotaForwardObserver{}
	second := &forwardOutcomeCollector{}
	retryCalls := 0
	authority := forwardAttemptAuthorityFunc(func(context.Context, providers.DispatchRequest) (ForwardAttempt, error) {
		return ForwardAttempt{
			Credential: ForwardCredential{Authorization: "Bearer first", ChatGPTAccountID: "chat-first"},
			Observer:   first,
			RetryQuota: func(context.Context) (ForwardAttempt, bool, error) {
				retryCalls++
				if len(first.quotas) != 0 || len(first.outcomes) != 0 || len(first.order) != 0 {
					t.Fatalf("rejected response mutated Pool state before alternate selection: quotas=%#v outcomes=%#v order=%#v", first.quotas, first.outcomes, first.order)
				}
				return ForwardAttempt{
					Credential: ForwardCredential{Authorization: "Bearer second", ChatGPTAccountID: "chat-second"},
					Observer:   second,
				}, true, nil
			},
		}, nil
	})

	var calls int
	var authorizations []string
	var bodies []string
	client, err := NewForward(ForwardConfig{
		Endpoint: testCanonicalForwardResponsesEndpoint,
		HTTPClient: &http.Client{Transport: forwardRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			authorizations = append(authorizations, req.Header.Get("Authorization"))
			body, readErr := io.ReadAll(req.Body)
			if readErr != nil {
				t.Fatal(readErr)
			}
			bodies = append(bodies, string(body))
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
			if len(first.quotas) != 1 || first.quotas[0].PrimaryUsedPercent != "100" {
				t.Fatalf("alternate sent before first quota observation: %#v", first.quotas)
			}
			if len(first.outcomes) != 1 || first.outcomes[0].Kind != ForwardOutcomeHTTP || first.outcomes[0].StatusCode != http.StatusTooManyRequests {
				t.Fatalf("alternate sent before first outcome was recorded: %#v", first.outcomes)
			}
			if len(first.order) != 2 || first.order[0] != "quota" || first.order[1] != "outcome" {
				t.Fatalf("first response observation order=%#v", first.order)
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
	if calls != 2 || retryCalls != 1 {
		t.Fatalf("calls=%d retryCalls=%d", calls, retryCalls)
	}
	if len(authorizations) != 2 || authorizations[0] != "Bearer first" || authorizations[1] != "Bearer second" {
		t.Fatalf("authorizations=%#v", authorizations)
	}
	if len(bodies) != 2 || bodies[0] == "" || bodies[0] != bodies[1] {
		t.Fatalf("prepared body was not replayed exactly: %#v", bodies)
	}
	if len(second.outcomes) != 0 {
		t.Fatalf("retry success recorded before terminal SSE event: %#v", second.outcomes)
	}
	event, err := stream.Next()
	if err != nil || event.Type != protocol.EventDone {
		t.Fatalf("event=%#v err=%v", event, err)
	}
	if len(second.outcomes) != 1 || second.outcomes[0].Kind != ForwardOutcomeCompleted {
		t.Fatalf("retry outcomes=%#v", second.outcomes)
	}
}

func TestForwardQuotaRetryUnexpectedResolutionFailureDoesNotRecordRejectedState(t *testing.T) {
	first := &quotaForwardObserver{}
	unexpected := errors.New("unexpected retry resolver failure")
	authority := forwardAttemptAuthorityFunc(func(context.Context, providers.DispatchRequest) (ForwardAttempt, error) {
		return ForwardAttempt{
			Credential: ForwardCredential{Authorization: "Bearer first"},
			Observer:   first,
			RetryQuota: func(context.Context) (ForwardAttempt, bool, error) {
				return ForwardAttempt{}, false, unexpected
			},
		}, nil
	})
	client, err := NewForward(ForwardConfig{
		Endpoint: testCanonicalForwardResponsesEndpoint,
		HTTPClient: &http.Client{Transport: forwardRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Header: http.Header{
					"Retry-After":                  []string{"60"},
					"X-Codex-Primary-Used-Percent": []string{"100"},
				},
				Body: io.NopCloser(strings.NewReader(`{"error":"quota"}`)),
			}, nil
		})},
		CredentialAuthority: authority,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, gotErr := client.Open(context.Background(), observedForwardDispatch(t))
	if !errors.Is(gotErr, unexpected) {
		t.Fatalf("err=%v", gotErr)
	}
	if len(first.quotas) != 0 || len(first.outcomes) != 0 || len(first.order) != 0 {
		t.Fatalf("unexpected retry failure mutated rejected state: quotas=%#v outcomes=%#v order=%#v", first.quotas, first.outcomes, first.order)
	}
}

func TestForwardQuotaRetryIsBoundedToOneAlternateAttempt(t *testing.T) {
	first := &forwardOutcomeCollector{}
	second := &forwardOutcomeCollector{}
	firstRetryCalls := 0
	secondRetryCalls := 0
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
						return ForwardAttempt{}, false, nil
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

	if _, err := client.Open(context.Background(), observedForwardDispatch(t)); err == nil {
		t.Fatal("expected second quota rejection")
	}
	if calls != 2 || firstRetryCalls != 1 || secondRetryCalls != 0 {
		t.Fatalf("calls=%d firstRetryCalls=%d secondRetryCalls=%d", calls, firstRetryCalls, secondRetryCalls)
	}
	if len(first.outcomes) != 1 || len(second.outcomes) != 1 {
		t.Fatalf("first=%#v second=%#v", first.outcomes, second.outcomes)
	}
}
