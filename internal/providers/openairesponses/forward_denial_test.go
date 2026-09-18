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

func TestClassifyForward403DenialExactStructuredCodes(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want ForwardDenial
	}{
		{name: "workspace root", body: "{\"code\":\"codex_workspace_access_denied\"}", want: ForwardDenialWorkspace},
		{name: "workspace nested", body: "{\"error\":{\"code\":\"workspace_access_denied\"}}", want: ForwardDenialWorkspace},
		{name: "workspace detail", body: "{\"detail\":{\"code\":\"codex_workspace_access_denied\"}}", want: ForwardDenialWorkspace},
		{name: "workspace selected detail", body: "{\"detail\":{\"code\":\"invalid_workspace_selected\"}}", want: ForwardDenialWorkspace},

		{name: "entitlement detail", body: "{\"detail\":{\"code\":\"entitlement_missing\"}}", want: ForwardDenialEntitlement},
		{name: "detail array", body: "{\"detail\":[{\"code\":\"codex_workspace_access_denied\"}]}", want: ""},
		{name: "detail non-string", body: "{\"detail\":{\"code\":403}}", want: ""},
		{name: "entitlement root", body: "{\"code\":\"codex_entitlement_missing\"}", want: ForwardDenialEntitlement},
		{name: "entitlement nested", body: "{\"error\":{\"code\":\"entitlement_missing\"}}", want: ForwardDenialEntitlement},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyForward403Denial(context.Background(), io.NopCloser(strings.NewReader(tc.body)))
			if got != tc.want {
				t.Fatalf("denial=%q want=%q", got, tc.want)
			}
		})
	}
}

func TestClassifyForward403DenialFailsClosedOnUntrustedBodies(t *testing.T) {
	oversized := "{\"code\":\"codex_workspace_access_denied\",\"padding\":\"" + strings.Repeat("x", forwardDenialBodyMaxBytes) + "\"}"
	for _, tc := range []struct {
		name string
		body []byte
	}{
		{name: "unknown", body: []byte("{\"code\":\"something_else\"}")},
		{name: "message only", body: []byte("{\"message\":\"workspace_access_denied\"}")},
		{name: "non string", body: []byte("{\"code\":123}")},
		{name: "error not object", body: []byte("{\"error\":\"workspace_access_denied\"}")},
		{name: "malformed", body: []byte("{\"code\":")},
		{name: "trailing document", body: []byte("{\"code\":\"workspace_access_denied\"} {}")},
		{name: "duplicate root", body: []byte("{\"code\":\"workspace_access_denied\",\"code\":\"other\"}")},
		{name: "duplicate nested unrelated", body: []byte("{\"error\":{\"code\":\"workspace_access_denied\"},\"meta\":{\"x\":1,\"x\":2}}")},
		{name: "invalid utf8", body: append([]byte("{\"code\":\"workspace_access_denied\",\"x\":\""), 0xff, '"', '}')},
		{name: "oversized", body: []byte(oversized)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyForward403Denial(context.Background(), io.NopCloser(strings.NewReader(string(tc.body)))); got != "" {
				t.Fatalf("denial=%q", got)
			}
		})
	}
}

func TestForward403OutcomeCarriesDenialAndPoolMapsIt(t *testing.T) {
	collector := &forwardOutcomeCollector{}
	authority := forwardAttemptAuthorityFunc(func(context.Context, providers.DispatchRequest) (ForwardAttempt, error) {
		return ForwardAttempt{Credential: ForwardCredential{Authorization: "Bearer selected"}, Observer: collector}, nil
	})
	client, err := NewForward(ForwardConfig{
		Endpoint: testCanonicalForwardResponsesEndpoint,
		HTTPClient: &http.Client{Transport: forwardRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusForbidden,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader("{\"error\":{\"code\":\"codex_workspace_access_denied\"}}")),
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
	if len(collector.outcomes) != 1 || collector.outcomes[0].Denial != ForwardDenialWorkspace {
		t.Fatalf("outcomes=%#v", collector.outcomes)
	}

	outcomes := &fakePoolOutcomeRecorder{}
	observer := newCodexPoolAttemptObserver(outcomes, codexauth.PoolCredential{AccountID: "acct"})
	observer.Observe(collector.outcomes[0])
	if len(outcomes.calls) != 1 || outcomes.calls[0].meta.Denial != codexauth.DenialWorkspace {
		t.Fatalf("calls=%#v", outcomes.calls)
	}
}

func TestForward403UnknownBodyRemainsCredentialFailureEvidence(t *testing.T) {
	outcomes := &fakePoolOutcomeRecorder{}
	observer := newCodexPoolAttemptObserver(outcomes, codexauth.PoolCredential{AccountID: "acct"})
	observer.Observe(ForwardOutcome{Kind: ForwardOutcomeHTTP, StatusCode: http.StatusForbidden})
	if len(outcomes.calls) != 1 || outcomes.calls[0].meta.Denial != "" {
		t.Fatalf("calls=%#v", outcomes.calls)
	}
	if got := codexauth.ClassifyUpstreamOutcome(outcomes.calls[0].outcome, outcomes.calls[0].meta.Denial); got != codexauth.OutcomeCredential {
		t.Fatalf("class=%q", got)
	}
}
