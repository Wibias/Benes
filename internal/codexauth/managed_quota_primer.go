package codexauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	defaultManagedQuotaTTL            = 5 * time.Minute
	defaultManagedQuotaConcurrency    = 4
	defaultManagedQuotaFailureBackoff = 30 * time.Second
)

var managedQuotaAccountIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

type ManagedQuotaTokenProvider interface {
	Get(context.Context, string) (ManagedToken, error)
}

type ManagedQuotaFetcher interface {
	Fetch(context.Context, ManagedToken, string) (WHAMFetchResult, error)
}

type ManagedQuotaCredentialReader interface {
	Read() ManagedCredentialSnapshot
}

type ManagedQuotaPrimerConfig struct {
	Accounts         ManagedAccountConfig
	Credentials      ManagedQuotaCredentialReader
	Tokens           ManagedQuotaTokenProvider
	Fetcher          ManagedQuotaFetcher
	Quotas           *QuotaState
	Reauth           *ReauthState
	WriterGeneration uint64
	Now              func() time.Time
	CacheTTL         time.Duration
	MaxConcurrency   int
}

type managedQuotaPrimeFlight struct {
	done chan struct{}
}

type managedQuotaPrimeFailure struct {
	generation int64
	until      time.Time
}

type ManagedQuotaPrimer struct {
	accounts         ManagedAccountConfig
	credentials      ManagedQuotaCredentialReader
	tokens           ManagedQuotaTokenProvider
	fetcher          ManagedQuotaFetcher
	quotas           *QuotaState
	reauth           *ReauthState
	writerGeneration uint64
	now              func() time.Time
	cacheTTL         time.Duration
	maxConcurrency   int
	failureBackoff   time.Duration

	mu       sync.Mutex
	flight   *managedQuotaPrimeFlight
	failures map[string]managedQuotaPrimeFailure
}

func NewManagedQuotaPrimer(config ManagedQuotaPrimerConfig) (*ManagedQuotaPrimer, error) {
	if config.Credentials == nil || config.Tokens == nil || config.Fetcher == nil || config.Quotas == nil || config.Reauth == nil {
		return nil, fmt.Errorf("managed Codex quota primer dependencies are required")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.CacheTTL <= 0 {
		config.CacheTTL = defaultManagedQuotaTTL
	}
	if config.MaxConcurrency <= 0 {
		config.MaxConcurrency = defaultManagedQuotaConcurrency
	}
	if config.MaxConcurrency > defaultManagedQuotaConcurrency {
		config.MaxConcurrency = defaultManagedQuotaConcurrency
	}
	return &ManagedQuotaPrimer{
		accounts:         config.Accounts,
		credentials:      config.Credentials,
		tokens:           config.Tokens,
		fetcher:          config.Fetcher,
		quotas:           config.Quotas,
		reauth:           config.Reauth,
		writerGeneration: config.WriterGeneration,
		now:              config.Now,
		cacheTTL:         config.CacheTTL,
		maxConcurrency:   config.MaxConcurrency,
		failureBackoff:   defaultManagedQuotaFailureBackoff,
		failures:         map[string]managedQuotaPrimeFailure{},
	}, nil
}

func (p *ManagedQuotaPrimer) Prime(ctx context.Context) {
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
	flight := &managedQuotaPrimeFlight{done: make(chan struct{})}
	p.flight = flight
	p.mu.Unlock()

	p.runPrime(ctx)

	p.mu.Lock()
	if p.flight == flight {
		p.flight = nil
	}
	close(flight.done)
	p.mu.Unlock()
}

func (p *ManagedQuotaPrimer) runPrime(ctx context.Context) {
	now := p.now()
	snapshot := p.credentials.Read()
	if snapshot.Status != ManagedCredentialStoreOK {
		return
	}
	stale := make([]ManagedAccount, 0, len(p.accounts.Accounts))
	for _, account := range p.accounts.Accounts {
		if !selectableManagedQuotaAccount(account) {
			continue
		}
		record, ok := snapshot.Records[account.ID]
		if !ok || record.Credential == nil || record.DeletedAtMS != nil {
			continue
		}
		quota := p.quotas.GetForCredential(account.ID, record.Generation)
		if quota != nil && now.Sub(quota.UpdatedAt) < p.cacheTTL {
			continue
		}
		stale = append(stale, account)
	}
	if len(stale) == 0 {
		return
	}

	workers := p.maxConcurrency
	if workers > len(stale) {
		workers = len(stale)
	}
	jobs := make(chan ManagedAccount)
	var group sync.WaitGroup
	group.Add(workers)
	for index := 0; index < workers; index++ {
		go func() {
			defer group.Done()
			for account := range jobs {
				if ctx.Err() != nil {
					return
				}
				p.refreshAccount(ctx, account)
			}
		}()
	}
	for _, account := range stale {
		select {
		case jobs <- account:
		case <-ctx.Done():
			close(jobs)
			group.Wait()
			return
		}
	}
	close(jobs)
	group.Wait()
}

func (p *ManagedQuotaPrimer) refreshAccount(ctx context.Context, account ManagedAccount) {
	snapshot := p.credentials.Read()
	record := snapshot.Records[account.ID]
	if p.inFailureBackoff(account.ID, record.Generation) {
		return
	}
	token, err := p.tokens.Get(ctx, account.ID)
	if err != nil {
		p.markProvenManagedRefreshReauth(account.ID, err)
		return
	}
	result, err := p.fetcher.Fetch(ctx, token, account.Plan)
	if err != nil {
		if ctx.Err() == nil {
			p.rememberFailure(account.ID, token.Generation)
		}
		return
	}
	if result.StatusCode == http.StatusUnauthorized {
		// A locally fresh bearer can still be rejected after a plan or
		// credential transition. Reuse the request-path same-account
		// ForceRefresh authority and replay WHAM once; do not hop.
		token, result, err = p.retryWHAMAfterUnauthorized(ctx, account)
		if err != nil {
			return
		}
		if result.StatusCode == http.StatusUnauthorized {
			p.reauth.Mark(account.ID, p.writerGeneration)
			return
		}
	}
	if result.StatusCode < 200 || result.StatusCode >= 300 || result.Quota.Quota == nil {
		if result.StatusCode >= 500 || result.StatusCode == http.StatusTooManyRequests {
			p.rememberFailure(account.ID, token.Generation)
		}
		return
	}
	if !managedQuotaGenerationLive(p.credentials.Read(), account.ID, token.Generation) {
		return
	}
	p.clearFailure(account.ID, token.Generation)
	p.quotas.SetParsedForCredential(account.ID, token.Generation, *result.Quota.Quota, p.writerGeneration, p.now())
}

func (p *ManagedQuotaPrimer) retryWHAMAfterUnauthorized(ctx context.Context, account ManagedAccount) (ManagedToken, WHAMFetchResult, error) {
	forcer, ok := p.tokens.(ManagedTokenForcer)
	if !ok {
		return ManagedToken{}, WHAMFetchResult{StatusCode: http.StatusUnauthorized}, nil
	}
	if err := ctx.Err(); err != nil {
		return ManagedToken{}, WHAMFetchResult{}, err
	}
	token, err := forcer.ForceRefresh(ctx, account.ID)
	if err != nil {
		p.markProvenManagedRefreshReauth(account.ID, err)
		return ManagedToken{}, WHAMFetchResult{}, err
	}
	if !managedQuotaGenerationLive(p.credentials.Read(), account.ID, token.Generation) {
		return ManagedToken{}, WHAMFetchResult{}, nil
	}
	result, err := p.fetcher.Fetch(ctx, token, account.Plan)
	if err != nil {
		return ManagedToken{}, WHAMFetchResult{}, err
	}
	return token, result, nil
}

func (p *ManagedQuotaPrimer) markProvenManagedRefreshReauth(accountID string, err error) {
	var refreshErr *ManagedTokenRefreshError
	if errors.As(err, &refreshErr) {
		p.reauth.Mark(accountID, p.writerGeneration)
	}
}

func (p *ManagedQuotaPrimer) inFailureBackoff(accountID string, generation int64) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	fail, ok := p.failures[accountID]
	if !ok || fail.generation != generation {
		return false
	}
	return p.now().Before(fail.until)
}

func (p *ManagedQuotaPrimer) rememberFailure(accountID string, generation int64) {
	p.mu.Lock()
	p.failures[accountID] = managedQuotaPrimeFailure{generation: generation, until: p.now().Add(p.failureBackoff)}
	p.mu.Unlock()
}

func (p *ManagedQuotaPrimer) clearFailure(accountID string, generation int64) {
	p.mu.Lock()
	if fail, ok := p.failures[accountID]; ok && fail.generation == generation {
		delete(p.failures, accountID)
	}
	p.mu.Unlock()
}

func selectableManagedQuotaAccount(account ManagedAccount) bool {
	if account.IsMain || account.ID == MainAccountID || !managedQuotaAccountIDPattern.MatchString(account.ID) {
		return false
	}
	switch strings.ToLower(account.ID) {
	case "__proto__", "prototype", "constructor":
		return false
	default:
		return true
	}
}

func managedQuotaGenerationLive(snapshot ManagedCredentialSnapshot, accountID string, generation int64) bool {
	return quotaCredentialGenerationLive(snapshot, accountID, generation)
}
