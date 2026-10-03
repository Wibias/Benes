package antigravity

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

var ErrNoUsableAccount = errors.New("no usable Cloud Code Assist account remains")

type FailureKind string

const (
	FailureRateLimit  FailureKind = "rate_limit"
	FailureQuota      FailureKind = "quota"
	FailureGeoblock   FailureKind = "geoblock"
	FailurePermission FailureKind = "permission"
	FailureAuth       FailureKind = "auth"
)

const (
	defaultCooldown      = 30 * time.Second
	maxCooldown          = 15 * time.Minute
	maxPreStreamFailover = 3
)

type Account struct {
	ID         string
	Token      string
	ProjectID  string
	SourcePath string
}

type accountState struct {
	until time.Time
	kind  FailureKind
}

type Pool struct {
	mu       sync.Mutex
	cooldown map[string]accountState
	now      func() time.Time
}

func NewPool() *Pool {
	return &Pool{cooldown: map[string]accountState{}, now: time.Now}
}

func (p *Pool) Select(accounts []Account) (Account, error) {
	if p == nil {
		return Account{}, ErrNoUsableAccount
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	for _, account := range accounts {
		if stringsEmpty(account.ID) || stringsEmpty(account.Token) {
			continue
		}
		state, cooling := p.cooldown[account.ID]
		if cooling && now.Before(state.until) {
			continue
		}
		return account, nil
	}
	return Account{}, ErrNoUsableAccount
}

func (p *Pool) Mark(accountID string, kind FailureKind, retryAfter time.Duration) {
	if p == nil || stringsEmpty(accountID) {
		return
	}
	wait := defaultCooldown
	if retryAfter > wait {
		wait = retryAfter
	}
	if wait > maxCooldown {
		wait = maxCooldown
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cooldown[accountID] = accountState{until: p.now().Add(wait), kind: kind}
}

func ClassifyStatus(status int, body string, geoblocked bool) FailureKind {
	lower := strings.ToLower(body)
	if geoblocked || isGeoblockBody(lower) {
		return FailureGeoblock
	}
	if status == 429 && isQuotaBody(lower) {
		return FailureQuota
	}
	if strings.Contains(lower, "resource_exhausted") && isQuotaBody(lower) {
		return FailureQuota
	}
	if status == http.StatusForbidden && isAccountPermissionBody(lower) {
		return FailurePermission
	}
	return FailureRateLimit
}

func isAccountPermissionBody(lower string) bool {
	return strings.Contains(lower, "validation_required") ||
		(strings.Contains(lower, "permission_denied") && strings.Contains(lower, "verification"))
}

func isGeoblockBody(lower string) bool {
	return strings.Contains(lower, "failed_precondition") ||
		strings.Contains(lower, "user location is not supported") ||
		strings.Contains(lower, "location is not supported")
}

func isQuotaBody(lower string) bool {
	return strings.Contains(lower, "quota") ||
		strings.Contains(lower, "quotafailure") ||
		strings.Contains(lower, "billing")
}

func ParseRetryAfter(header http.Header) time.Duration {
	if header == nil {
		return 0
	}
	raw := strings.TrimSpace(header.Get("Retry-After"))
	if raw == "" {
		return 0
	}
	if secs, err := strconv.Atoi(raw); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	if when, err := http.ParseTime(raw); err == nil {
		if wait := time.Until(when); wait > 0 {
			return wait
		}
	}
	return 0
}

func stringsEmpty(v string) bool { return strings.TrimSpace(v) == "" }
