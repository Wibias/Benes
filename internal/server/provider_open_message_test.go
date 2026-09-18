package server

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestPublicProviderOpenMessageProjectsHTTP400Detail(t *testing.T) {
	got := publicProviderOpenMessage(fmt.Errorf("OpenAI Responses forward upstream returned HTTP 400: Unknown parameter: 'max_output_tokens'."))
	if got != "provider returned HTTP 400: Unknown parameter: 'max_output_tokens'." {
		t.Fatalf("got %q", got)
	}
}

func TestPublicProviderOpenMessageKeepsUnknownOpenErrorsOpaque(t *testing.T) {
	secret := "SECRET-UPSTREAM-BODY"
	got := publicProviderOpenMessage(errors.New(secret))
	if got != "provider request could not be opened" || strings.Contains(got, secret) {
		t.Fatalf("got %q", got)
	}
}

func TestPublicProviderOpenMessageOmitsNon400Bodies(t *testing.T) {
	got := publicProviderOpenMessage(fmt.Errorf("OpenAI Responses upstream returned HTTP 401: SECRET-UPSTREAM-BODY"))
	if got != "provider returned HTTP 401" || strings.Contains(got, "SECRET-UPSTREAM-BODY") {
		t.Fatalf("got %q", got)
	}
}

func TestPublicProviderOpenMessageDropsSecretLike400Detail(t *testing.T) {
	got := publicProviderOpenMessage(fmt.Errorf("OpenAI Responses upstream returned HTTP 400: invalid key sk-leaked"))
	if got != "provider returned HTTP 400" {
		t.Fatalf("got %q", got)
	}
}
