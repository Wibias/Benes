package codexauth

import (
	"context"
	"errors"
	"strings"
	"time"
)

var (
	ErrPoolCredentialResolverConfiguration = errors.New("Codex Pool credential resolver is not configured")
	ErrPoolNoUsableAccount                 = errors.New("Codex Pool has no usable account credential")
	ErrPoolSelectedCredentialUnavailable   = errors.New("selected Codex Pool credential is unavailable")
	ErrPoolOutcomeAwareTransportRequired   = errors.New("Codex Pool cooldown recovery requires outcome-aware transport")
)

type PoolAffinityExpiredError struct {
	AccountID string
}

func (*PoolAffinityExpiredError) Error() string {
	return "Codex thread account affinity expired"
}

type PoolCooldownError struct {
	AccountID string
	Scope     QuotaScope
	Until     time.Time
	Source    CooldownSource
}

func (*PoolCooldownError) Error() string {
	return "selected Codex Pool account is cooling down"
}

func (*PoolCooldownError) Unwrap() error {
	return ErrPoolOutcomeAwareTransportRequired
}

type PoolCredentialMaterializationError struct {
	AccountID string
	cause     error
}

func (*PoolCredentialMaterializationError) Error() string {
	return "Codex Pool account authentication failed"
}

func (e *PoolCredentialMaterializationError) Unwrap() error {
	return e.cause
}

type PoolCredential struct {
	AccountID         string
	AccessToken       string
	ChatGPTAccountID  string
	MainIdentity      string
	UsageAccount      string
	Generation        int64
	WriterGeneration  uint64
	Scope             QuotaScope
	ProbeLease        *ProbeLease
	FailoverThreshold *int
	FixedAccount      bool
}

type PoolCredentialRequest struct {
	Selection                 PoolSelectionInput
	WriterGeneration          uint64
	OutcomeAware              bool
	Alternate                 bool
	FixedAccountID            string
	ForceMainRefresh          bool
	ForceStoredAccountRefresh bool
	RefreshAccountID          string
}

type ManagedTokenGetter interface {
	Get(context.Context, string) (ManagedToken, error)
}

type ManagedTokenForcer interface {
	ForceRefresh(context.Context, string) (ManagedToken, error)
}

type ManagedCredentialReader interface {
	Read() ManagedCredentialSnapshot
}

type MainCredentialReader interface {
	Read(time.Time) MainCredentialResult
}

type PoolCredentialResolverConfig struct {
	Selector               *PoolSelector
	ManagedTokens          ManagedTokenGetter
	ManagedCredentials     ManagedCredentialReader
	MainCredentials        MainCredentialReader
	OnSelectedQuotaMissing func(string)
}

type PoolCredentialResolver struct {
	selector               *PoolSelector
	managedTokens          ManagedTokenGetter
	managedCredentials     ManagedCredentialReader
	mainCredentials        MainCredentialReader
	onSelectedQuotaMissing func(string)
}

func NewPoolCredentialResolver(config PoolCredentialResolverConfig) (*PoolCredentialResolver, error) {
	if config.Selector == nil || config.ManagedTokens == nil || config.ManagedCredentials == nil || config.MainCredentials == nil {
		return nil, ErrPoolCredentialResolverConfiguration
	}
	return &PoolCredentialResolver{
		selector: config.Selector, managedTokens: config.ManagedTokens,
		managedCredentials: config.ManagedCredentials, mainCredentials: config.MainCredentials,
		onSelectedQuotaMissing: config.OnSelectedQuotaMissing,
	}, nil
}

func (r *PoolCredentialResolver) Resolve(ctx context.Context, request PoolCredentialRequest) (PoolCredential, error) {
	if err := ctx.Err(); err != nil {
		return PoolCredential{}, err
	}
	selectionInput := request.Selection
	if selectionInput.Now.IsZero() {
		selectionInput.Now = time.Now()
	}
	fixed := strings.TrimSpace(request.FixedAccountID) != ""
	var selection PoolSelectionResult
	switch {
	case fixed:
		selection = r.selector.ResolveFixed(selectionInput, strings.TrimSpace(request.FixedAccountID))
	case request.Alternate:
		selection = r.selector.PickAlternate(selectionInput)
	default:
		selection = r.selector.Resolve(selectionInput)
	}
	switch selection.Status {
	case PoolSelectionExpired:
		return PoolCredential{}, &PoolAffinityExpiredError{AccountID: selection.AccountID}
	case PoolSelectionNone:
		if fixed {
			return PoolCredential{}, ErrPoolSelectedCredentialUnavailable
		}
		return PoolCredential{}, ErrPoolNoUsableAccount
	case PoolSelectionSelected:
	default:
		return PoolCredential{}, ErrPoolNoUsableAccount
	}
	if r.onSelectedQuotaMissing != nil {
		if _, known := selectionInput.Quotas[selection.AccountID]; !known {
			r.onSelectedQuotaMissing(selection.AccountID)
		}
	}

	var probeLease *ProbeLease
	if selection.Degraded {
		if fixed {
			if selection.RequiresProbe {
				return PoolCredential{}, r.cooldownError(selectionInput, selection.AccountID)
			}
			return PoolCredential{}, ErrPoolSelectedCredentialUnavailable
		}
		if !selection.RequiresProbe {
			return PoolCredential{}, ErrPoolSelectedCredentialUnavailable
		}
		if !request.OutcomeAware {
			return PoolCredential{}, r.cooldownError(selectionInput, selection.AccountID)
		}
		lease, ok, err := r.tryAcquireProbe(selectionInput, selection.AccountID)
		if err != nil {
			return PoolCredential{}, err
		}
		if !ok {
			return PoolCredential{}, r.cooldownError(selectionInput, selection.AccountID)
		}
		probeLease = &lease
	}

	releaseProbe := func() {
		if probeLease == nil {
			return
		}
		r.selector.Health.ReleaseProbe(*probeLease, selectionInput.Now)
		probeLease = nil
	}
	threshold := cloneOptionalInt(selectionInput.FailoverThreshold)

	if selection.AccountID == MainAccountID {
		main, err := r.materializeMain(ctx, selectionInput.Now, request.ForceMainRefresh)
		if err != nil {
			releaseProbe()
			if shouldMarkMainRefreshNeedsReauth(err) {
				r.selector.Reauth.Mark(MainAccountID, request.WriterGeneration)
			}
			return PoolCredential{}, ErrPoolSelectedCredentialUnavailable
		}
		if main.Status != MainCredentialOK || !validPoolAccessToken(main.Credential.AccessToken) {
			releaseProbe()
			r.selector.Reauth.Mark(MainAccountID, request.WriterGeneration)
			return PoolCredential{}, ErrPoolSelectedCredentialUnavailable
		}
		return PoolCredential{
			AccountID: MainAccountID, AccessToken: main.Credential.AccessToken,
			ChatGPTAccountID: main.Credential.ChatGPTAccountID, MainIdentity: main.Identity,
			UsageAccount: UsageLogLabel(MainAccountID, selectionInput.Accounts), Generation: 0,
			WriterGeneration: request.WriterGeneration, Scope: selectionInput.Scope,
			ProbeLease: probeLease, FailoverThreshold: threshold, FixedAccount: fixed,
		}, nil
	}

	var token ManagedToken
	var err error
	if request.ForceStoredAccountRefresh {
		target := strings.TrimSpace(request.RefreshAccountID)
		if target == "" {
			target = selection.AccountID
		}
		if selection.AccountID != target {
			releaseProbe()
			return PoolCredential{}, ErrPoolSelectedCredentialUnavailable
		}
		forcer, ok := r.managedTokens.(ManagedTokenForcer)
		if !ok {
			releaseProbe()
			return PoolCredential{}, ErrPoolSelectedCredentialUnavailable
		}
		token, err = forcer.ForceRefresh(ctx, selection.AccountID)
	} else {
		token, err = r.managedTokens.Get(ctx, selection.AccountID)
	}
	if err != nil {
		releaseProbe()
		if shouldMarkPoolMaterializationNeedsReauth(err) {
			r.selector.Reauth.Mark(selection.AccountID, request.WriterGeneration)
		}
		return PoolCredential{}, &PoolCredentialMaterializationError{AccountID: selection.AccountID, cause: err}
	}
	if err := ctx.Err(); err != nil {
		releaseProbe()
		return PoolCredential{}, err
	}
	if !validPoolAccessToken(token.AccessToken) {
		releaseProbe()
		r.selector.Reauth.Mark(selection.AccountID, request.WriterGeneration)
		return PoolCredential{}, ErrPoolSelectedCredentialUnavailable
	}
	live := r.managedCredentials.Read()
	if !CredentialGenerationLive(live, selection.AccountID, token.Generation) {
		releaseProbe()
		return PoolCredential{}, &PoolCredentialMaterializationError{
			AccountID: selection.AccountID, cause: ErrManagedCredentialGenerationConflict,
		}
	}
	return PoolCredential{
		AccountID: selection.AccountID, AccessToken: token.AccessToken,
		ChatGPTAccountID: token.ChatGPTAccountID, Generation: token.Generation,
		UsageAccount:     UsageLogLabel(selection.AccountID, selectionInput.Accounts),
		WriterGeneration: request.WriterGeneration, Scope: selectionInput.Scope,
		ProbeLease: probeLease, FailoverThreshold: threshold, FixedAccount: fixed,
	}, nil
}

func (r *PoolCredentialResolver) tryAcquireProbe(input PoolSelectionInput, accountID string) (ProbeLease, bool, error) {
	if _, exists := r.selector.Health.HardCooldown(accountID, input.Now); exists {
		return r.selector.Health.TryAcquireHardCooldownProbe(accountID, input.Now)
	}
	if input.Scope != "" {
		if _, exists := r.selector.Health.ScopedHardCooldown(accountID, input.Scope, input.Now); exists {
			return r.selector.Health.TryAcquireScopedHardCooldownProbe(accountID, input.Scope, input.Now)
		}
	}
	return ProbeLease{}, false, nil
}

func (r *PoolCredentialResolver) cooldownError(input PoolSelectionInput, accountID string) error {
	if cooldown, ok := r.selector.Health.HardCooldown(accountID, input.Now); ok {
		return &PoolCooldownError{AccountID: accountID, Until: cooldown.Until, Source: cooldown.Source}
	}
	if input.Scope != "" {
		if cooldown, ok := r.selector.Health.ScopedHardCooldown(accountID, input.Scope, input.Now); ok {
			return &PoolCooldownError{AccountID: accountID, Scope: input.Scope, Until: cooldown.Until, Source: cooldown.Source}
		}
	}
	return ErrPoolOutcomeAwareTransportRequired
}

func validPoolAccessToken(token string) bool {
	return strings.TrimSpace(token) != "" && !strings.ContainsAny(token, " \t\r\n")
}

func cloneOptionalInt(value *int) *int {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func shouldMarkPoolMaterializationNeedsReauth(err error) bool {
	return !errors.Is(err, ErrManagedCredentialGenerationConflict) &&
		!errors.Is(err, ErrManagedCredentialRefreshBusy) &&
		!errors.Is(err, context.Canceled) &&
		!errors.Is(err, context.DeadlineExceeded)
}

func (r *PoolCredentialResolver) materializeMain(ctx context.Context, now time.Time, force bool) (MainCredentialResult, error) {
	if materializer, ok := r.mainCredentials.(MainCredentialMaterializer); ok {
		return materializer.Ensure(ctx, now, force)
	}
	return r.mainCredentials.Read(now), nil
}

func shouldMarkMainRefreshNeedsReauth(err error) bool {
	return !errors.Is(err, ErrMainAuthChangedDuringRefresh) &&
		!errors.Is(err, ErrManagedCredentialRefreshBusy) &&
		!errors.Is(err, context.Canceled) &&
		!errors.Is(err, context.DeadlineExceeded)
}
