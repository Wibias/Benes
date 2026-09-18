package openairesponses

import (
	"context"
	"errors"
	"strings"

	"github.com/Wibias/Benes/internal/providers"
)

var ErrCodexAccountAuthorityConfiguration = errors.New("Codex account authority is not configured")

type CodexAccountAuthority struct {
	direct ForwardCredentialAuthority
	fixed  ForwardCredentialAuthority
}

func NewCodexAccountAuthority(direct, fixed ForwardCredentialAuthority) *CodexAccountAuthority {
	return &CodexAccountAuthority{direct: direct, fixed: fixed}
}

func (a *CodexAccountAuthority) Resolve(ctx context.Context, dispatch providers.DispatchRequest) (ForwardCredential, error) {
	attempt, err := a.ResolveAttempt(ctx, dispatch)
	return attempt.Credential, err
}

func (a *CodexAccountAuthority) ResolveAttempt(ctx context.Context, dispatch providers.DispatchRequest) (ForwardAttempt, error) {
	if strings.TrimSpace(dispatch.CodexAccountID) != "" {
		if a == nil || a.fixed == nil {
			return ForwardAttempt{}, ErrCodexAccountAuthorityConfiguration
		}
		attempt, err := resolveForwardAttempt(ctx, a.fixed, dispatch)
		if err != nil {
			return ForwardAttempt{}, err
		}
		// Exact selectors are single-account requests. Defense in depth keeps a
		// future fixed authority from accidentally exposing Pool failover seams.
		attempt.RetryQuota = nil
		attempt.RetryModel400 = nil
		attempt.CommitQuotaRetry = nil
		return attempt, nil
	}
	if a == nil || a.direct == nil {
		return ForwardAttempt{}, ErrCodexAccountAuthorityConfiguration
	}
	return resolveForwardAttempt(ctx, a.direct, dispatch)
}

var _ ForwardCredentialAuthority = (*CodexAccountAuthority)(nil)
var _ ForwardAttemptAuthority = (*CodexAccountAuthority)(nil)
