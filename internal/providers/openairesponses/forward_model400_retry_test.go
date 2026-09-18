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

const exactModel400Body = "{\"detail\":\"The 'gpt-5.6' model is not supported when using Codex with a ChatGPT account.\"}"

func TestAllowListedCodexAccountModel400MatchesOnlyCapturedEnvelope(t *testing.T) {
	modelID := "gpt-5.6"
	accepted := []string{
		exactModel400Body,
		"{\"detail\":\"  THE 'GPT-5.6'   MODEL IS NOT SUPPORTED WHEN USING CODEX WITH A CHATGPT ACCOUNT.  \"}",
		"{\"detail\":\"The 'gpt-5.6'\\tmodel is not supported when using Codex with a ChatGPT account.\"}",
	}
	for _, body := range accepted {
		if !isAllowListedCodexAccountModel400(http.StatusBadRequest, []byte(body), modelID) {
			t.Fatalf("expected allow-list match for %s", body)
		}
	}

	rejected := []struct {
		name   string
		status int
		body   string
	}{
		{name: "wrong status", status: http.StatusNotFound, body: exactModel400Body},
		{name: "generic", status: http.StatusBadRequest, body: "{\"detail\":\"unsupported model\"}"},
		{name: "wrong model", status: http.StatusBadRequest, body: "{\"detail\":\"The 'other-model' model is not supported when using Codex with a ChatGPT account.\"}"},
		{name: "nested detail", status: http.StatusBadRequest, body: "{\"error\":{\"detail\":\"The 'gpt-5.6' model is not supported when using Codex with a ChatGPT account.\"}}"},
		{name: "non string", status: http.StatusBadRequest, body: "{\"detail\":400}"},
		{name: "array", status: http.StatusBadRequest, body: "[{\"detail\":\"The 'gpt-5.6' model is not supported when using Codex with a ChatGPT account.\"}]"},
		{name: "malformed", status: http.StatusBadRequest, body: "{\"detail\":"},
	}
	for _, tc := range rejected {
		t.Run(tc.name, func(t *testing.T) {
			if isAllowListedCodexAccountModel400(tc.status, []byte(tc.body), modelID) {
				t.Fatalf("unexpected allow-list match for %s", tc.body)
			}
		})
	}
}

func TestForwardModel400RetrySelectsAlternateBeforeRejectedStateAndIsOneShot(t *testing.T) {
	first := &quotaForwardObserver{}
	second := &forwardOutcomeCollector{}
	retryCalls := 0
	secondRetryCalls := 0
	authority := forwardAttemptAuthorityFunc(func(context.Context, providers.DispatchRequest) (ForwardAttempt, error) {
		return ForwardAttempt{
			Credential: ForwardCredential{Authorization: "Bearer first", ChatGPTAccountID: "chat-first"},
			Observer:   first,
			RetryModel400: func(context.Context) (ForwardAttempt, bool, error) {
				retryCalls++
				if len(first.quotas) != 0 || len(first.outcomes) != 0 || len(first.order) != 0 {
					t.Fatalf("model-400 mutated state before alternate selection: quotas=%#v outcomes=%#v order=%#v", first.quotas, first.outcomes, first.order)
				}
				return ForwardAttempt{
					Credential: ForwardCredential{Authorization: "Bearer second", ChatGPTAccountID: "chat-second"},
					Observer:   second,
					RetryModel400: func(context.Context) (ForwardAttempt, bool, error) {
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
		HTTPClient: &http.Client{Transport: forwardRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				if req.Header.Get("Authorization") != "Bearer first" {
					t.Fatalf("first authorization=%q", req.Header.Get("Authorization"))
				}
				return &http.Response{
					StatusCode: http.StatusBadRequest,
					Header:     http.Header{"X-Codex-Primary-Used-Percent": []string{"77"}},
					Body:       io.NopCloser(strings.NewReader(exactModel400Body)),
				}, nil
			}
			if req.Header.Get("Authorization") != "Bearer second" {
				t.Fatalf("second authorization=%q", req.Header.Get("Authorization"))
			}
			if len(first.quotas) != 0 {
				t.Fatalf("retryable model-400 quota should not be published before alternate send: %#v", first.quotas)
			}
			if len(first.outcomes) != 1 || first.outcomes[0].Kind != ForwardOutcomeHTTP || first.outcomes[0].StatusCode != http.StatusBadRequest {
				t.Fatalf("first outcomes=%#v", first.outcomes)
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
	dispatch.Parsed.ModelID = "friendly-alias"
	dispatch.Parsed.UpstreamModelID = "gpt-5.6"
	stream, err := client.Open(context.Background(), dispatch)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if calls != 2 || retryCalls != 1 || secondRetryCalls != 0 {
		t.Fatalf("calls=%d retryCalls=%d secondRetryCalls=%d", calls, retryCalls, secondRetryCalls)
	}
	event, err := stream.Next()
	if err != nil || event.Type != protocol.EventDone {
		t.Fatalf("event=%#v err=%v", event, err)
	}
	if len(second.outcomes) != 1 || second.outcomes[0].Kind != ForwardOutcomeCompleted {
		t.Fatalf("second outcomes=%#v", second.outcomes)
	}
}

func TestForwardModel400RetryRejectsUnsafeOrNonMatchingBodies(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "wrong model", body: "{\"detail\":\"The 'other-model' model is not supported when using Codex with a ChatGPT account.\"}"},
		{name: "generic", body: "{\"detail\":\"unsupported model\"}"},
		{name: "oversized", body: "{\"detail\":\"" + strings.Repeat(" ", forwardModel400BodyMaxBytes) + "The 'gpt-5.6' model is not supported when using Codex with a ChatGPT account.\"}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			observer := &quotaForwardObserver{}
			retryCalls := 0
			authority := forwardAttemptAuthorityFunc(func(context.Context, providers.DispatchRequest) (ForwardAttempt, error) {
				return ForwardAttempt{
					Credential: ForwardCredential{Authorization: "Bearer first"},
					Observer:   observer,
					RetryModel400: func(context.Context) (ForwardAttempt, bool, error) {
						retryCalls++
						return ForwardAttempt{}, false, nil
					},
				}, nil
			})
			client, err := NewForward(ForwardConfig{
				Endpoint: testCanonicalForwardResponsesEndpoint,
				HTTPClient: &http.Client{Transport: forwardRoundTripFunc(func(*http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusBadRequest,
						Header:     http.Header{"X-Codex-Primary-Used-Percent": []string{"33"}},
						Body:       io.NopCloser(strings.NewReader(tc.body)),
					}, nil
				})},
				CredentialAuthority: authority,
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.Open(context.Background(), observedForwardDispatch(t)); err == nil {
				t.Fatal("expected original 400")
			}
			if retryCalls != 0 {
				t.Fatalf("retryCalls=%d", retryCalls)
			}
			if len(observer.quotas) != 1 || observer.quotas[0].PrimaryUsedPercent != "33" || len(observer.outcomes) != 1 {
				t.Fatalf("observer=%#v", observer)
			}
		})
	}
}
