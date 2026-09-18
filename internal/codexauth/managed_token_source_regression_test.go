package codexauth

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestManagedTokenSourcePersistsRotatedGrantAfterCallerCancellation(t *testing.T) {
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
			return tokenResponse(http.StatusOK, `{"access_token":"new-after-cancel","refresh_token":"rotated-refresh","expires_in":3600}`), nil
		})},
	})

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := source.Get(ctx, "acct")
		result <- err
	}()
	<-entered
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("caller err=%v", err)
	}
	close(release)

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		record := store.Read().Records["acct"]
		if record.Generation == 2 && record.Credential != nil &&
			record.Credential.AccessToken == "new-after-cancel" &&
			record.Credential.RefreshToken == "rotated-refresh" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("rotated credential was not persisted: %#v", store.Read().Records["acct"])
}

func TestManagedTokenSourceRejectsTrailingRefreshJSON(t *testing.T) {
	home := t.TempDir()
	writeManagedStore(t, home, []byte(`{"acct":{"generation":1,"credential":{"accessToken":"old","refreshToken":"refresh","expiresAt":1,"chatgptAccountId":"chat"}}}`), 0o600)
	store := mustManagedCredentialStore(t, home)
	source := mustManagedTokenSource(t, ManagedTokenSourceConfig{
		Store: store,
		HTTPClient: &http.Client{Transport: tokenRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return tokenResponse(http.StatusOK, `{"access_token":"new"} {"unexpected":true}`), nil
		})},
	})
	if _, err := source.Get(context.Background(), "acct"); !errors.Is(err, ErrManagedCredentialRefreshData) {
		t.Fatalf("err=%v", err)
	}
}
