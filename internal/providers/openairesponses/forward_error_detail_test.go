package openairesponses

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/providers"
)

func TestForwardOpenIncludesSanitizedHTTP400Detail(t *testing.T) {
	client, err := NewForward(ForwardConfig{
		Endpoint: testCanonicalForwardResponsesEndpoint,
		CredentialAuthority: forwardCredentialAuthorityFunc(func(context.Context, providers.DispatchRequest) (ForwardCredential, error) {
			return ForwardCredential{Authorization: "Bearer token"}, nil
		}),
		HTTPClient: &http.Client{Transport: forwardRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusBadRequest,
				Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"Unknown parameter: 'max_output_tokens'."}}`)),
			}, nil
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Open(context.Background(), observedForwardDispatch(t))
	if err == nil || !strings.Contains(err.Error(), "Unknown parameter: 'max_output_tokens'.") {
		t.Fatalf("err=%v", err)
	}
}

func TestForwardOpenOmitsHTTP401Body(t *testing.T) {
	client, err := NewForward(ForwardConfig{
		Endpoint: testCanonicalForwardResponsesEndpoint,
		CredentialAuthority: forwardCredentialAuthorityFunc(func(context.Context, providers.DispatchRequest) (ForwardCredential, error) {
			return ForwardCredential{Authorization: "Bearer token"}, nil
		}),
		HTTPClient: &http.Client{Transport: forwardRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusUnauthorized,
				Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"SECRET-UPSTREAM-BODY"}}`)),
			}, nil
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Open(context.Background(), observedForwardDispatch(t))
	if err == nil || strings.Contains(err.Error(), "SECRET-UPSTREAM-BODY") {
		t.Fatalf("err=%v", err)
	}
}

func TestSanitizeProviderOpenDetailRejectsSecrets(t *testing.T) {
	if got := sanitizeProviderOpenDetail("invalid key sk-leaked"); got != "" {
		t.Fatalf("got %q", got)
	}
	if got := extractUpstreamErrorDetail([]byte(`{"error":{"message":"Unknown parameter: 'max_output_tokens'."}}`)); got != "Unknown parameter: 'max_output_tokens'." {
		t.Fatalf("got %q", got)
	}
}
