package openairesponses

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/Wibias/Benes/internal/codexauth"
	"github.com/Wibias/Benes/internal/providers"
)

var (
	ErrCodexPoolAuthorityConfiguration = errors.New("Codex Pool authority is not configured")
	ErrCodexPoolCredentialInvalid      = errors.New("Codex Pool authority returned an invalid credential")
)

type CodexPoolDispatchIdentity struct {
	ModelID  string
	ThreadID string
}

type CodexPoolSnapshotSource interface {
	Snapshot(context.Context, CodexPoolDispatchIdentity) (codexauth.PoolCredentialRequest, error)
}

type CodexPoolSnapshotFunc func(context.Context, CodexPoolDispatchIdentity) (codexauth.PoolCredentialRequest, error)

func (f CodexPoolSnapshotFunc) Snapshot(ctx context.Context, identity CodexPoolDispatchIdentity) (codexauth.PoolCredentialRequest, error) {
	return f(ctx, identity)
}

type codexPoolCredentialResolver interface {
	Resolve(context.Context, codexauth.PoolCredentialRequest) (codexauth.PoolCredential, error)
}

type codexPoolQuotaPromoter interface {
	PromoteQuotaAlternate(codexauth.PoolSelectionInput, string, string) bool
}

type codexPoolOutcomeRecorder interface {
	Record(string, codexauth.UpstreamOutcome, codexauth.OutcomeMeta) codexauth.OutcomeClass
}

type codexPoolQuotaRecorder interface {
	Record(codexauth.PoolCredential, codexauth.UpstreamQuotaHeaders) bool
}

type CodexPoolAuthority struct {
	snapshot CodexPoolSnapshotSource
	resolver codexPoolCredentialResolver
	outcomes codexPoolOutcomeRecorder
	quotas   codexPoolQuotaRecorder
}

func NewCodexPoolAuthority(snapshot CodexPoolSnapshotSource, resolver codexPoolCredentialResolver) (*CodexPoolAuthority, error) {
	return newCodexPoolAuthority(snapshot, resolver, nil, nil)
}

func NewCodexPoolAuthorityWithOutcomes(
	snapshot CodexPoolSnapshotSource,
	resolver codexPoolCredentialResolver,
	outcomes codexPoolOutcomeRecorder,
) (*CodexPoolAuthority, error) {
	if outcomes == nil {
		return nil, ErrCodexPoolAuthorityConfiguration
	}
	return newCodexPoolAuthority(snapshot, resolver, outcomes, nil)
}

func NewCodexPoolAuthorityWithOutcomesAndQuota(
	snapshot CodexPoolSnapshotSource,
	resolver codexPoolCredentialResolver,
	outcomes codexPoolOutcomeRecorder,
	quotas codexPoolQuotaRecorder,
) (*CodexPoolAuthority, error) {
	if outcomes == nil || quotas == nil {
		return nil, ErrCodexPoolAuthorityConfiguration
	}
	return newCodexPoolAuthority(snapshot, resolver, outcomes, quotas)
}

func newCodexPoolAuthority(
	snapshot CodexPoolSnapshotSource,
	resolver codexPoolCredentialResolver,
	outcomes codexPoolOutcomeRecorder,
	quotas codexPoolQuotaRecorder,
) (*CodexPoolAuthority, error) {
	if snapshot == nil || resolver == nil {
		return nil, ErrCodexPoolAuthorityConfiguration
	}
	return &CodexPoolAuthority{snapshot: snapshot, resolver: resolver, outcomes: outcomes, quotas: quotas}, nil
}

func (a *CodexPoolAuthority) Resolve(ctx context.Context, dispatch providers.DispatchRequest) (ForwardCredential, error) {
	credential, _, err := a.resolvePoolCredential(ctx, dispatch, false, "", false, false, "")
	if err != nil {
		return ForwardCredential{}, err
	}
	return forwardCredentialFromPool(credential)
}

func (a *CodexPoolAuthority) ResolveAttempt(ctx context.Context, dispatch providers.DispatchRequest) (ForwardAttempt, error) {
	if a.outcomes == nil {
		credential, err := a.Resolve(ctx, dispatch)
		return ForwardAttempt{Credential: credential}, err
	}
	credential, _, err := a.resolvePoolCredential(ctx, dispatch, true, "", false, false, "")
	if err != nil {
		return ForwardAttempt{}, err
	}
	return a.forwardAttemptForPoolCredential(dispatch, credential, true)
}

func (a *CodexPoolAuthority) forwardAttemptForPoolCredential(
	dispatch providers.DispatchRequest,
	credential codexauth.PoolCredential,
	allowAccountRetry bool,
) (ForwardAttempt, error) {
	observer := newCodexPoolAttemptObserverWithQuota(a.outcomes, a.quotas, credential)
	forward, err := forwardCredentialFromPool(credential)
	if err != nil {
		observer.Abandon()
		return ForwardAttempt{}, err
	}
	attempt := ForwardAttempt{Credential: forward, Observer: observer}
	if allowAccountRetry {
		observer.pendingSameAccountAuthRetry = true
		attempt.RetryAuth = func(ctx context.Context) (ForwardAttempt, bool, error) {
			sameAccountID := credential.AccountID
			forceMain := sameAccountID == codexauth.MainAccountID
			forceStored := sameAccountID != "" && sameAccountID != codexauth.MainAccountID
			refreshed, _, err := a.resolvePoolCredential(ctx, dispatch, true, "", forceMain, forceStored, sameAccountID)
			if err != nil {
				observer.pendingSameAccountAuthRetry = false
				if ctxErr := ctx.Err(); ctxErr != nil {
					return ForwardAttempt{}, false, ctxErr
				}
				if codexPoolRetryUnavailable(err) {
					return ForwardAttempt{}, false, nil
				}
				return ForwardAttempt{}, false, err
			}
			if refreshed.AccountID != sameAccountID {
				observer.pendingSameAccountAuthRetry = false
				abandonForwardObserver(newCodexPoolAttemptObserverWithQuota(a.outcomes, a.quotas, refreshed))
				return ForwardAttempt{}, false, nil
			}
			retryAttempt, err := a.forwardAttemptForPoolCredential(dispatch, refreshed, false)
			if err != nil {
				observer.pendingSameAccountAuthRetry = false
				return ForwardAttempt{}, false, err
			}
			return retryAttempt, true, nil
		}
	}
	if !allowAccountRetry || credential.FixedAccount {
		return attempt, nil
	}
	excludedAccountID := credential.AccountID
	resolveAlternate := func(ctx context.Context) (ForwardAttempt, bool, error) {
		alternate, selectionInput, err := a.resolvePoolCredential(ctx, dispatch, true, excludedAccountID, false, false, "")
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ForwardAttempt{}, false, ctxErr
			}
			if codexPoolRetryUnavailable(err) {
				return ForwardAttempt{}, false, nil
			}
			return ForwardAttempt{}, false, err
		}
		if alternate.AccountID == "" || alternate.AccountID == excludedAccountID {
			alternateObserver := newCodexPoolAttemptObserverWithQuota(a.outcomes, a.quotas, alternate)
			alternateObserver.Abandon()
			return ForwardAttempt{}, false, nil
		}
		retryAttempt, err := a.forwardAttemptForPoolCredential(dispatch, alternate, false)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ForwardAttempt{}, false, ctxErr
			}
			if errors.Is(err, ErrCodexPoolCredentialInvalid) {
				return ForwardAttempt{}, false, nil
			}
			return ForwardAttempt{}, false, err
		}
		if promoter, ok := a.resolver.(codexPoolQuotaPromoter); ok {
			input := selectionInput
			alternateAccountID := alternate.AccountID
			retryAttempt.CommitQuotaRetry = func() {
				promoter.PromoteQuotaAlternate(input, excludedAccountID, alternateAccountID)
			}
		}
		return retryAttempt, true, nil
	}
	attempt.RetryQuota = resolveAlternate
	attempt.RetryModel400 = func(ctx context.Context) (ForwardAttempt, bool, error) {
		retryAttempt, retry, err := resolveAlternate(ctx)
		if retry {
			retryAttempt.CommitQuotaRetry = nil
		}
		return retryAttempt, retry, err
	}
	return attempt, nil
}

func codexPoolRetryUnavailable(err error) bool {
	if errors.Is(err, codexauth.ErrPoolNoUsableAccount) ||
		errors.Is(err, codexauth.ErrPoolSelectedCredentialUnavailable) ||
		errors.Is(err, codexauth.ErrPoolOutcomeAwareTransportRequired) {
		return true
	}
	var affinityExpired *codexauth.PoolAffinityExpiredError
	if errors.As(err, &affinityExpired) {
		return true
	}
	var materialization *codexauth.PoolCredentialMaterializationError
	return errors.As(err, &materialization)
}

func (a *CodexPoolAuthority) resolvePoolCredential(
	ctx context.Context,
	dispatch providers.DispatchRequest,
	outcomeAware bool,
	excludeAccountID string,
	forceMainRefresh bool,
	forceStoredAccountRefresh bool,
	refreshAccountID string,
) (codexauth.PoolCredential, codexauth.PoolSelectionInput, error) {
	if err := ctx.Err(); err != nil {
		return codexauth.PoolCredential{}, codexauth.PoolSelectionInput{}, err
	}
	modelID := strings.TrimSpace(dispatch.Parsed.UpstreamModelID)
	if modelID == "" {
		modelID = strings.TrimSpace(dispatch.Parsed.ModelID)
	}
	identity := CodexPoolDispatchIdentity{
		ModelID:  modelID,
		ThreadID: dispatch.ForwardHeaders.Get("x-codex-parent-thread-id"),
	}
	request, err := a.snapshot.Snapshot(ctx, identity)
	if err != nil {
		return codexauth.PoolCredential{}, codexauth.PoolSelectionInput{}, err
	}
	// The live dispatch owns thread identity. Snapshot providers may decide model
	// scope/config from identity, but cannot substitute a different caller thread.
	request.Selection.ThreadID = identity.ThreadID
	fixedAccountID := normalizeFixedCodexAccountID(dispatch.CodexAccountID)
	if fixedAccountID == "" {
		if pin := dispatch.PhysicalPin; pin != nil {
			fixedAccountID = normalizeFixedCodexAccountID(pin.CredentialRef)
		}
	}
	if fixedAccountID != "" {
		request.FixedAccountID = fixedAccountID
		request.Selection.ExcludeAccountID = ""
		request.Alternate = false
	} else {
		request.Selection.ExcludeAccountID = excludeAccountID
		request.Alternate = excludeAccountID != ""
	}
	if forceStoredAccountRefresh {
		target := strings.TrimSpace(refreshAccountID)
		if target != "" && target != codexauth.MainAccountID {
			request.FixedAccountID = target
			request.Selection.ExcludeAccountID = ""
			request.Alternate = false
		}
	}
	request.OutcomeAware = outcomeAware
	request.ForceMainRefresh = forceMainRefresh
	request.ForceStoredAccountRefresh = forceStoredAccountRefresh
	request.RefreshAccountID = strings.TrimSpace(refreshAccountID)
	credential, err := a.resolver.Resolve(ctx, request)
	return credential, request.Selection, err
}

func normalizeFixedCodexAccountID(accountID string) string {
	accountID = strings.TrimSpace(accountID)
	if accountID == "@main" {
		return codexauth.MainAccountID
	}
	return accountID
}

func forwardCredentialFromPool(credential codexauth.PoolCredential) (ForwardCredential, error) {
	accessToken := credential.AccessToken
	if strings.TrimSpace(accessToken) == "" || strings.ContainsAny(accessToken, " \t\r\n") {
		return ForwardCredential{}, ErrCodexPoolCredentialInvalid
	}
	return ForwardCredential{
		Authorization:    "Bearer " + accessToken,
		ChatGPTAccountID: credential.ChatGPTAccountID,
		TrustedAccountID: strings.TrimSpace(credential.AccountID),
		UsageAccount:     strings.TrimSpace(credential.UsageAccount),
	}, nil
}

type codexPoolAttemptObserver struct {
	outcomes                    codexPoolOutcomeRecorder
	quotas                      codexPoolQuotaRecorder
	credential                  codexauth.PoolCredential
	pendingSameAccountAuthRetry bool
}

func newCodexPoolAttemptObserver(
	outcomes codexPoolOutcomeRecorder,
	credential codexauth.PoolCredential,
) *codexPoolAttemptObserver {
	return newCodexPoolAttemptObserverWithQuota(outcomes, nil, credential)
}

func newCodexPoolAttemptObserverWithQuota(
	outcomes codexPoolOutcomeRecorder,
	quotas codexPoolQuotaRecorder,
	credential codexauth.PoolCredential,
) *codexPoolAttemptObserver {
	return &codexPoolAttemptObserver{outcomes: outcomes, quotas: quotas, credential: credential}
}

func (o *codexPoolAttemptObserver) ObserveQuota(headers ForwardQuotaHeaders) {
	if o.quotas == nil {
		return
	}
	o.quotas.Record(o.credential, codexauth.UpstreamQuotaHeaders{
		PrimaryUsedPercent:     headers.PrimaryUsedPercent,
		SecondaryUsedPercent:   headers.SecondaryUsedPercent,
		TertiaryUsedPercent:    headers.TertiaryUsedPercent,
		PrimaryResetAt:         headers.PrimaryResetAt,
		SecondaryResetAt:       headers.SecondaryResetAt,
		TertiaryResetAt:        headers.TertiaryResetAt,
		PrimaryWindowMinutes:   headers.PrimaryWindowMinutes,
		SecondaryWindowMinutes: headers.SecondaryWindowMinutes,
	})
}

func (o *codexPoolAttemptObserver) Observe(forward ForwardOutcome) {
	var outcome codexauth.UpstreamOutcome
	switch forward.Kind {
	case ForwardOutcomeHTTP:
		outcome = codexauth.HTTPOutcome(forward.StatusCode)
	case ForwardOutcomeTransportError:
		kind := codexauth.OutcomeConnectError
		if forward.TimedOut {
			kind = codexauth.OutcomeTimeout
		}
		outcome = codexauth.UpstreamOutcome{Kind: kind}
	case ForwardOutcomeCompleted:
		outcome = codexauth.HTTPOutcome(200)
	case ForwardOutcomeFailed, ForwardOutcomeIncomplete:
		outcome = codexauth.HTTPOutcome(502)
	default:
		return
	}
	if o.pendingSameAccountAuthRetry && forward.Kind == ForwardOutcomeHTTP && forward.StatusCode == http.StatusUnauthorized {
		o.pendingSameAccountAuthRetry = false
		o.record(codexauth.UpstreamOutcome{Kind: codexauth.OutcomeConnectNeutral}, "", nil, "")
		return
	}
	o.record(outcome, forward.RetryAfter, forward.ResetAt, codexOutcomeDenial(forward.Denial))
}

func (o *codexPoolAttemptObserver) Abandon() {
	o.record(codexauth.UpstreamOutcome{Kind: codexauth.OutcomeConnectNeutral}, "", nil, "")
}

func (o *codexPoolAttemptObserver) record(
	outcome codexauth.UpstreamOutcome,
	retryAfter string,
	resetAt []string,
	denial codexauth.OutcomeDenial,
) {
	var resetValues []any
	if len(resetAt) > 0 {
		resetValues = make([]any, len(resetAt))
		for index, value := range resetAt {
			resetValues[index] = value
		}
	}
	o.outcomes.Record(o.credential.AccountID, outcome, codexauth.OutcomeMeta{
		Denial:            denial,
		RetryAfter:        retryAfter,
		ResetAt:           resetValues,
		Scope:             o.credential.Scope,
		ProbeLease:        o.credential.ProbeLease,
		FixedAccount:      o.credential.FixedAccount,
		WriterGeneration:  o.credential.WriterGeneration,
		FailoverThreshold: o.credential.FailoverThreshold,
		MainIdentity:      o.credential.MainIdentity,
	})
}

func codexOutcomeDenial(denial ForwardDenial) codexauth.OutcomeDenial {
	switch denial {
	case ForwardDenialWorkspace:
		return codexauth.DenialWorkspace
	case ForwardDenialEntitlement:
		return codexauth.DenialEntitlement
	default:
		return ""
	}
}

var _ ForwardCredentialAuthority = (*CodexPoolAuthority)(nil)
var _ ForwardAttemptAuthority = (*CodexPoolAuthority)(nil)
var _ ForwardOutcomeObserver = (*codexPoolAttemptObserver)(nil)
var _ ForwardQuotaObserver = (*codexPoolAttemptObserver)(nil)
var _ ForwardOutcomeAbandoner = (*codexPoolAttemptObserver)(nil)
