package authpublic

import "fmt"

const (
	GenericOAuthFailed = "OAuth authentication failed. Check the Benes account status and retry."

	LoginRequiredTemplate = "Not logged in to %s. Run: benes login %s"
	LoginBusyTemplate     = "A login for %s is already in progress"

	ReauthMismatchMessage   = "Signed-in account does not match the selected account. Sign in with the same account."
	ReauthUnverifiedMessage = "Could not verify signed-in account identity for reauth."
	PublicationMessage      = "OAuth credential was saved, but the provider entry was not written. Resolve the account namespace collision, then retry login."
	SupersededMessage       = "OAuth login was superseded before credential persistence"
	MutationBusyMessage     = "OAuth mutation queue is busy"
	MutationTimeoutMessage  = "OAuth mutation queue wait timed out"

	SidecarHTTPMessage    = "sidecar HTTP %d"
	SidecarAuthMessage    = "sidecar auth failed"
	SidecarStreamMessage  = "sidecar stream error"
	SidecarConnectMessage = "sidecar connect failed"
)

var KnownOAuthProviders = map[string]struct{}{
	"command-code":       {},
	"xai":                {},
	"anthropic":          {},
	"kimi":               {},
	"nous":               {},
	"kiro":               {},
	"google-antigravity": {},
	"cursor":             {},
	"github-copilot":     {},
	"chatgpt":            {},
}

type LoginRequiredError struct{ Provider string }

func (e LoginRequiredError) Error() string {
	name := KnownProviderName(e.Provider, KnownOAuthProviders)
	if name == "" {
		name = "provider"
	}
	return fmt.Sprintf(LoginRequiredTemplate, name, name)
}

type LoginBusyError struct{ Provider string }

func (e LoginBusyError) Error() string {
	name := KnownProviderName(e.Provider, KnownOAuthProviders)
	if name == "" {
		name = "provider"
	}
	return fmt.Sprintf(LoginBusyTemplate, name)
}

type ProviderPublicationError struct{}

func (ProviderPublicationError) Error() string { return PublicationMessage }

type ReauthIdentityMismatchError struct{}

func (ReauthIdentityMismatchError) Error() string { return ReauthMismatchMessage }

type ReauthIdentityUnverifiedError struct{}

func (ReauthIdentityUnverifiedError) Error() string { return ReauthUnverifiedMessage }

type LoginSupersededError struct{}

func (LoginSupersededError) Error() string { return SupersededMessage }

type MutationBusyError struct{ TimedOut bool }

func (e MutationBusyError) Error() string {
	if e.TimedOut {
		return MutationTimeoutMessage
	}
	return MutationBusyMessage
}

type SidecarError struct {
	Status  int
	Kind    string
	Auth    bool
	Connect bool
	Stream  bool
}

func (e SidecarError) Error() string {
	if e.Auth {
		return SidecarAuthMessage
	}
	if e.Connect {
		return SidecarConnectMessage
	}
	if e.Stream {
		return SidecarStreamMessage
	}
	if e.Status > 0 {
		return fmt.Sprintf(SidecarHTTPMessage, e.Status)
	}
	if e.Kind != "" {
		return "sidecar " + e.Kind
	}
	return SidecarConnectMessage
}
