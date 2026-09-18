package openairesponses

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/providers"
)

const testCanonicalForwardResponsesEndpoint = "https://chatgpt.com/backend-api/codex/responses"

type forwardRoundTripFunc func(*http.Request) (*http.Response, error)

func (f forwardRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type forwardCredentialAuthorityFunc func(context.Context, providers.DispatchRequest) (ForwardCredential, error)

func (f forwardCredentialAuthorityFunc) Resolve(ctx context.Context, dispatch providers.DispatchRequest) (ForwardCredential, error) {
	return f(ctx, dispatch)
}

func forwardSSEClient(t *testing.T, inspect func(*http.Request)) *http.Client {
	t.Helper()
	return &http.Client{Transport: forwardRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		if inspect != nil {
			inspect(request)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body: io.NopCloser(strings.NewReader(
				"data: {\"type\":\"response.completed\",\"response\":{}}\n\n",
			)),
		}, nil
	})}
}

func TestNewForwardRequiresCanonicalEndpoint(t *testing.T) {
	for _, endpoint := range []string{"", testCanonicalForwardResponsesEndpoint, testCanonicalForwardResponsesEndpoint + "/"} {
		client, err := NewForward(ForwardConfig{Endpoint: endpoint, HTTPClient: forwardSSEClient(t, nil)})
		if err != nil || client == nil {
			t.Fatalf("endpoint=%q client=%#v err=%v", endpoint, client, err)
		}
	}

	for _, endpoint := range []string{
		"http://chatgpt.com/backend-api/codex/responses",
		"https://example.com/responses",
		"https://chatgpt.com/backend-api/codex/other",
		"https://chatgpt.com/backend-api/codex/responses?target=other",
		"https://user@chatgpt.com/backend-api/codex/responses",
	} {
		if _, err := NewForward(ForwardConfig{Endpoint: endpoint, HTTPClient: forwardSSEClient(t, nil)}); err == nil {
			t.Fatalf("forward auth accepted endpoint %q", endpoint)
		}
	}
}

func TestForwardCallerAuthRelaysOnlyDispatchAuthority(t *testing.T) {
	var reached bool
	client, err := NewForward(ForwardConfig{
		Endpoint: testCanonicalForwardResponsesEndpoint,
		HTTPClient: forwardSSEClient(t, func(request *http.Request) {
			reached = true
			if request.URL.String() != testCanonicalForwardResponsesEndpoint {
				t.Errorf("url=%q", request.URL.String())
			}
			want := map[string]string{
				"Authorization":                         "Bearer caller-oauth",
				"ChatGPT-Account-Id":                    "acct_123",
				"OpenAI-Beta":                           "responses=experimental",
				"X-Codex-Turn-State":                    "turn-state",
				"X-Oai-Attestation":                     "attestation",
				"X-Responsesapi-Include-Timing-Metrics": "1",
			}
			for name, value := range want {
				if got := request.Header.Get(name); got != value {
					t.Errorf("%s=%q want=%q", name, got, value)
				}
			}
			for _, name := range []string{"Cookie", "X-Api-Key", "X-Benes-API-Key", "X-Arbitrary"} {
				if got := request.Header.Get(name); got != "" {
					t.Errorf("disallowed %s=%q", name, got)
				}
			}
			if request.Header.Get("Content-Type") != "application/json" || request.Header.Get("Accept") != "text/event-stream" {
				t.Errorf("headers=%v", request.Header)
			}
		}),
		MaxStreamBytes:    1 << 20,
		InactivityTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}

	dispatch := canonicalRequest(t, `{"model":"openai/gpt-5.6","store":false,"input":"hi"}`, "gpt-5.6")
	dispatch.ForwardHeaders = providers.NewForwardHeaders(map[string]string{
		"authorization":                         "Bearer caller-oauth",
		"chatgpt-account-id":                    "acct_123",
		"openai-beta":                           "responses=experimental",
		"x-codex-turn-state":                    "turn-state",
		"x-oai-attestation":                     "attestation",
		"x-responsesapi-include-timing-metrics": "1",
		"cookie":                                "session=secret",
		"x-api-key":                             "provider-key",
		"x-benes-api-key":                   "proxy-key",
		"x-arbitrary":                           "nope",
	})
	stream, err := client.Open(context.Background(), dispatch)
	if err != nil {
		t.Fatalf("Open(): %v", err)
	}
	defer stream.Close()
	if _, err := stream.Next(); err != nil {
		t.Fatalf("Next(): %v", err)
	}
	if !reached {
		t.Fatal("forward client did not reach transport")
	}
}

func TestForwardCallerAuthRejectsBlockedOrMissingAuthorizationBeforeNetwork(t *testing.T) {
	for _, tc := range []struct {
		name    string
		headers providers.ForwardHeaders
		want    error
	}{
		{
			name: "blocked proxy admission bearer",
			headers: providers.NewForwardHeadersWithBlockedAuthorization(map[string]string{
				"authorization": "Bearer local-proxy-secret",
			}, true),
			want: ErrForwardAdmissionCredential,
		},
		{
			name:    "missing caller authorization",
			headers: providers.NewForwardHeaders(map[string]string{"chatgpt-account-id": "acct_123"}),
			want:    ErrForwardAuthorizationRequired,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reached := false
			client, err := NewForward(ForwardConfig{
				Endpoint:   testCanonicalForwardResponsesEndpoint,
				HTTPClient: forwardSSEClient(t, func(*http.Request) { reached = true }),
			})
			if err != nil {
				t.Fatal(err)
			}
			dispatch := canonicalRequest(t, `{"model":"openai/gpt-5.6","store":false}`, "gpt-5.6")
			dispatch.ForwardHeaders = tc.headers
			if _, err := client.Open(context.Background(), dispatch); !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want=%v", err, tc.want)
			}
			if reached {
				t.Fatal("invalid forward authorization reached network")
			}
		})
	}
}

func TestKeyModeIgnoresCallerForwardAuthorization(t *testing.T) {
	var gotAuthorization string
	client, err := New(Config{
		Endpoint:   "https://api.openai.com/v1/responses",
		APIKey:     "provider-key",
		HTTPClient: forwardSSEClient(t, func(request *http.Request) { gotAuthorization = request.Header.Get("Authorization") }),
	})
	if err != nil {
		t.Fatal(err)
	}
	dispatch := canonicalRequest(t, `{"model":"openai-apikey/gpt-5.6","store":false}`, "gpt-5.6")
	dispatch.ForwardHeaders = providers.NewForwardHeaders(map[string]string{"authorization": "Bearer caller-oauth"})
	stream, err := client.Open(context.Background(), dispatch)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if _, err := stream.Next(); err != nil {
		t.Fatal(err)
	}
	if gotAuthorization != "Bearer provider-key" {
		t.Fatalf("Authorization=%q", gotAuthorization)
	}
}

func TestCallerForwardAuthorityResolvesExistingDispatchCredential(t *testing.T) {
	authority := CallerForwardAuthority{}
	dispatch := providers.DispatchRequest{ForwardHeaders: providers.NewForwardHeaders(map[string]string{
		"authorization":      "Bearer caller-oauth",
		"chatgpt-account-id": "acct_123",
	})}

	credential, err := authority.Resolve(context.Background(), dispatch)
	if err != nil {
		t.Fatal(err)
	}
	if credential.Authorization != "Bearer caller-oauth" || credential.ChatGPTAccountID != "acct_123" {
		t.Fatalf("credential=%#v", credential)
	}
}

func TestCallerForwardAuthorityRejectsBlockedOrMissingAuthorization(t *testing.T) {
	authority := CallerForwardAuthority{}
	for _, tc := range []struct {
		name    string
		headers providers.ForwardHeaders
		want    error
	}{
		{
			name: "blocked proxy admission bearer",
			headers: providers.NewForwardHeadersWithBlockedAuthorization(map[string]string{
				"authorization": "Bearer local-proxy-secret",
			}, true),
			want: ErrForwardAdmissionCredential,
		},
		{
			name:    "missing caller authorization",
			headers: providers.NewForwardHeaders(map[string]string{"chatgpt-account-id": "acct_123"}),
			want:    ErrForwardAuthorizationRequired,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := authority.Resolve(context.Background(), providers.DispatchRequest{ForwardHeaders: tc.headers})
			if !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want=%v", err, tc.want)
			}
		})
	}
}

func TestForwardCustomAuthorityOverridesCallerCredentialsAndPreservesMetadata(t *testing.T) {
	client, err := NewForward(ForwardConfig{
		Endpoint: testCanonicalForwardResponsesEndpoint,
		CredentialAuthority: forwardCredentialAuthorityFunc(func(_ context.Context, dispatch providers.DispatchRequest) (ForwardCredential, error) {
			if got := dispatch.ForwardHeaders.Get("authorization"); got != "Bearer caller-oauth" {
				t.Fatalf("resolver authorization=%q", got)
			}
			return ForwardCredential{Authorization: "Bearer pool-selected", ChatGPTAccountID: "acct_pool"}, nil
		}),
		HTTPClient: forwardSSEClient(t, func(request *http.Request) {
			if got := request.Header.Get("Authorization"); got != "Bearer pool-selected" {
				t.Errorf("Authorization=%q", got)
			}
			if got := request.Header.Get("ChatGPT-Account-Id"); got != "acct_pool" {
				t.Errorf("ChatGPT-Account-Id=%q", got)
			}
			if got := request.Header.Get("X-Codex-Turn-State"); got != "turn-state" {
				t.Errorf("X-Codex-Turn-State=%q", got)
			}
			if got := request.Header.Get("X-Oai-Attestation"); got != "attestation" {
				t.Errorf("X-Oai-Attestation=%q", got)
			}
			if got := request.Header.Get("X-Arbitrary"); got != "" {
				t.Errorf("X-Arbitrary=%q", got)
			}
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	dispatch := canonicalRequest(t, `{"model":"openai/gpt-5.6","store":false}`, "gpt-5.6")
	dispatch.ForwardHeaders = providers.NewForwardHeaders(map[string]string{
		"authorization":      "Bearer caller-oauth",
		"chatgpt-account-id": "acct_caller",
		"x-codex-turn-state": "turn-state",
		"x-oai-attestation":  "attestation",
		"x-arbitrary":        "nope",
	})
	stream, err := client.Open(context.Background(), dispatch)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
}

func TestForwardCustomAuthorityCanOmitCallerAccountID(t *testing.T) {
	client, err := NewForward(ForwardConfig{
		Endpoint: testCanonicalForwardResponsesEndpoint,
		CredentialAuthority: forwardCredentialAuthorityFunc(func(context.Context, providers.DispatchRequest) (ForwardCredential, error) {
			return ForwardCredential{Authorization: "Bearer pool-selected"}, nil
		}),
		HTTPClient: forwardSSEClient(t, func(request *http.Request) {
			if got := request.Header.Get("ChatGPT-Account-Id"); got != "" {
				t.Errorf("ChatGPT-Account-Id leaked caller authority: %q", got)
			}
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	dispatch := canonicalRequest(t, `{"model":"openai/gpt-5.6","store":false}`, "gpt-5.6")
	dispatch.ForwardHeaders = providers.NewForwardHeaders(map[string]string{
		"authorization":      "Bearer caller-oauth",
		"chatgpt-account-id": "acct_caller",
	})
	stream, err := client.Open(context.Background(), dispatch)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
}

func TestForwardCredentialAuthorityFailuresStayBeforeNetwork(t *testing.T) {
	resolverErr := errors.New("credential unavailable")
	for _, tc := range []struct {
		name      string
		ctx       func() context.Context
		authority ForwardCredentialAuthority
		want      error
	}{
		{
			name: "resolver error",
			ctx:  context.Background,
			authority: forwardCredentialAuthorityFunc(func(context.Context, providers.DispatchRequest) (ForwardCredential, error) {
				return ForwardCredential{}, resolverErr
			}),
			want: resolverErr,
		},
		{
			name: "cancelled context reaches resolver",
			ctx: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			},
			authority: forwardCredentialAuthorityFunc(func(ctx context.Context, _ providers.DispatchRequest) (ForwardCredential, error) {
				if !errors.Is(ctx.Err(), context.Canceled) {
					t.Fatalf("resolver ctx err=%v", ctx.Err())
				}
				return ForwardCredential{}, ctx.Err()
			}),
			want: context.Canceled,
		},
		{
			name: "blank resolved authorization",
			ctx:  context.Background,
			authority: forwardCredentialAuthorityFunc(func(context.Context, providers.DispatchRequest) (ForwardCredential, error) {
				return ForwardCredential{ChatGPTAccountID: "acct_pool"}, nil
			}),
			want: ErrForwardAuthorizationRequired,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reached := false
			client, err := NewForward(ForwardConfig{
				Endpoint:            testCanonicalForwardResponsesEndpoint,
				CredentialAuthority: tc.authority,
				HTTPClient:          forwardSSEClient(t, func(*http.Request) { reached = true }),
			})
			if err != nil {
				t.Fatal(err)
			}
			dispatch := canonicalRequest(t, `{"model":"openai/gpt-5.6","store":false}`, "gpt-5.6")
			if _, err := client.Open(tc.ctx(), dispatch); !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want=%v", err, tc.want)
			}
			if reached {
				t.Fatal("credential authority failure reached network")
			}
		})
	}
}
