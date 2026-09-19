package codexauth

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

type tokenRoundTripFunc func(*http.Request) (*http.Response, error)

func (f tokenRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestManagedTokenSourceReturnsFreshCredentialWithoutNetwork(t *testing.T) {
	home := t.TempDir()
	now := time.UnixMilli(1_700_000_000_000)
	writeManagedStore(t, home, []byte(`{"acct":{"generation":4,"credential":{"accessToken":"fresh-access","refreshToken":"fresh-refresh","expiresAt":1700000061001,"chatgptAccountId":"chat-a"}}}`), 0o600)
	store := mustManagedCredentialStore(t, home)
	reached := false
	source := mustManagedTokenSource(t, ManagedTokenSourceConfig{
		Store: store,
		Now:   func() time.Time { return now },
		HTTPClient: &http.Client{Transport: tokenRoundTripFunc(func(*http.Request) (*http.Response, error) {
			reached = true
			return nil, errors.New("network should not run")
		})},
	})

	token, err := source.Get(context.Background(), "acct")
	if err != nil {
		t.Fatal(err)
	}
	if reached {
		t.Fatal("fresh credential reached network")
	}
	if token.AccessToken != "fresh-access" || token.ChatGPTAccountID != "chat-a" || token.Generation != 4 {
		t.Fatalf("token=%#v", token)
	}
}

func TestManagedTokenSourceForceRefreshRefreshesEvenWhenLocallyFresh(t *testing.T) {
	home := t.TempDir()
	now := time.UnixMilli(1_700_000_000_000)
	writeManagedStore(t, home, []byte(`{"acct":{"generation":4,"credential":{"accessToken":"fresh-access","refreshToken":"fresh-refresh","expiresAt":1700000061001,"chatgptAccountId":"chat-a"}}}`), 0o600)
	store := mustManagedCredentialStore(t, home)
	requests := 0
	source := mustManagedTokenSource(t, ManagedTokenSourceConfig{
		Store: store,
		Now:   func() time.Time { return now },
		HTTPClient: &http.Client{Transport: tokenRoundTripFunc(func(*http.Request) (*http.Response, error) {
			requests++
			return tokenResponse(http.StatusOK, `{"access_token":"forced-access","refresh_token":"forced-refresh","expires_in":3600}`), nil
		})},
	})
	token, err := source.ForceRefresh(context.Background(), "acct")
	if err != nil {
		t.Fatal(err)
	}
	if requests != 1 {
		t.Fatalf("requests=%d", requests)
	}
	if token.AccessToken != "forced-access" || token.Generation != 5 {
		t.Fatalf("token=%#v", token)
	}
}

func TestManagedTokenSourceForceRefreshDoesNotAdoptSiblingLocallyFreshGrant(t *testing.T) {
	home := t.TempDir()
	now := time.UnixMilli(1_700_000_000_000)
	writeManagedStore(t, home, []byte(`{"acct-a":{"generation":4,"credential":{"accessToken":"stale-a","refreshToken":"shared-refresh","expiresAt":1700000061001,"chatgptAccountId":"chat-a"}},"acct-b":{"generation":9,"credential":{"accessToken":"sibling-fresh","refreshToken":"shared-refresh","expiresAt":1700000061001,"chatgptAccountId":"chat-b"}}}`), 0o600)
	store := mustManagedCredentialStore(t, home)
	requests := 0
	source := mustManagedTokenSource(t, ManagedTokenSourceConfig{
		Store: store,
		Now:   func() time.Time { return now },
		HTTPClient: &http.Client{Transport: tokenRoundTripFunc(func(*http.Request) (*http.Response, error) {
			requests++
			return tokenResponse(http.StatusOK, `{"access_token":"forced-a","refresh_token":"forced-refresh","expires_in":3600}`), nil
		})},
	})
	token, err := source.ForceRefresh(context.Background(), "acct-a")
	if err != nil {
		t.Fatal(err)
	}
	if requests != 1 {
		t.Fatalf("requests=%d", requests)
	}
	if token.AccessToken != "forced-a" || token.ChatGPTAccountID != "chat-a" || token.Generation != 5 {
		t.Fatalf("token=%#v", token)
	}
	recordB := store.Read().Records["acct-b"]
	if recordB.Credential == nil || recordB.Credential.AccessToken != "sibling-fresh" || recordB.Generation != 9 {
		t.Fatalf("sibling overwritten: %#v", recordB)
	}
}

func TestManagedTokenSourceForceRefreshRefreshesLocallyFreshCredentialAndKeepsExternalReplacement(t *testing.T) {
	home := t.TempDir()
	now := time.UnixMilli(1_700_000_000_000)
	writeManagedStore(t, home, []byte(`{"acct":{"generation":4,"credential":{"accessToken":"fresh-access","refreshToken":"fresh-refresh","expiresAt":1700000061001,"chatgptAccountId":"chat-a"}}}`), 0o600)
	store := mustManagedCredentialStore(t, home)
	external := mustManagedCredentialStore(t, home)
	requests := 0
	source := mustManagedTokenSource(t, ManagedTokenSourceConfig{
		Store: store,
		Now:   func() time.Time { return now },
		HTTPClient: &http.Client{Transport: tokenRoundTripFunc(func(*http.Request) (*http.Response, error) {
			requests++
			if _, err := external.CompareAndSwap(context.Background(), "acct", 4, ManagedCredential{
				AccessToken: "external", RefreshToken: "external-refresh", ExpiresAtMS: 1_800_000_000_000, ChatGPTAccountID: "chat-external",
			}); err != nil {
				t.Fatalf("external replacement: %v", err)
			}
			return tokenResponse(http.StatusOK, `{"access_token":"forced-result","expires_in":3600}`), nil
		})},
	})

	_, err := source.ForceRefresh(context.Background(), "acct")
	if !errors.Is(err, ErrManagedCredentialGenerationConflict) {
		t.Fatalf("err=%v requests=%d", err, requests)
	}
	if requests != 1 {
		t.Fatalf("requests=%d", requests)
	}
	record := store.Read().Records["acct"]
	if record.Generation != 5 || record.Credential == nil || record.Credential.AccessToken != "external" {
		t.Fatalf("external credential overwritten: %#v", record)
	}
}

func TestManagedTokenSourceRefreshesAtSkewBoundaryAndCASPersists(t *testing.T) {
	home := t.TempDir()
	now := time.UnixMilli(1_700_000_000_000)
	writeManagedStore(t, home, []byte(`{"acct":{"generation":7,"credential":{"accessToken":"old-access","refreshToken":"old-refresh","expiresAt":1700000060000,"chatgptAccountId":"chat-a"}}}`), 0o600)
	store := mustManagedCredentialStore(t, home)
	requests := 0
	source := mustManagedTokenSource(t, ManagedTokenSourceConfig{
		Store: store,
		Now:   func() time.Time { return now },
		HTTPClient: &http.Client{Transport: tokenRoundTripFunc(func(request *http.Request) (*http.Response, error) {
			requests++
			if request.URL.String() != managedTokenRefreshEndpoint || request.Method != http.MethodPost {
				t.Fatalf("request=%s %s", request.Method, request.URL)
			}
			if got := request.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
				t.Fatalf("content-type=%q", got)
			}
			body, err := io.ReadAll(request.Body)
			if err != nil {
				t.Fatal(err)
			}
			values, err := url.ParseQuery(string(body))
			if err != nil {
				t.Fatal(err)
			}
			if values.Get("grant_type") != "refresh_token" || values.Get("client_id") != managedTokenClientID || values.Get("refresh_token") != "old-refresh" {
				t.Fatalf("form=%v", values)
			}
			return tokenResponse(http.StatusOK, `{"access_token":"new-access","refresh_token":"new-refresh","expires_in":3600}`), nil
		})},
	})

	token, err := source.Get(context.Background(), "acct")
	if err != nil {
		t.Fatal(err)
	}
	if requests != 1 {
		t.Fatalf("requests=%d", requests)
	}
	if token.AccessToken != "new-access" || token.ChatGPTAccountID != "chat-a" || token.Generation != 8 {
		t.Fatalf("token=%#v", token)
	}
	record := store.Read().Records["acct"]
	if record.Generation != 8 || record.Credential == nil || record.Credential.AccessToken != "new-access" || record.Credential.RefreshToken != "new-refresh" || record.Credential.ExpiresAtMS != now.Add(time.Hour).UnixMilli() {
		t.Fatalf("record=%#v", record)
	}
	if record.RefreshGrantFingerprint != RefreshGrantFingerprintForToken("new-refresh") {
		t.Fatalf("fingerprint=%q", record.RefreshGrantFingerprint)
	}
}

func TestManagedTokenSourceMalformedExpiresInFallsBackToOneHour(t *testing.T) {
	home := t.TempDir()
	now := time.UnixMilli(1_700_000_000_000)
	writeManagedStore(t, home, []byte(`{"acct":{"generation":1,"credential":{"accessToken":"old","refreshToken":"refresh","expiresAt":1,"chatgptAccountId":"chat"}}}`), 0o600)
	store := mustManagedCredentialStore(t, home)
	for _, body := range []string{
		`{"access_token":"new","expires_in":"bad"}`,
		`{"access_token":"new","expires_in":-1}`,
		`{"access_token":"new","expires_in":1e999}`,
	} {
		t.Run(body, func(t *testing.T) {
			writeManagedStore(t, home, []byte(`{"acct":{"generation":1,"credential":{"accessToken":"old","refreshToken":"refresh","expiresAt":1,"chatgptAccountId":"chat"}}}`), 0o600)
			source := mustManagedTokenSource(t, ManagedTokenSourceConfig{
				Store: store,
				Now:   func() time.Time { return now },
				HTTPClient: &http.Client{Transport: tokenRoundTripFunc(func(*http.Request) (*http.Response, error) {
					return tokenResponse(http.StatusOK, body), nil
				})},
			})
			if _, err := source.Get(context.Background(), "acct"); err != nil {
				t.Fatal(err)
			}
			if got := store.Read().Records["acct"].Credential.ExpiresAtMS; got != now.Add(time.Hour).UnixMilli() {
				t.Fatalf("expiresAt=%d", got)
			}
		})
	}
}

func TestManagedTokenSourceSingleFlightSharesRotatedGrantAcrossAccounts(t *testing.T) {
	home := t.TempDir()
	writeManagedStore(t, home, []byte(`{
		"a":{"generation":2,"credential":{"accessToken":"old-a","refreshToken":"shared-refresh","expiresAt":1,"chatgptAccountId":"chat-a"}},
		"b":{"generation":9,"credential":{"accessToken":"old-b","refreshToken":"shared-refresh","expiresAt":1,"chatgptAccountId":"chat-b"}}
	}`), 0o600)
	store := mustManagedCredentialStore(t, home)
	entered := make(chan struct{})
	release := make(chan struct{})
	requests := 0
	var requestMu sync.Mutex
	source := mustManagedTokenSource(t, ManagedTokenSourceConfig{
		Store: store,
		Now:   func() time.Time { return time.UnixMilli(1_700_000_000_000) },
		HTTPClient: &http.Client{Transport: tokenRoundTripFunc(func(*http.Request) (*http.Response, error) {
			requestMu.Lock()
			requests++
			if requests == 1 {
				close(entered)
			}
			requestMu.Unlock()
			<-release
			return tokenResponse(http.StatusOK, `{"access_token":"shared-new","refresh_token":"rotated-refresh","expires_in":3600}`), nil
		})},
	})

	type result struct {
		id    string
		token ManagedToken
		err   error
	}
	results := make(chan result, 2)
	go func() {
		token, err := source.Get(context.Background(), "a")
		results <- result{id: "a", token: token, err: err}
	}()
	<-entered
	go func() {
		token, err := source.Get(context.Background(), "b")
		results <- result{id: "b", token: token, err: err}
	}()
	time.Sleep(25 * time.Millisecond)
	close(release)

	for range 2 {
		got := <-results
		if got.err != nil {
			t.Fatalf("%s err=%v", got.id, got.err)
		}
		if got.token.AccessToken != "shared-new" {
			t.Fatalf("%s token=%#v", got.id, got.token)
		}
	}
	requestMu.Lock()
	defer requestMu.Unlock()
	if requests != 1 {
		t.Fatalf("refresh requests=%d", requests)
	}
	snapshot := store.Read()
	if snapshot.Records["a"].Generation != 3 || snapshot.Records["b"].Generation != 10 {
		t.Fatalf("generations a=%d b=%d", snapshot.Records["a"].Generation, snapshot.Records["b"].Generation)
	}
	if snapshot.Records["a"].Credential.RefreshToken != "rotated-refresh" || snapshot.Records["b"].Credential.RefreshToken != "rotated-refresh" {
		t.Fatalf("rotated credential not shared")
	}
}

func TestManagedTokenSourceGenerationConflictDoesNotOverwriteExternalReplacement(t *testing.T) {
	home := t.TempDir()
	writeManagedStore(t, home, []byte(`{"acct":{"generation":1,"credential":{"accessToken":"old","refreshToken":"refresh","expiresAt":1,"chatgptAccountId":"chat"}}}`), 0o600)
	store := mustManagedCredentialStore(t, home)
	external := mustManagedCredentialStore(t, home)
	source := mustManagedTokenSource(t, ManagedTokenSourceConfig{
		Store: store,
		Now:   func() time.Time { return time.UnixMilli(1_700_000_000_000) },
		HTTPClient: &http.Client{Transport: tokenRoundTripFunc(func(*http.Request) (*http.Response, error) {
			if _, err := external.CompareAndSwap(context.Background(), "acct", 1, ManagedCredential{
				AccessToken: "external", RefreshToken: "external-refresh", ExpiresAtMS: 1_800_000_000_000, ChatGPTAccountID: "chat-external",
			}); err != nil {
				t.Fatalf("external replacement: %v", err)
			}
			return tokenResponse(http.StatusOK, `{"access_token":"refresh-result","expires_in":3600}`), nil
		})},
	})

	_, err := source.Get(context.Background(), "acct")
	if !errors.Is(err, ErrManagedCredentialGenerationConflict) {
		t.Fatalf("err=%v", err)
	}
	record := store.Read().Records["acct"]
	if record.Generation != 2 || record.Credential == nil || record.Credential.AccessToken != "external" {
		t.Fatalf("external credential overwritten: %#v", record)
	}
}

func TestManagedTokenSourceRefreshBusyCapsDistinctGrants(t *testing.T) {
	home := t.TempDir()
	writeManagedStore(t, home, []byte(`{
		"a":{"generation":1,"credential":{"accessToken":"a","refreshToken":"refresh-a","expiresAt":1,"chatgptAccountId":"chat-a"}},
		"b":{"generation":1,"credential":{"accessToken":"b","refreshToken":"refresh-b","expiresAt":1,"chatgptAccountId":"chat-b"}}
	}`), 0o600)
	store := mustManagedCredentialStore(t, home)
	entered := make(chan struct{})
	release := make(chan struct{})
	source := mustManagedTokenSource(t, ManagedTokenSourceConfig{
		Store:             store,
		MaxRefreshFlights: 1,
		HTTPClient: &http.Client{Transport: tokenRoundTripFunc(func(*http.Request) (*http.Response, error) {
			select {
			case <-entered:
			default:
				close(entered)
			}
			<-release
			return tokenResponse(http.StatusOK, `{"access_token":"new","expires_in":3600}`), nil
		})},
	})
	first := make(chan error, 1)
	go func() {
		_, err := source.Get(context.Background(), "a")
		first <- err
	}()
	<-entered
	if _, err := source.Get(context.Background(), "b"); !errors.Is(err, ErrManagedCredentialRefreshBusy) {
		t.Fatalf("busy err=%v", err)
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
}

func TestManagedTokenSourceRefreshLockHonorsCancellation(t *testing.T) {
	home := t.TempDir()
	writeManagedStore(t, home, []byte(`{"acct":{"generation":1,"credential":{"accessToken":"old","refreshToken":"refresh","expiresAt":1,"chatgptAccountId":"chat"}}}`), 0o600)
	store := mustManagedCredentialStore(t, home)
	source := mustManagedTokenSource(t, ManagedTokenSourceConfig{Store: store})
	fingerprint := store.Read().Records["acct"].RefreshGrantFingerprint
	lock, err := acquireManagedStoreMutationLock(context.Background(), source.refreshLockPath(fingerprint))
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Millisecond)
	defer cancel()
	_, err = source.Get(ctx, "acct")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%v", err)
	}
}

func TestManagedTokenSourceSharedFlightSurvivesInitiatorCancelDuringLockWait(t *testing.T) {
	home := t.TempDir()
	writeManagedStore(t, home, []byte(`{"acct":{"generation":1,"credential":{"accessToken":"old","refreshToken":"refresh","expiresAt":1,"chatgptAccountId":"chat"}}}`), 0o600)
	store := mustManagedCredentialStore(t, home)
	requests := 0
	source := mustManagedTokenSource(t, ManagedTokenSourceConfig{
		Store: store,
		HTTPClient: &http.Client{Transport: tokenRoundTripFunc(func(*http.Request) (*http.Response, error) {
			requests++
			return tokenResponse(http.StatusOK, `{"access_token":"shared-fresh","refresh_token":"rotated","expires_in":3600}`), nil
		})},
	})
	fingerprint := store.Read().Records["acct"].RefreshGrantFingerprint
	lock, err := acquireManagedStoreMutationLock(context.Background(), source.refreshLockPath(fingerprint))
	if err != nil {
		t.Fatal(err)
	}

	aCtx, cancelA := context.WithCancel(context.Background())
	aErr := make(chan error, 1)
	go func() {
		_, err := source.Get(aCtx, "acct")
		aErr <- err
	}()
	time.Sleep(60 * time.Millisecond)

	bErr := make(chan error, 1)
	bToken := make(chan ManagedToken, 1)
	go func() {
		token, err := source.Get(context.Background(), "acct")
		bErr <- err
		bToken <- token
	}()
	time.Sleep(40 * time.Millisecond)
	cancelA()
	if err := <-aErr; !errors.Is(err, context.Canceled) {
		lock.Close()
		t.Fatalf("initiator err=%v", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-bErr:
		if err != nil {
			t.Fatalf("joiner err=%v", err)
		}
		token := <-bToken
		if token.AccessToken != "shared-fresh" || token.Generation != 2 {
			t.Fatalf("joiner token=%#v", token)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("joiner did not receive the shared refresh")
	}
	if requests != 1 {
		t.Fatalf("requests=%d", requests)
	}
}

func TestManagedTokenSourceSharedFlightCancelsWhenAllWaitersLeaveDuringLockWait(t *testing.T) {
	home := t.TempDir()
	writeManagedStore(t, home, []byte(`{"acct":{"generation":1,"credential":{"accessToken":"old","refreshToken":"refresh","expiresAt":1,"chatgptAccountId":"chat"}}}`), 0o600)
	store := mustManagedCredentialStore(t, home)
	requests := 0
	source := mustManagedTokenSource(t, ManagedTokenSourceConfig{
		Store:           store,
		RefreshLockWait: time.Second,
		HTTPClient: &http.Client{Transport: tokenRoundTripFunc(func(*http.Request) (*http.Response, error) {
			requests++
			return tokenResponse(http.StatusOK, `{"access_token":"leaked","expires_in":3600}`), nil
		})},
	})
	fingerprint := store.Read().Records["acct"].RefreshGrantFingerprint
	lock, err := acquireManagedStoreMutationLock(context.Background(), source.refreshLockPath(fingerprint))
	if err != nil {
		t.Fatal(err)
	}

	aCtx, cancelA := context.WithCancel(context.Background())
	bCtx, cancelB := context.WithCancel(context.Background())
	aErr := make(chan error, 1)
	bErr := make(chan error, 1)
	go func() {
		_, err := source.Get(aCtx, "acct")
		aErr <- err
	}()
	time.Sleep(50 * time.Millisecond)
	go func() {
		_, err := source.Get(bCtx, "acct")
		bErr <- err
	}()
	time.Sleep(40 * time.Millisecond)
	cancelA()
	cancelB()
	if err := <-aErr; !errors.Is(err, context.Canceled) {
		lock.Close()
		t.Fatalf("a err=%v", err)
	}
	if err := <-bErr; !errors.Is(err, context.Canceled) {
		lock.Close()
		t.Fatalf("b err=%v", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	if requests != 0 {
		t.Fatalf("zero-waiter flight still refreshed: requests=%d", requests)
	}
	record := store.Read().Records["acct"]
	if record.Generation != 1 || record.Credential == nil || record.Credential.AccessToken != "old" {
		t.Fatalf("zero-waiter flight mutated store: %#v", record)
	}
}

func TestManagedTokenSourceJoinerCancelDoesNotAbortInitiatorRefresh(t *testing.T) {
	home := t.TempDir()
	writeManagedStore(t, home, []byte(`{"acct":{"generation":1,"credential":{"accessToken":"old","refreshToken":"refresh","expiresAt":1,"chatgptAccountId":"chat"}}}`), 0o600)
	store := mustManagedCredentialStore(t, home)
	entered := make(chan struct{})
	release := make(chan struct{})
	source := mustManagedTokenSource(t, ManagedTokenSourceConfig{
		Store: store,
		HTTPClient: &http.Client{Transport: tokenRoundTripFunc(func(*http.Request) (*http.Response, error) {
			close(entered)
			<-release
			return tokenResponse(http.StatusOK, `{"access_token":"kept","expires_in":3600}`), nil
		})},
	})

	aErr := make(chan error, 1)
	aToken := make(chan ManagedToken, 1)
	go func() {
		token, err := source.Get(context.Background(), "acct")
		aErr <- err
		aToken <- token
	}()
	<-entered
	bCtx, cancelB := context.WithCancel(context.Background())
	bErr := make(chan error, 1)
	go func() {
		_, err := source.Get(bCtx, "acct")
		bErr <- err
	}()
	time.Sleep(25 * time.Millisecond)
	cancelB()
	if err := <-bErr; !errors.Is(err, context.Canceled) {
		close(release)
		t.Fatalf("joiner err=%v", err)
	}
	close(release)
	if err := <-aErr; err != nil {
		t.Fatalf("initiator err=%v", err)
	}
	if token := <-aToken; token.AccessToken != "kept" {
		t.Fatalf("initiator token=%#v", token)
	}
}

func TestManagedTokenSourceSharedFlightKeepsNewerGenerationWrittenDuringLockWait(t *testing.T) {
	home := t.TempDir()
	now := time.UnixMilli(1_700_000_000_000)
	writeManagedStore(t, home, []byte(`{"acct":{"generation":1,"credential":{"accessToken":"old","refreshToken":"refresh","expiresAt":1,"chatgptAccountId":"chat"}}}`), 0o600)
	store := mustManagedCredentialStore(t, home)
	external := mustManagedCredentialStore(t, home)
	requests := 0
	source := mustManagedTokenSource(t, ManagedTokenSourceConfig{
		Store: store,
		Now:   func() time.Time { return now },
		HTTPClient: &http.Client{Transport: tokenRoundTripFunc(func(*http.Request) (*http.Response, error) {
			requests++
			return tokenResponse(http.StatusOK, `{"access_token":"stale-flight","expires_in":3600}`), nil
		})},
	})
	fingerprint := store.Read().Records["acct"].RefreshGrantFingerprint
	lock, err := acquireManagedStoreMutationLock(context.Background(), source.refreshLockPath(fingerprint))
	if err != nil {
		t.Fatal(err)
	}

	result := make(chan error, 1)
	go func() {
		_, err := source.Get(context.Background(), "acct")
		result <- err
	}()
	time.Sleep(50 * time.Millisecond)
	if _, err := external.CompareAndSwap(context.Background(), "acct", 1, ManagedCredential{
		AccessToken: "replacement", RefreshToken: "replacement-refresh", ExpiresAtMS: now.Add(time.Hour).UnixMilli(), ChatGPTAccountID: "chat-new",
	}); err != nil {
		lock.Close()
		t.Fatalf("replacement: %v", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	err = <-result
	if err != nil && !errors.Is(err, ErrManagedCredentialGenerationConflict) {
		t.Fatalf("err=%v", err)
	}
	record := store.Read().Records["acct"]
	if record.Credential == nil || record.Credential.AccessToken != "replacement" {
		t.Fatalf("stale flight overwrote replacement: %#v requests=%d", record, requests)
	}
}

func TestManagedTokenSourceSharedFlightDoesNotResurrectDeletedAccount(t *testing.T) {
	home := t.TempDir()
	writeManagedStore(t, home, []byte(`{"acct":{"generation":1,"credential":{"accessToken":"old","refreshToken":"refresh","expiresAt":1,"chatgptAccountId":"chat"}}}`), 0o600)
	store := mustManagedCredentialStore(t, home)
	requests := 0
	source := mustManagedTokenSource(t, ManagedTokenSourceConfig{
		Store: store,
		HTTPClient: &http.Client{Transport: tokenRoundTripFunc(func(*http.Request) (*http.Response, error) {
			requests++
			return tokenResponse(http.StatusOK, `{"access_token":"resurrected","expires_in":3600}`), nil
		})},
	})
	fingerprint := store.Read().Records["acct"].RefreshGrantFingerprint
	lock, err := acquireManagedStoreMutationLock(context.Background(), source.refreshLockPath(fingerprint))
	if err != nil {
		t.Fatal(err)
	}

	result := make(chan error, 1)
	go func() {
		_, err := source.Get(context.Background(), "acct")
		result <- err
	}()
	time.Sleep(50 * time.Millisecond)
	writeManagedStore(t, home, []byte(`{"acct":{"generation":2,"deletedAt":1700000000000}}`), 0o600)
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, ErrManagedCredentialUnavailable) {
		t.Fatalf("err=%v requests=%d", err, requests)
	}
	record := store.Read().Records["acct"]
	if record.DeletedAtMS == nil || record.Credential != nil {
		t.Fatalf("deleted account resurrected: %#v requests=%d", record, requests)
	}
}

func TestManagedTokenSourceRefreshErrorsAreClassifiedAndSecretFree(t *testing.T) {
	home := t.TempDir()
	secret := "super-secret-refresh-token"
	writeManagedStore(t, home, []byte(`{"acct":{"generation":1,"credential":{"accessToken":"old","refreshToken":"`+secret+`","expiresAt":1,"chatgptAccountId":"chat"}}}`), 0o600)
	store := mustManagedCredentialStore(t, home)
	source := mustManagedTokenSource(t, ManagedTokenSourceConfig{
		Store: store,
		HTTPClient: &http.Client{Transport: tokenRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return tokenResponse(http.StatusBadRequest, `{"error":"invalid_grant","error_description":"refresh token revoked `+secret+`"}`), nil
		})},
	})
	_, err := source.Get(context.Background(), "acct")
	var refreshErr *ManagedTokenRefreshError
	if !errors.As(err, &refreshErr) || refreshErr.Reason != ManagedRefreshRevoked {
		t.Fatalf("err=%v", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error leaked secret: %v", err)
	}
}

func TestManagedTokenSourceRejectsRedirects(t *testing.T) {
	home := t.TempDir()
	writeManagedStore(t, home, []byte(`{"acct":{"generation":1,"credential":{"accessToken":"old","refreshToken":"refresh","expiresAt":1,"chatgptAccountId":"chat"}}}`), 0o600)
	store := mustManagedCredentialStore(t, home)
	requests := 0
	source := mustManagedTokenSource(t, ManagedTokenSourceConfig{
		Store: store,
		HTTPClient: &http.Client{Transport: tokenRoundTripFunc(func(request *http.Request) (*http.Response, error) {
			requests++
			return &http.Response{
				StatusCode: http.StatusFound,
				Header:     http.Header{"Location": []string{"https://example.com/steal"}},
				Body:       io.NopCloser(strings.NewReader("redirect")),
				Request:    request,
			}, nil
		})},
	})
	if _, err := source.Get(context.Background(), "acct"); err == nil {
		t.Fatal("redirect was accepted")
	}
	if requests != 1 {
		t.Fatalf("requests=%d", requests)
	}
}

func tokenResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func mustManagedTokenSource(t *testing.T, config ManagedTokenSourceConfig) *ManagedTokenSource {
	t.Helper()
	source, err := NewManagedTokenSource(config)
	if err != nil {
		t.Fatalf("NewManagedTokenSource(): %v", err)
	}
	return source
}

func TestManagedTokenSourceClassifiesNestedRefreshErrors(t *testing.T) {
	tests := []struct {
		name   string
		body   string
		reason ManagedRefreshReason
	}{
		{
			name:   "invalidated",
			body:   `{"error":{"message":"session ended","type":"invalid_request_error","code":"refresh_token_invalidated"}}`,
			reason: ManagedRefreshRevoked,
		},
		{
			name:   "expired",
			body:   `{"error":{"message":"session expired","type":"invalid_request_error","code":"refresh_token_expired"}}`,
			reason: ManagedRefreshExpired,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			writeManagedStore(t, home, []byte(`{"acct":{"generation":1,"credential":{"accessToken":"old","refreshToken":"refresh","expiresAt":1,"chatgptAccountId":"chat"}}}`), 0o600)
			store := mustManagedCredentialStore(t, home)
			source := mustManagedTokenSource(t, ManagedTokenSourceConfig{
				Store: store,
				HTTPClient: &http.Client{Transport: tokenRoundTripFunc(func(*http.Request) (*http.Response, error) {
					return tokenResponse(http.StatusUnauthorized, tc.body), nil
				})},
			})
			_, err := source.Get(context.Background(), "acct")
			var refreshErr *ManagedTokenRefreshError
			if !errors.As(err, &refreshErr) || refreshErr.Reason != tc.reason {
				t.Fatalf("err=%v reason=%v want=%v", err, refreshErr, tc.reason)
			}
		})
	}
}
