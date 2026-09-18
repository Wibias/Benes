package codexauth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Wibias/Benes/internal/transport"
)

const (
	managedTokenRefreshEndpoint = "https://auth.openai.com/oauth/token"
	managedTokenClientID        = "app_EMoamEEZ73f0CkXaXp7hrann"
	managedTokenMaxBodyBytes    = 1 << 20
	defaultMaxRefreshFlights    = 32
	defaultManagedRefreshSkew   = 60 * time.Second
	defaultManagedRefreshWait   = 65 * time.Second
	defaultManagedRefreshHTTP   = 30 * time.Second
)

var (
	ErrManagedCredentialUnavailable = errors.New("managed Codex credential is unavailable; reauthenticate the account")
	ErrManagedCredentialRefreshBusy = errors.New("managed Codex credential refresh capacity reached")
	ErrManagedCredentialRefreshData = errors.New("managed Codex token refresh returned invalid data")
)

type ManagedRefreshReason string

const (
	ManagedRefreshExpired ManagedRefreshReason = "expired"
	ManagedRefreshRevoked ManagedRefreshReason = "revoked"
	ManagedRefreshUnknown ManagedRefreshReason = "unknown"
)

type ManagedTokenRefreshError struct {
	Reason ManagedRefreshReason
}

func (e *ManagedTokenRefreshError) Error() string {
	return fmt.Sprintf("managed Codex token refresh failed (%s); reauthenticate the account", e.Reason)
}

type ManagedToken struct {
	AccessToken      string
	ChatGPTAccountID string
	Generation       int64
}

type ManagedTokenSourceConfig struct {
	Store              *ManagedCredentialStore
	HTTPClient         *http.Client
	Now                func() time.Time
	RefreshSkew        time.Duration
	RefreshLockWait    time.Duration
	RefreshHTTPTimeout time.Duration
	MaxRefreshFlights  int
}

type managedRefreshResult struct {
	token         ManagedToken
	credential    ManagedCredential
	hasCredential bool
}

type managedRefreshFlight struct {
	done       chan struct{}
	result     managedRefreshResult
	err        error
	waiters    int
	cancelLock context.CancelFunc
}

type ManagedTokenSource struct {
	store              *ManagedCredentialStore
	httpClient         *http.Client
	now                func() time.Time
	refreshSkew        time.Duration
	refreshLockWait    time.Duration
	refreshHTTPTimeout time.Duration
	maxRefreshFlights  int

	mu      sync.Mutex
	flights map[string]*managedRefreshFlight
}

func NewManagedTokenSource(config ManagedTokenSourceConfig) (*ManagedTokenSource, error) {
	if config.Store == nil {
		return nil, fmt.Errorf("managed credential store is required")
	}
	client := config.HTTPClient
	if client == nil {
		client = transport.DefaultUnpinnedClient()
	}
	clientCopy := *client
	clientCopy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.RefreshSkew <= 0 {
		config.RefreshSkew = defaultManagedRefreshSkew
	}
	if config.RefreshLockWait <= 0 {
		config.RefreshLockWait = defaultManagedRefreshWait
	}
	if config.RefreshHTTPTimeout <= 0 {
		config.RefreshHTTPTimeout = defaultManagedRefreshHTTP
	}
	if config.MaxRefreshFlights <= 0 {
		config.MaxRefreshFlights = defaultMaxRefreshFlights
	}
	return &ManagedTokenSource{
		store:              config.Store,
		httpClient:         &clientCopy,
		now:                config.Now,
		refreshSkew:        config.RefreshSkew,
		refreshLockWait:    config.RefreshLockWait,
		refreshHTTPTimeout: config.RefreshHTTPTimeout,
		maxRefreshFlights:  config.MaxRefreshFlights,
		flights:            make(map[string]*managedRefreshFlight),
	}, nil
}

func (s *ManagedTokenSource) Get(ctx context.Context, accountID string) (ManagedToken, error) {
	record, err := s.loadRecord(accountID)
	if err != nil {
		return ManagedToken{}, err
	}
	now := s.now()
	if managedCredentialFresh(*record.Credential, now, s.refreshSkew) {
		return managedTokenFromRecord(record), nil
	}
	return s.refreshThroughFlightForced(ctx, accountID, record, false)
}

func (s *ManagedTokenSource) ForceRefresh(ctx context.Context, accountID string) (ManagedToken, error) {
	record, err := s.loadRecord(accountID)
	if err != nil {
		return ManagedToken{}, err
	}
	return s.refreshThroughFlightForced(ctx, accountID, record, true)
}

func (s *ManagedTokenSource) refreshThroughFlightForced(ctx context.Context, accountID string, record ManagedCredentialRecord, force bool) (ManagedToken, error) {
	fingerprint := record.RefreshGrantFingerprint
	if fingerprint == "" {
		fingerprint = RefreshGrantFingerprintForToken(record.Credential.RefreshToken)
	}

	s.mu.Lock()
	if flight := s.flights[fingerprint]; flight != nil {
		flight.waiters++
		s.mu.Unlock()
		return s.waitForFlight(ctx, accountID, fingerprint, flight)
	}
	if len(s.flights) >= s.maxRefreshFlights {
		s.mu.Unlock()
		return ManagedToken{}, ErrManagedCredentialRefreshBusy
	}
	lockCtx, cancelLock := context.WithTimeout(context.Background(), s.refreshLockWait)
	flight := &managedRefreshFlight{done: make(chan struct{}), waiters: 1, cancelLock: cancelLock}
	s.flights[fingerprint] = flight
	s.mu.Unlock()

	go s.runRefreshFlight(lockCtx, accountID, fingerprint, flight, force)
	return s.waitForFlight(ctx, accountID, fingerprint, flight)
}

func (s *ManagedTokenSource) runRefreshFlight(
	lockCtx context.Context,
	accountID string,
	fingerprint string,
	flight *managedRefreshFlight,
	force bool,
) {
	lock, err := acquireManagedStoreMutationLock(lockCtx, s.refreshLockPath(fingerprint))
	if err != nil {
		s.finishRefreshFlight(fingerprint, flight, managedRefreshResult{}, err)
		return
	}
	defer lock.Close()
	if s.flightWaiters(flight) == 0 {
		s.finishRefreshFlight(fingerprint, flight, managedRefreshResult{}, context.Canceled)
		return
	}

	workTimeout := s.refreshHTTPTimeout + s.refreshLockWait + 5*time.Second
	workCtx, cancelWork := context.WithTimeout(context.Background(), workTimeout)
	result, refreshErr := s.refreshGrantLocked(workCtx, accountID, fingerprint, force)
	cancelWork()
	s.finishRefreshFlight(fingerprint, flight, result, refreshErr)
}

func (s *ManagedTokenSource) flightWaiters(flight *managedRefreshFlight) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return flight.waiters
}

func (s *ManagedTokenSource) leaveFlight(flight *managedRefreshFlight) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if flight.waiters > 0 {
		flight.waiters--
	}
	if flight.waiters != 0 || flight.cancelLock == nil {
		return
	}
	select {
	case <-flight.done:
	default:
		flight.cancelLock()
	}
}

func (s *ManagedTokenSource) finishRefreshFlight(
	fingerprint string,
	flight *managedRefreshFlight,
	result managedRefreshResult,
	err error,
) {
	s.mu.Lock()
	if flight.cancelLock != nil {
		flight.cancelLock()
		flight.cancelLock = nil
	}
	flight.result = result
	flight.err = err
	if s.flights[fingerprint] == flight {
		delete(s.flights, fingerprint)
	}
	close(flight.done)
	s.mu.Unlock()
}

func (s *ManagedTokenSource) waitForFlight(
	ctx context.Context,
	accountID string,
	fingerprint string,
	flight *managedRefreshFlight,
) (ManagedToken, error) {
	defer s.leaveFlight(flight)
	select {
	case <-ctx.Done():
		return ManagedToken{}, ctx.Err()
	case <-flight.done:
	}
	if flight.err != nil {
		return ManagedToken{}, flight.err
	}
	if !flight.result.hasCredential {
		return s.Get(ctx, accountID)
	}
	return s.adoptSharedCredential(ctx, accountID, fingerprint, flight.result.credential)
}

func (s *ManagedTokenSource) adoptSharedCredential(
	ctx context.Context,
	accountID string,
	fingerprint string,
	credential ManagedCredential,
) (ManagedToken, error) {
	current, err := s.loadRecord(accountID)
	if err != nil {
		return ManagedToken{}, err
	}
	now := s.now()
	if managedCredentialFresh(*current.Credential, now, s.refreshSkew) {
		return managedTokenFromRecord(current), nil
	}
	if current.RefreshGrantFingerprint != fingerprint {
		return ManagedToken{}, ErrManagedCredentialGenerationConflict
	}
	generation, err := s.store.CompareAndSwap(ctx, accountID, current.Generation, credential)
	if err != nil {
		if errors.Is(err, ErrManagedCredentialGenerationConflict) {
			latest, loadErr := s.loadRecord(accountID)
			if loadErr == nil && managedCredentialFresh(*latest.Credential, s.now(), s.refreshSkew) {
				return managedTokenFromRecord(latest), nil
			}
		}
		return ManagedToken{}, err
	}
	return ManagedToken{
		AccessToken:      credential.AccessToken,
		ChatGPTAccountID: credential.ChatGPTAccountID,
		Generation:       generation,
	}, nil
}

func (s *ManagedTokenSource) refreshGrantLocked(
	ctx context.Context,
	accountID string,
	fingerprint string,
	force bool,
) (managedRefreshResult, error) {
	current, err := s.loadRecord(accountID)
	if err != nil {
		return managedRefreshResult{}, err
	}
	now := s.now()
	if current.RefreshGrantFingerprint != fingerprint {
		if managedCredentialFresh(*current.Credential, now, s.refreshSkew) {
			return refreshResultFromRecord(current), nil
		}
		return managedRefreshResult{}, ErrManagedCredentialGenerationConflict
	}
	if !force && managedCredentialFresh(*current.Credential, now, s.refreshSkew) {
		return refreshResultFromRecord(current), nil
	}

	if !force {
		if shared, ok := s.findFreshCredentialForGrant(fingerprint, accountID, now); ok {
			generation, err := s.store.CompareAndSwap(ctx, accountID, current.Generation, shared)
			if err != nil {
				return managedRefreshResult{}, err
			}
			return managedRefreshResult{
				token:         ManagedToken{AccessToken: shared.AccessToken, ChatGPTAccountID: shared.ChatGPTAccountID, Generation: generation},
				credential:    shared,
				hasCredential: true,
			}, nil
		}
	}

	updated, err := s.requestRefresh(ctx, *current.Credential, now)
	if err != nil {
		return managedRefreshResult{}, err
	}
	generation, err := s.store.CompareAndSwap(ctx, accountID, current.Generation, updated)
	if err != nil {
		return managedRefreshResult{}, err
	}
	return managedRefreshResult{
		token:         ManagedToken{AccessToken: updated.AccessToken, ChatGPTAccountID: updated.ChatGPTAccountID, Generation: generation},
		credential:    updated,
		hasCredential: true,
	}, nil
}

func (s *ManagedTokenSource) loadRecord(accountID string) (ManagedCredentialRecord, error) {
	snapshot := s.store.Read()
	switch snapshot.Status {
	case ManagedCredentialStoreOK:
	case ManagedCredentialStoreInvalid:
		return ManagedCredentialRecord{}, ErrManagedCredentialStoreInvalid
	case ManagedCredentialStoreUnreadable:
		return ManagedCredentialRecord{}, ErrManagedCredentialStoreUnreadable
	default:
		return ManagedCredentialRecord{}, ErrManagedCredentialUnavailable
	}
	record, exists := snapshot.Records[accountID]
	if !exists || record.Credential == nil || record.DeletedAtMS != nil {
		return ManagedCredentialRecord{}, ErrManagedCredentialUnavailable
	}
	return record, nil
}

func (s *ManagedTokenSource) findFreshCredentialForGrant(
	fingerprint string,
	excludeID string,
	now time.Time,
) (ManagedCredential, bool) {
	snapshot := s.store.Read()
	if snapshot.Status != ManagedCredentialStoreOK {
		return ManagedCredential{}, false
	}
	for accountID, record := range snapshot.Records {
		if accountID == excludeID || record.Credential == nil || record.DeletedAtMS != nil {
			continue
		}
		if record.RefreshGrantFingerprint != fingerprint {
			continue
		}
		if managedCredentialFresh(*record.Credential, now, s.refreshSkew) {
			return *record.Credential, true
		}
	}
	return ManagedCredential{}, false
}

func (s *ManagedTokenSource) requestRefresh(
	ctx context.Context,
	credential ManagedCredential,
	now time.Time,
) (ManagedCredential, error) {
	refreshCtx, cancel := context.WithTimeout(ctx, s.refreshHTTPTimeout)
	defer cancel()
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {managedTokenClientID},
		"refresh_token": {credential.RefreshToken},
	}
	request, err := http.NewRequestWithContext(
		refreshCtx,
		http.MethodPost,
		managedTokenRefreshEndpoint,
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return ManagedCredential{}, fmt.Errorf("build managed Codex token refresh request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := s.httpClient.Do(request)
	if err != nil {
		return ManagedCredential{}, fmt.Errorf("managed Codex token refresh request failed: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, managedTokenMaxBodyBytes+1))
	if err != nil {
		return ManagedCredential{}, fmt.Errorf("read managed Codex token refresh response: %w", err)
	}
	if len(body) > managedTokenMaxBodyBytes {
		return ManagedCredential{}, ErrManagedCredentialRefreshData
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return ManagedCredential{}, &ManagedTokenRefreshError{Reason: classifyManagedRefreshFailure(body)}
	}

	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var payload map[string]any
	if err := decoder.Decode(&payload); err != nil || payload == nil {
		return ManagedCredential{}, ErrManagedCredentialRefreshData
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return ManagedCredential{}, ErrManagedCredentialRefreshData
	}
	accessToken, ok := payload["access_token"].(string)
	if !ok || strings.TrimSpace(accessToken) == "" {
		return ManagedCredential{}, ErrManagedCredentialRefreshData
	}
	refreshToken := credential.RefreshToken
	if raw, exists := payload["refresh_token"]; exists && raw != nil {
		value, ok := raw.(string)
		if !ok {
			return ManagedCredential{}, ErrManagedCredentialRefreshData
		}
		refreshToken = value
	}
	expiresIn := parseManagedExpiresIn(payload["expires_in"])
	return ManagedCredential{
		AccessToken:      accessToken,
		RefreshToken:     refreshToken,
		ExpiresAtMS:      safeManagedExpiryMillis(now, expiresIn),
		ChatGPTAccountID: credential.ChatGPTAccountID,
	}, nil
}

func (s *ManagedTokenSource) refreshLockPath(fingerprint string) string {
	digest := sha256.Sum256([]byte(fingerprint))
	name := "codex-refresh-" + hex.EncodeToString(digest[:])[:32] + ".lock"
	return filepath.Join(filepath.Dir(s.store.path), name)
}

func managedCredentialFresh(credential ManagedCredential, now time.Time, skew time.Duration) bool {
	return credential.ExpiresAtMS > now.Add(skew).UnixMilli()
}

func managedTokenFromRecord(record ManagedCredentialRecord) ManagedToken {
	return ManagedToken{
		AccessToken:      record.Credential.AccessToken,
		ChatGPTAccountID: record.Credential.ChatGPTAccountID,
		Generation:       record.Generation,
	}
}

func refreshResultFromRecord(record ManagedCredentialRecord) managedRefreshResult {
	credential := *record.Credential
	return managedRefreshResult{
		token: ManagedToken{
			AccessToken:      credential.AccessToken,
			ChatGPTAccountID: credential.ChatGPTAccountID,
			Generation:       record.Generation,
		},
		credential:    credential,
		hasCredential: true,
	}
}

func parseManagedExpiresIn(raw any) float64 {
	const fallback = 3600.0
	number, ok := raw.(json.Number)
	if !ok {
		return fallback
	}
	value, err := number.Float64()
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return fallback
	}
	return value
}

func safeManagedExpiryMillis(now time.Time, expiresInSeconds float64) int64 {
	candidate := float64(now.UnixMilli()) + expiresInSeconds*1000
	if math.IsNaN(candidate) || math.IsInf(candidate, 0) || candidate > math.MaxInt64 || candidate < math.MinInt64 {
		return now.Add(time.Hour).UnixMilli()
	}
	return int64(candidate)
}

func classifyManagedRefreshFailure(body []byte) ManagedRefreshReason {
	var payload struct {
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	_ = json.Unmarshal(body, &payload)
	text := strings.ToLower(payload.Error + " " + payload.ErrorDescription)
	switch {
	case strings.Contains(text, "invalidated"), strings.Contains(text, "revoked"):
		return ManagedRefreshRevoked
	case strings.Contains(text, "expired"):
		return ManagedRefreshExpired
	default:
		return ManagedRefreshUnknown
	}
}
