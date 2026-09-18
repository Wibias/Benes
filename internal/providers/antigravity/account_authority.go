package antigravity

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
)

var (
	ErrAccountAuthorityUnavailable   = errors.New("Cloud Code Assist account authority is unavailable")
	ErrAccountChangedDuringRefresh   = errors.New("Cloud Code Assist account changed during refresh")
	ErrAccountRefreshProjectRequired = errors.New("Cloud Code Assist refreshed project id is required")
)

type AccountAuthority interface {
	Snapshot(ctx context.Context, accountID string) (Account, error)
	RefreshAfterUnauthorized(ctx context.Context, stale Account) (Account, error)
}

type FileAccountAuthority struct {
	path       string
	httpClient *http.Client
	discover   func(context.Context, string) (string, error)

	mu       sync.Mutex
	inflight map[string]*accountRefreshCall
}

type accountRefreshCall struct {
	done    chan struct{}
	account Account
	err     error
	waiters int
	cancel  context.CancelFunc
}

func NewFileAccountAuthority(path string, client *http.Client, discover func(context.Context, string) (string, error)) *FileAccountAuthority {
	return &FileAccountAuthority{
		path:       strings.TrimSpace(path),
		httpClient: client,
		discover:   discover,
		inflight:   map[string]*accountRefreshCall{},
	}
}

func (a *FileAccountAuthority) Snapshot(ctx context.Context, accountID string) (Account, error) {
	if a == nil || strings.TrimSpace(a.path) == "" {
		return Account{}, ErrAccountAuthorityUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Account{}, err
	}
	stored, err := loadStoredAccount(a.path, accountID)
	if err != nil {
		return Account{}, err
	}
	account, err := runtimeAccount(stored, a.path)
	if err != nil {
		return Account{}, err
	}
	if strings.TrimSpace(account.ProjectID) != "" {
		return account, nil
	}
	project, err := a.discoverProject(ctx, account.Token)
	if err != nil || strings.TrimSpace(project) == "" {
		return Account{}, ErrAccountRefreshProjectRequired
	}
	updated := stored
	updated.ProjectID = strings.TrimSpace(project)
	if err := replaceStoredAccountIfGeneration(a.path, account.ID, storedGeneration(stored), updated); err != nil {
		if errors.Is(err, ErrAccountChangedDuringRefresh) {
			latest, readErr := loadStoredAccount(a.path, account.ID)
			if readErr == nil {
				latestAccount, convertErr := runtimeAccount(latest, a.path)
				if convertErr == nil && strings.TrimSpace(latestAccount.ProjectID) != "" {
					return latestAccount, nil
				}
			}
		}
		return Account{}, err
	}
	return runtimeAccount(updated, a.path)
}

func (a *FileAccountAuthority) RefreshAfterUnauthorized(ctx context.Context, stale Account) (Account, error) {
	if a == nil || strings.TrimSpace(a.path) == "" {
		return Account{}, ErrAccountAuthorityUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Account{}, err
	}
	id := strings.TrimSpace(stale.ID)
	if id == "" {
		return Account{}, ErrAccountAuthorityUnavailable
	}

	a.mu.Lock()
	if call := a.inflight[id]; call != nil {
		call.waiters++
		a.mu.Unlock()
		return a.waitForRefresh(ctx, id, call)
	}
	workCtx, cancel := context.WithCancel(context.Background())
	call := &accountRefreshCall{done: make(chan struct{}), waiters: 1, cancel: cancel}
	a.inflight[id] = call
	a.mu.Unlock()

	go a.runRefresh(workCtx, id, stale, call)
	return a.waitForRefresh(ctx, id, call)
}

func (a *FileAccountAuthority) waitForRefresh(ctx context.Context, id string, call *accountRefreshCall) (Account, error) {
	defer a.leaveRefresh(id, call)
	select {
	case <-ctx.Done():
		return Account{}, ctx.Err()
	case <-call.done:
		if err := ctx.Err(); err != nil {
			return Account{}, err
		}
		return call.account, call.err
	}
}

func (a *FileAccountAuthority) leaveRefresh(id string, call *accountRefreshCall) {
	if a == nil || call == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if call.waiters > 0 {
		call.waiters--
	}
	if call.waiters != 0 {
		return
	}
	select {
	case <-call.done:
		return
	default:
	}
	if a.inflight[id] == call {
		delete(a.inflight, id)
	}
	if call.cancel != nil {
		call.cancel()
	}
}

func (a *FileAccountAuthority) runRefresh(ctx context.Context, id string, stale Account, call *accountRefreshCall) {
	account, err := a.refreshOne(ctx, stale)

	a.mu.Lock()
	call.account = account
	call.err = err
	if a.inflight[id] == call {
		delete(a.inflight, id)
	}
	if call.cancel != nil {
		call.cancel()
		call.cancel = nil
	}
	close(call.done)
	a.mu.Unlock()
}

func (a *FileAccountAuthority) refreshOne(ctx context.Context, stale Account) (Account, error) {
	current, err := loadStoredAccount(a.path, stale.ID)
	if err != nil {
		return Account{}, err
	}
	currentAccount, err := runtimeAccount(current, a.path)
	if err != nil {
		return Account{}, err
	}
	if currentAccount.Token != stale.Token || currentAccount.ProjectID != stale.ProjectID {
		if strings.TrimSpace(currentAccount.ProjectID) == "" {
			return a.Snapshot(ctx, stale.ID)
		}
		return currentAccount, nil
	}
	if strings.TrimSpace(current.Refresh) == "" {
		return Account{}, ErrAccountAuthorityUnavailable
	}

	access, expires, err := refreshAccessTokenWithExpiry(ctx, a.httpClient, current.Refresh)
	if err != nil {
		return Account{}, fmt.Errorf("refresh Cloud Code Assist credential: %w", err)
	}
	project, err := a.discoverProject(ctx, access)
	if err != nil || strings.TrimSpace(project) == "" {
		return Account{}, ErrAccountRefreshProjectRequired
	}
	updated := current
	updated.Token = access
	updated.Expires = expires
	updated.ProjectID = strings.TrimSpace(project)
	updated.NeedsReauth = false

	if err := replaceStoredAccountIfGeneration(a.path, stale.ID, storedGeneration(current), updated); err != nil {
		if errors.Is(err, ErrAccountChangedDuringRefresh) {
			latest, readErr := loadStoredAccount(a.path, stale.ID)
			if readErr == nil {
				latestAccount, convertErr := runtimeAccount(latest, a.path)
				if convertErr == nil && (latestAccount.Token != stale.Token || latestAccount.ProjectID != stale.ProjectID) {
					return latestAccount, nil
				}
			}
		}
		return Account{}, err
	}
	return runtimeAccount(updated, a.path)
}

func (a *FileAccountAuthority) discoverProject(ctx context.Context, token string) (string, error) {
	if a != nil && a.discover != nil {
		return a.discover(ctx, token)
	}
	if a == nil || a.httpClient == nil {
		return "", ErrAccountAuthorityUnavailable
	}
	return DiscoverProject(ctx, a.httpClient, DailyAPI, token)
}

func loadStoredAccount(path, accountID string) (StoredAccount, error) {
	path = strings.TrimSpace(path)
	accountID = strings.TrimSpace(accountID)
	if path == "" || accountID == "" {
		return StoredAccount{}, ErrAccountAuthorityUnavailable
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return StoredAccount{}, ErrAccountAuthorityUnavailable
	}
	for _, account := range ParseAuthStoreFor(raw, AuthStoreProvider) {
		if storedAccountID(account) != accountID {
			continue
		}
		if account.NeedsReauth || strings.TrimSpace(account.Token) == "" {
			return StoredAccount{}, ErrAccountAuthorityUnavailable
		}
		return account, nil
	}
	return StoredAccount{}, ErrAccountAuthorityUnavailable
}

func runtimeAccount(stored StoredAccount, sourcePath string) (Account, error) {
	id := storedAccountID(stored)
	if id == "" || stored.NeedsReauth || strings.TrimSpace(stored.Token) == "" {
		return Account{}, ErrAccountAuthorityUnavailable
	}
	return Account{
		ID:         id,
		Token:      stored.Token,
		ProjectID:  strings.TrimSpace(stored.ProjectID),
		SourcePath: strings.TrimSpace(sourcePath),
	}, nil
}

func storedAccountID(account StoredAccount) string {
	id := strings.TrimSpace(account.ID)
	if id == "" {
		id = strings.TrimSpace(account.ProjectID)
	}
	return id
}

func storedGeneration(account StoredAccount) [32]byte {
	parts := []string{
		account.ID,
		account.AccountID,
		account.Token,
		account.Refresh,
		account.ProjectID,
		account.Email,
		strconv.FormatInt(account.Expires, 10),
		strconv.FormatBool(account.NeedsReauth),
		account.ProfileARN,
		account.APIRegion,
		account.SSORegion,
		account.AuthType,
		account.ClientID,
		account.ClientSecret,
	}
	return sha256.Sum256([]byte(strings.Join(parts, "\x00")))
}

func replaceStoredAccountIfGeneration(path, accountID string, expected [32]byte, next StoredAccount) error {
	return mutateAuthStore(path, AuthStoreProvider, func(accounts []StoredAccount, active string) ([]StoredAccount, string, error) {
		for i := range accounts {
			if storedAccountID(accounts[i]) != strings.TrimSpace(accountID) {
				continue
			}
			if storedGeneration(accounts[i]) != expected {
				return nil, "", ErrAccountChangedDuringRefresh
			}
			next.ID = accounts[i].ID
			accounts[i] = next
			return accounts, active, nil
		}
		return nil, "", ErrAccountChangedDuringRefresh
	})
}
