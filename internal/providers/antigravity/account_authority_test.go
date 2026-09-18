package antigravity

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestFileAccountAuthorityRefreshPublishesBearerAndProjectTogether(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	if err := AppendStoredAccount(path, AuthStoreProvider, StoredAccount{
		ID: "a", Token: "stale", Refresh: "refresh-a", ProjectID: "old-project", Expires: 1,
	}); err != nil {
		t.Fatal(err)
	}

	var refreshes atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refreshes.Add(1)
		if r.URL.Path != "/token" {
			t.Errorf("unexpected path %q", r.URL.Path)
			http.Error(w, "unexpected path", http.StatusNotFound)
			return
		}
		_, _ = io.WriteString(w, `{"access_token":"fresh","expires_in":3600}`)
	}))
	defer upstream.Close()
	client := &http.Client{Transport: rewriteHost{base: upstream.URL, next: http.DefaultTransport}}
	authority := NewFileAccountAuthority(path, client, func(ctx context.Context, token string) (string, error) {
		if token != "fresh" {
			t.Fatalf("discover token=%q", token)
		}
		return "new-project", nil
	})

	got, err := authority.RefreshAfterUnauthorized(context.Background(), Account{ID: "a", Token: "stale", ProjectID: "old-project", SourcePath: path})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "a" || got.Token != "fresh" || got.ProjectID != "new-project" || got.SourcePath != path {
		t.Fatalf("snapshot=%#v", got)
	}
	if refreshes.Load() != 1 {
		t.Fatalf("refreshes=%d", refreshes.Load())
	}
	stored, err := loadStoredAccount(path, "a")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Token != "fresh" || stored.Refresh != "refresh-a" || stored.ProjectID != "new-project" || stored.Expires <= 1 {
		t.Fatalf("published=%#v", stored)
	}
}

func TestFileAccountAuthorityDoesNotOverwriteReplacementDuringRefresh(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	if err := AppendStoredAccount(path, AuthStoreProvider, StoredAccount{
		ID: "a", Token: "stale", Refresh: "refresh-a", ProjectID: "old-project", Expires: 1,
	}); err != nil {
		t.Fatal(err)
	}

	started := make(chan struct{})
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		_, _ = io.WriteString(w, `{"access_token":"must-not-publish","expires_in":3600}`)
	}))
	defer upstream.Close()
	client := &http.Client{Transport: rewriteHost{base: upstream.URL, next: http.DefaultTransport}}
	authority := NewFileAccountAuthority(path, client, func(context.Context, string) (string, error) {
		return "must-not-publish-project", nil
	})

	type result struct {
		account Account
		err     error
	}
	resultCh := make(chan result, 1)
	go func() {
		account, err := authority.RefreshAfterUnauthorized(context.Background(), Account{ID: "a", Token: "stale", ProjectID: "old-project", SourcePath: path})
		resultCh <- result{account: account, err: err}
	}()
	<-started
	if err := AppendStoredAccount(path, AuthStoreProvider, StoredAccount{
		ID: "a", Token: "external", Refresh: "external-refresh", ProjectID: "external-project", Expires: 99,
	}); err != nil {
		t.Fatal(err)
	}
	close(release)
	got := <-resultCh
	if got.err != nil {
		t.Fatal(got.err)
	}
	if got.account.Token != "external" || got.account.ProjectID != "external-project" {
		t.Fatalf("authority did not honor replacement: %#v", got.account)
	}
	stored, err := loadStoredAccount(path, "a")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Token != "external" || stored.Refresh != "external-refresh" || stored.ProjectID != "external-project" {
		t.Fatalf("late refresh overwrote replacement: %#v", stored)
	}
}

func TestFileAccountAuthorityConcurrent401sJoinOneRefresh(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	if err := AppendStoredAccount(path, AuthStoreProvider, StoredAccount{
		ID: "a", Token: "stale", Refresh: "refresh-a", ProjectID: "old-project", Expires: 1,
	}); err != nil {
		t.Fatal(err)
	}

	var refreshes atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	var startOnce sync.Once
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refreshes.Add(1)
		startOnce.Do(func() { close(started) })
		<-release
		_, _ = io.WriteString(w, `{"access_token":"fresh","expires_in":3600}`)
	}))
	defer upstream.Close()
	client := &http.Client{Transport: rewriteHost{base: upstream.URL, next: http.DefaultTransport}}
	authority := NewFileAccountAuthority(path, client, func(context.Context, string) (string, error) {
		return "new-project", nil
	})
	stale := Account{ID: "a", Token: "stale", ProjectID: "old-project", SourcePath: path}

	type result struct {
		account Account
		err     error
	}
	results := make(chan result, 2)
	go func() {
		account, err := authority.RefreshAfterUnauthorized(context.Background(), stale)
		results <- result{account: account, err: err}
	}()
	<-started
	go func() {
		account, err := authority.RefreshAfterUnauthorized(context.Background(), stale)
		results <- result{account: account, err: err}
	}()
	close(release)

	for range 2 {
		got := <-results
		if got.err != nil || got.account.Token != "fresh" || got.account.ProjectID != "new-project" {
			t.Fatalf("result=%#v err=%v", got.account, got.err)
		}
	}
	if refreshes.Load() != 1 {
		t.Fatalf("refreshes=%d, want 1", refreshes.Load())
	}
}

func TestFileAccountAuthorityCanceledLeaderDoesNotPoisonJoinedCaller(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	if err := AppendStoredAccount(path, AuthStoreProvider, StoredAccount{
		ID: "a", Token: "stale", Refresh: "refresh-a", ProjectID: "old-project", Expires: 1,
	}); err != nil {
		t.Fatal(err)
	}

	var refreshes atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refreshes.Add(1)
		close(started)
		<-release
		_, _ = io.WriteString(w, `{"access_token":"fresh","expires_in":3600}`)
	}))
	defer upstream.Close()
	client := &http.Client{Transport: rewriteHost{base: upstream.URL, next: http.DefaultTransport}}
	authority := NewFileAccountAuthority(path, client, func(context.Context, string) (string, error) {
		return "new-project", nil
	})
	stale := Account{ID: "a", Token: "stale", ProjectID: "old-project", SourcePath: path}

	type result struct {
		account Account
		err     error
	}
	leaderCtx, cancelLeader := context.WithCancel(context.Background())
	leader := make(chan result, 1)
	joined := make(chan result, 1)
	go func() {
		account, err := authority.RefreshAfterUnauthorized(leaderCtx, stale)
		leader <- result{account: account, err: err}
	}()
	<-started
	go func() {
		account, err := authority.RefreshAfterUnauthorized(context.Background(), stale)
		joined <- result{account: account, err: err}
	}()

	deadline := time.Now().Add(time.Second)
	for {
		authority.mu.Lock()
		call := authority.inflight["a"]
		waiters := 0
		if call != nil {
			waiters = call.waiters
		}
		authority.mu.Unlock()
		if waiters == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("joined caller did not enter refresh flight; waiters=%d", waiters)
		}
		time.Sleep(time.Millisecond)
	}

	cancelLeader()
	if got := <-leader; !errors.Is(got.err, context.Canceled) {
		t.Fatalf("leader err=%v account=%#v", got.err, got.account)
	}
	close(release)
	got := <-joined
	if got.err != nil || got.account.Token != "fresh" || got.account.ProjectID != "new-project" {
		t.Fatalf("joined result=%#v err=%v", got.account, got.err)
	}
	if refreshes.Load() != 1 {
		t.Fatalf("refreshes=%d, want 1", refreshes.Load())
	}
}

func TestFileAccountAuthorityMissingRefreshedProjectFailsClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	if err := AppendStoredAccount(path, AuthStoreProvider, StoredAccount{
		ID: "a", Token: "stale", Refresh: "refresh-a", ProjectID: "old-project", Expires: 1,
	}); err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"access_token":"fresh","expires_in":3600}`)
	}))
	defer upstream.Close()
	client := &http.Client{Transport: rewriteHost{base: upstream.URL, next: http.DefaultTransport}}
	authority := NewFileAccountAuthority(path, client, func(context.Context, string) (string, error) {
		return "", nil
	})

	_, err := authority.RefreshAfterUnauthorized(context.Background(), Account{ID: "a", Token: "stale", ProjectID: "old-project", SourcePath: path})
	if !errors.Is(err, ErrAccountRefreshProjectRequired) {
		t.Fatalf("err=%v", err)
	}
	stored, readErr := loadStoredAccount(path, "a")
	if readErr != nil {
		t.Fatal(readErr)
	}
	if stored.Token != "stale" || stored.ProjectID != "old-project" {
		t.Fatalf("failed refresh mutated authority: %#v", stored)
	}
}
