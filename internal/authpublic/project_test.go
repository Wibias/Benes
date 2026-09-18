package authpublic

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/Wibias/Benes/internal/codexauth"
	"github.com/Wibias/Benes/internal/credentials"
)

func TestProjectMapsTypedAuthErrorsAndRedactsSecrets(t *testing.T) {
	if got := Project(codexauth.ErrManagedCredentialUnavailable); got != "credential is unavailable; reauthenticate the account" {
		t.Fatalf("unavailable=%q", got)
	}
	if got := Project(fmt.Errorf("wrap: %w", credentials.ErrPlaintextDisabled)); got != "credential is unavailable; reauthenticate the account" {
		t.Fatalf("plaintext=%q", got)
	}
	if got := Project(codexauth.ErrManagedCredentialRefreshBusy); got != "authentication is busy; retry shortly" {
		t.Fatalf("busy=%q", got)
	}
	if got := Project(&codexauth.ManagedTokenRefreshError{Reason: codexauth.ManagedRefreshExpired}); got != "authentication expired; reauthenticate the account" {
		t.Fatalf("refresh=%q", got)
	}
	leaky := errors.New("refresh failed: Bearer sk-live-secret from C:\\Users\\ws\\.benes\\auth.json")
	if got := Project(leaky); got != GenericAuthenticationFailed {
		t.Fatalf("leaky=%q", got)
	}
	if tokenLike.MatchString(Project(leaky)) {
		t.Fatal("secret leaked through projector")
	}
	if got := ProjectOAuth(leaky); got != GenericOAuthFailed {
		t.Fatalf("oauth leak=%q", got)
	}
}

func TestProjectPreservesTypedOAuthContracts(t *testing.T) {
	if got := Project(LoginRequiredError{Provider: "xai"}); got != "Not logged in to xai. Run: benes login xai" {
		t.Fatalf("login required=%q", got)
	}
	if got := Project(LoginRequiredError{Provider: "../evil\nsk-secret"}); got != "Not logged in to provider. Run: benes login provider" {
		t.Fatalf("attacker provider=%q", got)
	}
	if got := Project(LoginBusyError{Provider: "xai"}); got != "A login for xai is already in progress" {
		t.Fatalf("busy=%q", got)
	}
	if got := Project(ReauthIdentityMismatchError{}); got != ReauthMismatchMessage {
		t.Fatalf("mismatch=%q", got)
	}
	if got := Project(ReauthIdentityUnverifiedError{}); got != ReauthUnverifiedMessage {
		t.Fatalf("unverified=%q", got)
	}
	if got := Project(ProviderPublicationError{}); got != PublicationMessage {
		t.Fatalf("publication=%q", got)
	}
	if got := Project(LoginSupersededError{}); got != SupersededMessage {
		t.Fatalf("superseded=%q", got)
	}
	if got := Project(MutationBusyError{TimedOut: true}); got != MutationTimeoutMessage {
		t.Fatalf("timeout=%q", got)
	}
}

func TestKnownProviderNameDoesNotEchoAttackerInput(t *testing.T) {
	known := map[string]struct{}{"openai": {}}
	if got := KnownProviderName("openai", known); got != "openai" {
		t.Fatalf("known=%q", got)
	}
	if got := KnownProviderName("../evil", known); got != "provider" {
		t.Fatalf("unknown=%q", got)
	}
	guidance := UnsupportedProviderGuidance("C:\\Users\\ws\\.benes\\config.json")
	if tokenLike.MatchString(guidance) || guidance != "Remove or reconfigure provider 'provider' in the Benes configuration." {
		t.Fatalf("guidance=%q", guidance)
	}
}

func TestSidecarErrorsNeverIncludeUpstreamBodies(t *testing.T) {
	err := SidecarHTTP(http.StatusUnauthorized, `{"error":"SECRET sk-leaked"}`)
	if got := Project(err); got != SidecarAuthMessage || tokenLike.MatchString(got) {
		t.Fatalf("401=%q", got)
	}
	err = SidecarHTTP(502, "internal stack /home/user/.benes/auth.json")
	if got := Project(err); got != "sidecar HTTP 502" {
		t.Fatalf("502=%q", got)
	}
	if got := Project(SidecarStream()); got != SidecarStreamMessage {
		t.Fatalf("stream=%q", got)
	}
	if got := Project(SidecarConnect()); got != SidecarConnectMessage {
		t.Fatalf("connect=%q", got)
	}
}
