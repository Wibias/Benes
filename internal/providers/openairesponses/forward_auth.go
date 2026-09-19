package openairesponses

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/resourcebudget"
	"github.com/Wibias/Benes/internal/responses/continuation"
	"github.com/Wibias/Benes/internal/responses/sse"
	"github.com/Wibias/Benes/internal/transport"
)

const canonicalForwardResponsesEndpoint = "https://chatgpt.com/backend-api/codex/responses"

type ForwardConfig struct {
	Endpoint            string
	ProviderID          string
	HTTPClient          *http.Client
	MaxStreamBytes      int64
	InactivityTimeout   time.Duration
	MaxSSELineBytes     int
	MaxSSEEventBytes    int
	DestinationPolicy   transport.DestinationPolicy
	TransportOptions    transport.ClientOptions
	CredentialAuthority ForwardCredentialAuthority
	Continuation        *continuation.Authority
}

type ForwardClient struct {
	endpoint            string
	destination         string
	providerID          string
	nativeForward       bool
	httpClient          *http.Client
	maxStreamBytes      int64
	inactivityTimeout   time.Duration
	sseLimits           sse.Limits
	credentialAuthority ForwardCredentialAuthority
	continuation        *continuation.Authority
}

func NewForward(config ForwardConfig) (*ForwardClient, error) {
	endpoint, err := normalizeForwardEndpoint(config.Endpoint)
	if err != nil {
		return nil, err
	}
	if config.HTTPClient == nil {
		return nil, fmt.Errorf("hardened HTTP client is required")
	}
	if config.MaxStreamBytes <= 0 {
		config.MaxStreamBytes = 64 << 20
	}
	if config.InactivityTimeout <= 0 {
		config.InactivityTimeout = 5 * time.Minute
	}
	authority := config.CredentialAuthority
	if authority == nil {
		authority = CallerForwardAuthority{}
	}
	providerID := strings.TrimSpace(config.ProviderID)
	if providerID == "" {
		providerID = "openai"
	}
	return &ForwardClient{
		endpoint:          endpoint,
		destination:       endpoint,
		providerID:        providerID,
		nativeForward:     nativeCodexForwardDestination(endpoint),
		httpClient:        config.HTTPClient,
		maxStreamBytes:    config.MaxStreamBytes,
		inactivityTimeout: config.InactivityTimeout,
		sseLimits: sse.Limits{
			MaxLineBytes:  config.MaxSSELineBytes,
			MaxEventBytes: config.MaxSSEEventBytes,
		},
		credentialAuthority: authority,
		continuation:        config.Continuation,
	}, nil
}

func NewForwardHardened(ctx context.Context, config ForwardConfig) (*ForwardClient, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	endpoint, err := normalizeForwardEndpoint(config.Endpoint)
	if err != nil {
		return nil, err
	}
	config.Endpoint = endpoint
	config.HTTPClient = newDeferredForwardHTTPClient(endpoint, config.DestinationPolicy, config.TransportOptions)
	return NewForward(config)
}

func (c *ForwardClient) Open(ctx context.Context, dispatch providers.DispatchRequest) (providers.EventStream, error) {
	attempt, err := resolveForwardAttempt(ctx, c.credentialAuthority, dispatch)
	if err != nil {
		return nil, err
	}
	stream, bound, err := c.openAttempt(ctx, dispatch, attempt, true)
	if err != nil {
		return nil, err
	}
	attachContinuationPersistence(stream, c.continuation, bound, continuationThread(dispatch))
	return wrapContinuationStream(stream, bound), nil
}

func (c *ForwardClient) openAttempt(
	ctx context.Context,
	dispatch providers.DispatchRequest,
	attempt ForwardAttempt,
	allowAccountRetry bool,
) (providers.EventStream, continuation.Bound, error) {
	credential := attempt.Credential
	if strings.TrimSpace(credential.Authorization) == "" {
		abandonForwardObserver(attempt.Observer)
		return nil, continuation.Bound{}, ErrForwardAuthorizationRequired
	}
	bound, err := bindDispatchContinuation(c.continuation, dispatch, continuation.PhysicalIdentity{
		Provider:      c.providerID,
		Destination:   c.destination,
		Adapter:       "openai-responses",
		Model:         firstNonEmptyThread(dispatch.Parsed.UpstreamModelID, dispatch.Parsed.ModelID),
		AuthClass:     "forward",
		Secret:        []byte(credential.Authorization),
		AccountHandle: credential.TrustedAccountID,
	}, c.nativeForward)
	if err != nil {
		abandonForwardObserver(attempt.Observer)
		return nil, continuation.Bound{}, err
	}
	body, err := prepareForwardCanonicalBody(preferNativePreviousWithoutReplay(bound, dispatch.Parsed), c.endpoint, dispatch.ConfiguredServiceTier)
	if err != nil {
		bound.Release()
		abandonForwardObserver(attempt.Observer)
		return nil, continuation.Bound{}, err
	}
	requestHeaders := dispatch.ForwardHeaders

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		bound.Release()
		abandonForwardObserver(attempt.Observer)
		return nil, continuation.Bound{}, fmt.Errorf("build OpenAI Responses forward request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	for name, value := range requestHeaders.Values() {
		switch strings.ToLower(name) {
		case "authorization", "chatgpt-account-id":
			continue
		default:
			req.Header.Set(name, value)
		}
	}
	req.Header.Set("Authorization", credential.Authorization)
	if credential.ChatGPTAccountID != "" {
		req.Header.Set("ChatGPT-Account-Id", credential.ChatGPTAccountID)
	}

	response, err := transport.DoPhysicalSend(ctx, c.httpClient, req, dispatch.Turn, "openai-responses-forward")
	if err != nil {
		bound.Release()
		if errors.Is(err, resourcebudget.ErrPhysicalSendBudgetExceeded) {
			abandonForwardObserver(attempt.Observer)
			return nil, continuation.Bound{}, fmt.Errorf("OpenAI Responses forward request failed: %w", err)
		}
		if attempt.Observer != nil {
			if ctx.Err() != nil {
				abandonForwardObserver(attempt.Observer)
			} else {
				attempt.Observer.Observe(ForwardOutcome{
					Kind:     ForwardOutcomeTransportError,
					TimedOut: errors.Is(err, context.DeadlineExceeded),
				})
			}
		}
		return nil, continuation.Bound{}, fmt.Errorf("OpenAI Responses forward request failed: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var denial ForwardDenial
		var errorBody []byte
		switch {
		case response.StatusCode == http.StatusForbidden:
			denial = classifyForward403Denial(ctx, response.Body)
		case response.StatusCode == http.StatusBadRequest || (response.StatusCode >= 500 && response.StatusCode < 600):
			errorBody = readForwardErrorBody(ctx, response)
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			_ = response.Body.Close()
			bound.Release()
			abandonForwardObserver(attempt.Observer)
			return nil, continuation.Bound{}, ctxErr
		}
		quotaFailure := isForwardQuotaRetry(response.StatusCode, errorBody)
		outcome := ForwardOutcome{
			Kind:       ForwardOutcomeHTTP,
			StatusCode: normalizedForwardQuotaOutcomeStatus(response.StatusCode, quotaFailure),
			RetryAfter: response.Header.Get("Retry-After"),
			ResetAt:    forwardCodexResetAt(response.Header),
			Denial:     denial,
		}
		statusErr := upstreamHTTPError("OpenAI Responses forward upstream", response.StatusCode, errorBody)

		if allowAccountRetry && response.StatusCode == http.StatusBadRequest && attempt.RetryModel400 != nil {
			modelID := strings.TrimSpace(dispatch.Parsed.UpstreamModelID)
			if modelID == "" {
				modelID = strings.TrimSpace(dispatch.Parsed.ModelID)
			}
			if shouldRetryForwardPoolModel400(response.StatusCode, errorBody, modelID) {
				retryAttempt, retry, retryErr := attempt.RetryModel400(ctx)
				if retryErr != nil {
					bound.Release()
					return nil, continuation.Bound{}, retryErr
				}
				if retry {
					if attempt.Observer != nil {
						attempt.Observer.Observe(outcome)
					}
					if !canReplayForwardAccountBoundFiles(dispatch.Parsed, credential, retryAttempt.Credential) {
						abandonForwardObserver(retryAttempt.Observer)
						bound.Release()
						return nil, continuation.Bound{}, statusErr
					}
					bound.Release()
					return c.openAttempt(ctx, dispatch, retryAttempt, false)
				}
				observeForwardQuota(attempt.Observer, response.Header)
				if attempt.Observer != nil {
					attempt.Observer.Observe(outcome)
				}
				bound.Release()
				return nil, continuation.Bound{}, statusErr
			}
		}

		if allowAccountRetry && response.StatusCode == http.StatusUnauthorized && attempt.RetryAuth != nil {
			_ = response.Body.Close()
			retryAttempt, retry, retryErr := attempt.RetryAuth(ctx)
			if retryErr != nil {
				bound.Release()
				return nil, continuation.Bound{}, retryErr
			}
			if attempt.Observer != nil {
				attempt.Observer.Observe(outcome)
			}
			if retry {
				if !canReplayForwardAccountBoundFiles(dispatch.Parsed, credential, retryAttempt.Credential) {
					abandonForwardObserver(retryAttempt.Observer)
					bound.Release()
					return nil, continuation.Bound{}, statusErr
				}
				bound.Release()
				return c.openAttempt(ctx, dispatch, retryAttempt, false)
			}
			bound.Release()
			return nil, continuation.Bound{}, statusErr
		}

		if allowAccountRetry && quotaFailure && attempt.RetryQuota != nil {
			_ = response.Body.Close()
			retryAttempt, retry, retryErr := attempt.RetryQuota(ctx)
			if retryErr != nil {
				bound.Release()
				return nil, continuation.Bound{}, retryErr
			}
			observeForwardQuota(attempt.Observer, response.Header)
			if attempt.Observer != nil {
				attempt.Observer.Observe(outcome)
			}
			if retry {
				if !canReplayForwardAccountBoundFiles(dispatch.Parsed, credential, retryAttempt.Credential) {
					abandonForwardObserver(retryAttempt.Observer)
					bound.Release()
					return nil, continuation.Bound{}, statusErr
				}
				if retryAttempt.CommitQuotaRetry != nil {
					retryAttempt.CommitQuotaRetry()
				}
				bound.Release()
				return c.openAttempt(ctx, dispatch, retryAttempt, false)
			}
			bound.Release()
			return nil, continuation.Bound{}, statusErr
		}
		observeForwardQuota(attempt.Observer, response.Header)
		if attempt.Observer != nil {
			attempt.Observer.Observe(outcome)
		}
		_ = response.Body.Close()
		bound.Release()
		return nil, continuation.Bound{}, statusErr
	}
	observeForwardQuota(attempt.Observer, response.Header)
	mediaType, _, mediaErr := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if mediaErr != nil || mediaType != "text/event-stream" {
		if attempt.Observer != nil {
			attempt.Observer.Observe(ForwardOutcome{Kind: ForwardOutcomeIncomplete})
		}
		_ = response.Body.Close()
		bound.Release()
		return nil, continuation.Bound{}, fmt.Errorf("OpenAI Responses forward upstream must return text/event-stream")
	}

	pipeReader, pipeWriter := io.Pipe()
	copyCtx, cancel := context.WithCancel(ctx)
	go func() {
		_, copyErr := transport.CopyBounded(copyCtx, pipeWriter, response.Body, transport.StreamLimits{
			MaxBytes:          c.maxStreamBytes,
			InactivityTimeout: c.inactivityTimeout,
		})
		_ = pipeWriter.CloseWithError(copyErr)
	}()
	stream := &Stream{
		decoder:  sse.NewDecoder(pipeReader, c.sseLimits),
		reader:   pipeReader,
		cancel:   cancel,
		turn:     dispatch.Turn,
		physical: physicalPinFromForward(c.destination, credential.TrustedAccountID),
	}
	if observer := providers.CommittedUsageAccountObserverFrom(ctx); observer != nil {
		if label := strings.TrimSpace(credential.UsageAccount); label != "" {
			observer.NoteCommittedUsageAccount(providers.CommittedUsageAccount(label))
		}
	}
	return newObservedForwardStream(ctx, stream, attempt.Observer), bound, nil
}

func forwardCodexResetAt(header http.Header) []string {
	names := [...]string{
		"X-Codex-Primary-Reset-At",
		"X-Codex-Secondary-Reset-At",
		"X-Codex-Tertiary-Reset-At",
	}
	values := make([]string, 0, len(names))
	for _, name := range names {
		if value := strings.TrimSpace(header.Get(name)); value != "" {
			values = append(values, value)
		}
	}
	if len(values) == 0 {
		return nil
	}
	return values
}

func normalizeForwardEndpoint(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return canonicalForwardResponsesEndpoint, nil
	}
	parsed, err := url.Parse(value)
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") || !strings.EqualFold(parsed.Hostname(), "chatgpt.com") {
		return "", fmt.Errorf("forward auth requires the canonical ChatGPT Responses endpoint")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("forward auth requires the canonical ChatGPT Responses endpoint")
	}
	if port := parsed.Port(); port != "" && port != "443" {
		return "", fmt.Errorf("forward auth requires the canonical ChatGPT Responses endpoint")
	}
	if strings.TrimRight(parsed.EscapedPath(), "/") != "/backend-api/codex/responses" {
		return "", fmt.Errorf("forward auth requires the canonical ChatGPT Responses endpoint")
	}
	return canonicalForwardResponsesEndpoint, nil
}

func physicalPinFromForward(destination, accountID string) *providers.PhysicalPin {
	return &providers.PhysicalPin{
		Destination:   strings.TrimSpace(destination),
		CredentialRef: strings.TrimSpace(accountID),
		AuthClass:     "forward",
	}
}
