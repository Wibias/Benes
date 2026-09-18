package codexauth

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"
)

const defaultMainQuotaTTL = 5 * time.Minute

type MainQuotaSnapshot struct {
	Identity  string
	Plan      string
	Quota     *QuotaReading
	UpdatedAt time.Time
}

type MainQuotaState struct {
	mu       sync.RWMutex
	snapshot MainQuotaSnapshot
}

func NewMainQuotaState() *MainQuotaState { return &MainQuotaState{} }

func (s *MainQuotaState) Publish(identity, plan string, quota *QuotaReading, now time.Time) bool {
	identity = strings.TrimSpace(identity)
	if identity == "" {
		return false
	}
	if now.IsZero() {
		now = time.Now()
	}
	s.mu.Lock()
	s.snapshot = MainQuotaSnapshot{
		Identity:  identity,
		Plan:      strings.TrimSpace(plan),
		Quota:     cloneOptionalQuotaReading(quota),
		UpdatedAt: now,
	}
	s.mu.Unlock()
	return true
}

func (s *MainQuotaState) Snapshot(identity string) MainQuotaSnapshot {
	identity = strings.TrimSpace(identity)
	if identity == "" {
		return MainQuotaSnapshot{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.snapshot.Identity != identity {
		return MainQuotaSnapshot{}
	}
	return cloneMainQuotaSnapshot(s.snapshot)
}

func (s *MainQuotaState) Clear(identity string) bool {
	identity = strings.TrimSpace(identity)
	if identity == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.snapshot.Identity != identity {
		return false
	}
	s.snapshot = MainQuotaSnapshot{}
	return true
}

func cloneMainQuotaSnapshot(snapshot MainQuotaSnapshot) MainQuotaSnapshot {
	return MainQuotaSnapshot{
		Identity:  snapshot.Identity,
		Plan:      snapshot.Plan,
		Quota:     cloneOptionalQuotaReading(snapshot.Quota),
		UpdatedAt: snapshot.UpdatedAt,
	}
}

func cloneOptionalQuotaReading(reading *QuotaReading) *QuotaReading {
	if reading == nil {
		return nil
	}
	cloned := cloneQuotaReading(*reading)
	return &cloned
}

type MainQuotaCredentialReader interface {
	Read(time.Time) MainCredentialResult
}

type MainQuotaFetcher interface {
	FetchMain(context.Context, ManagedToken, string) (WHAMFetchResult, error)
}

type MainQuotaPrimerConfig struct {
	Credentials      MainQuotaCredentialReader
	Fetcher          MainQuotaFetcher
	State            *MainQuotaState
	Reauth           *ReauthState
	WriterGeneration uint64
	ConfiguredPlan   string
	Now              func() time.Time
	CacheTTL         time.Duration
}

type mainQuotaPrimeFlight struct {
	done chan struct{}
}

type MainQuotaPrimer struct {
	credentials      MainQuotaCredentialReader
	fetcher          MainQuotaFetcher
	state            *MainQuotaState
	reauth           *ReauthState
	writerGeneration uint64
	configuredPlan   string
	now              func() time.Time
	cacheTTL         time.Duration

	mu     sync.Mutex
	flight *mainQuotaPrimeFlight
}

func NewMainQuotaPrimer(config MainQuotaPrimerConfig) (*MainQuotaPrimer, error) {
	if config.Credentials == nil || config.Fetcher == nil || config.State == nil || config.Reauth == nil {
		return nil, &MainQuotaConfigError{}
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.CacheTTL <= 0 {
		config.CacheTTL = defaultMainQuotaTTL
	}
	return &MainQuotaPrimer{
		credentials:      config.Credentials,
		fetcher:          config.Fetcher,
		state:            config.State,
		reauth:           config.Reauth,
		writerGeneration: config.WriterGeneration,
		configuredPlan:   strings.TrimSpace(config.ConfiguredPlan),
		now:              config.Now,
		cacheTTL:         config.CacheTTL,
	}, nil
}

type MainQuotaConfigError struct{}

func (*MainQuotaConfigError) Error() string { return "physical main quota primer dependencies are required" }

func (p *MainQuotaPrimer) Prime(ctx context.Context) {
	if ctx == nil || ctx.Err() != nil {
		return
	}
	p.mu.Lock()
	if current := p.flight; current != nil {
		p.mu.Unlock()
		select {
		case <-current.done:
		case <-ctx.Done():
		}
		return
	}
	flight := &mainQuotaPrimeFlight{done: make(chan struct{})}
	p.flight = flight
	p.mu.Unlock()

	p.prime(ctx, 1)

	p.mu.Lock()
	if p.flight == flight {
		p.flight = nil
	}
	close(flight.done)
	p.mu.Unlock()
}

func (p *MainQuotaPrimer) prime(ctx context.Context, retriesRemaining int) {
	if ctx.Err() != nil {
		return
	}
	now := p.now()
	credential := p.credentials.Read(now)
	if !usableMainQuotaCredential(credential) {
		return
	}

	cached := p.state.Snapshot(credential.Identity)
	if cached.Identity != "" && now.Sub(cached.UpdatedAt) < p.cacheTTL {
		return
	}
	fallbackPlan := cached.Plan
	if fallbackPlan == "" {
		fallbackPlan = p.configuredPlan
	}

	result, err := p.fetcher.FetchMain(ctx, ManagedToken{
		AccessToken:       credential.Credential.AccessToken,
		ChatGPTAccountID: credential.Credential.ChatGPTAccountID,
	}, fallbackPlan)
	if !p.identityStillLive(credential.Identity) {
		if retriesRemaining > 0 {
			p.prime(ctx, retriesRemaining-1)
		}
		return
	}
	if err != nil {
		return
	}
	if result.StatusCode < http.StatusOK || result.StatusCode >= http.StatusMultipleChoices {
		if result.TerminalMainAuth {
			p.state.Clear(credential.Identity)
			p.reauth.Mark(MainAccountID, p.writerGeneration)
		}
		return
	}

	plan := strings.TrimSpace(result.Quota.Plan)
	if plan == "" {
		plan = fallbackPlan
	}
	p.state.Publish(credential.Identity, plan, result.Quota.Quota, p.now())
	p.reauth.Clear(MainAccountID)
}

func (p *MainQuotaPrimer) identityStillLive(identity string) bool {
	current := p.credentials.Read(p.now())
	return current.Status == MainCredentialOK && current.Identity != "" && current.Identity == identity
}

func usableMainQuotaCredential(result MainCredentialResult) bool {
	return result.Status == MainCredentialOK &&
		strings.TrimSpace(result.Identity) != "" &&
		strings.TrimSpace(result.Credential.AccessToken) != "" &&
		strings.TrimSpace(result.Credential.ChatGPTAccountID) != ""
}
