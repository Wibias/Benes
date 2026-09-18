package openairesponses

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/Wibias/Benes/internal/providers"
)

var (
	ErrForwardAdmissionCredential   = errors.New("proxy admission authorization cannot be forwarded upstream")
	ErrForwardAuthorizationRequired = errors.New("caller authorization is required for forward auth")
	ErrDirectAuthorizationRequired  = errors.New("Codex Direct requires a caller Authorization bearer token")
	directBearerPattern             = regexp.MustCompile(`(?i)^Bearer\s+\S+`)
)

type ForwardCredential struct {
	Authorization    string
	ChatGPTAccountID string
	TrustedAccountID string
	UsageAccount     string
}

type ForwardCredentialAuthority interface {
	Resolve(context.Context, providers.DispatchRequest) (ForwardCredential, error)
}

type CallerForwardAuthority struct{}

func (CallerForwardAuthority) Resolve(_ context.Context, dispatch providers.DispatchRequest) (ForwardCredential, error) {
	headers := dispatch.ForwardHeaders
	if headers.BlockedAuthorization() {
		return ForwardCredential{}, ErrForwardAdmissionCredential
	}
	authorization := headers.Get("authorization")
	if strings.TrimSpace(authorization) == "" {
		return ForwardCredential{}, ErrForwardAuthorizationRequired
	}
	return ForwardCredential{
		Authorization:    authorization,
		ChatGPTAccountID: headers.Get("chatgpt-account-id"),
	}, nil
}

type DirectForwardAuthority struct{}

func (DirectForwardAuthority) Resolve(_ context.Context, dispatch providers.DispatchRequest) (ForwardCredential, error) {
	headers := dispatch.ForwardHeaders
	if headers.BlockedAuthorization() {
		return ForwardCredential{}, ErrForwardAdmissionCredential
	}
	authorization := headers.Get("authorization")
	if !directBearerPattern.MatchString(strings.TrimSpace(authorization)) {
		return ForwardCredential{}, ErrDirectAuthorizationRequired
	}
	return ForwardCredential{
		Authorization:    authorization,
		ChatGPTAccountID: headers.Get("chatgpt-account-id"),
	}, nil
}
