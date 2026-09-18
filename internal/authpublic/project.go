package authpublic

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/Wibias/Benes/internal/codexauth"
	"github.com/Wibias/Benes/internal/credentials"
)

const GenericAuthenticationFailed = "authentication failed"

var tokenLike = regexp.MustCompile(`(?i)(sk-|rk-|eyJ|bearer\s+[a-z0-9._\-]+|oauth:|/home/|/users/|[a-z]:\\)`)

func IsAuthentication(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, credentials.ErrCredentialUnavailable) ||
		errors.Is(err, credentials.ErrPlaintextDisabled) ||
		errors.Is(err, codexauth.ErrManagedCredentialUnavailable) ||
		errors.Is(err, codexauth.ErrManagedCredentialRefreshBusy) ||
		errors.Is(err, codexauth.ErrManagedCredentialRefreshData) {
		return true
	}
	var refresh *codexauth.ManagedTokenRefreshError
	if errors.As(err, &refresh) {
		return true
	}
	var loginRequired LoginRequiredError
	var busy LoginBusyError
	var publication ProviderPublicationError
	var mismatch ReauthIdentityMismatchError
	var unverified ReauthIdentityUnverifiedError
	var superseded LoginSupersededError
	var mutation MutationBusyError
	var sidecar SidecarError
	return errors.As(err, &loginRequired) ||
		errors.As(err, &busy) ||
		errors.As(err, &publication) ||
		errors.As(err, &mismatch) ||
		errors.As(err, &unverified) ||
		errors.As(err, &superseded) ||
		errors.As(err, &mutation) ||
		errors.As(err, &sidecar)
}

func Project(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, credentials.ErrCredentialUnavailable),
		errors.Is(err, credentials.ErrPlaintextDisabled),
		errors.Is(err, codexauth.ErrManagedCredentialUnavailable):
		return "credential is unavailable; reauthenticate the account"
	case errors.Is(err, codexauth.ErrManagedCredentialRefreshBusy):
		return "authentication is busy; retry shortly"
	case errors.Is(err, codexauth.ErrManagedCredentialRefreshData):
		return "authentication data is invalid; reauthenticate the account"
	}
	var refresh *codexauth.ManagedTokenRefreshError
	if errors.As(err, &refresh) {
		return "authentication expired; reauthenticate the account"
	}
	var loginRequired LoginRequiredError
	if errors.As(err, &loginRequired) {
		return loginRequired.Error()
	}
	var busy LoginBusyError
	if errors.As(err, &busy) {
		return busy.Error()
	}
	var publication ProviderPublicationError
	if errors.As(err, &publication) {
		return publication.Error()
	}
	var mismatch ReauthIdentityMismatchError
	if errors.As(err, &mismatch) {
		return mismatch.Error()
	}
	var unverified ReauthIdentityUnverifiedError
	if errors.As(err, &unverified) {
		return unverified.Error()
	}
	var superseded LoginSupersededError
	if errors.As(err, &superseded) {
		return superseded.Error()
	}
	var mutation MutationBusyError
	if errors.As(err, &mutation) {
		return mutation.Error()
	}
	var sidecar SidecarError
	if errors.As(err, &sidecar) {
		return sidecar.Error()
	}
	return failClosed(err)
}

func failClosed(err error) string {
	if err == nil {
		return GenericAuthenticationFailed
	}
	_ = err.Error()
	return GenericAuthenticationFailed
}

func ProjectOAuth(err error) string {
	if err == nil {
		return ""
	}
	msg := Project(err)
	if msg == GenericAuthenticationFailed {
		return GenericOAuthFailed
	}
	return msg
}

func KnownProviderName(name string, known map[string]struct{}) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if known == nil {
		known = KnownOAuthProviders
	}
	if _, ok := known[name]; ok {
		return name
	}
	return "provider"
}

func UnsupportedProviderGuidance(name string) string {
	safe := KnownProviderName(name, KnownOAuthProviders)
	if safe == "" {
		safe = "provider"
	}
	return fmt.Sprintf("Remove or reconfigure provider '%s' in the Benes configuration.", safe)
}

func SidecarHTTP(status int, body string) error {
	_ = body
	if status == http.StatusUnauthorized {
		return SidecarError{Status: status, Auth: true}
	}
	return SidecarError{Status: status}
}

func SidecarConnect() error {
	return SidecarError{Connect: true}
}

func SidecarStream() error {
	return SidecarError{Stream: true}
}
