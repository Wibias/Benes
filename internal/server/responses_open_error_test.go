package server

import (
	"net/http"
	"testing"

	"github.com/Wibias/Benes/internal/capability"
)

func TestCanonicalResponsesOpenErrorMapsStructuredOutputCapabilityRefusal(t *testing.T) {
	got := canonicalResponsesOpenError(capability.ErrStructuredOutputUnsupported)
	if got == nil {
		t.Fatal("expected canonical local open error")
	}
	if got.StatusCode != http.StatusBadRequest || got.ErrorType != "invalid_request_error" || got.Code != "unsupported_structured_output" {
		t.Fatalf("open error=%#v", got)
	}
	if got.Retryable == nil || *got.Retryable {
		t.Fatalf("retryable=%#v", got.Retryable)
	}
	if got.Message == "" {
		t.Fatal("missing public message")
	}
}
