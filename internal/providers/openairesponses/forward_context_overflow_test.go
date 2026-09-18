package openairesponses

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/providers"
)

func TestForwardProvider413ReturnsCanonicalContextOverflow(t *testing.T) {
	const secret = "provider-secret-must-not-leak"
	client, err := NewForward(ForwardConfig{
		Endpoint: testCanonicalForwardResponsesEndpoint,
		HTTPClient: &http.Client{Transport: forwardRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusRequestEntityTooLarge,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"detail":"request too large; ` + secret + `"}`)),
			}, nil
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	dispatch := canonicalRequest(t, `{"model":"openai/gpt-5.6","store":false,"input":"oversized"}`, "gpt-5.6")
	dispatch.ForwardHeaders = providers.NewForwardHeaders(map[string]string{
		"authorization": "Bearer caller-oauth",
	})

	_, err = client.Open(context.Background(), dispatch)
	var openErr *providers.OpenError
	if !errors.As(err, &openErr) {
		t.Fatalf("err=%v", err)
	}
	if openErr.StatusCode != http.StatusRequestEntityTooLarge || openErr.ErrorType != "invalid_request_error" || openErr.Code != "context_length_exceeded" {
		t.Fatalf("openErr=%#v", openErr)
	}
	if openErr.Retryable == nil || *openErr.Retryable {
		t.Fatalf("retryable=%v", openErr.Retryable)
	}
	if strings.Contains(openErr.Error(), secret) {
		t.Fatalf("provider body leaked: %q", openErr.Error())
	}
}
