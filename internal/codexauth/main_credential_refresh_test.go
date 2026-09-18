package codexauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMainCredentialSourceMarksExpiredRefreshGrantWithoutExposingIt(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	home := t.TempDir()
	token := jwtPayload(t, fmt.Sprintf(`{"exp":%d}`, now.Add(-time.Second).Unix()))
	writeMainAuth(t, home, []byte(fmt.Sprintf(
		`{"tokens":{"access_token":%q,"refresh_token":"native-refresh-grant","account_id":"acct_expired"},"unrelated":true}`,
		token,
	)), 0o600)
	source := mustMainCredentialSource(t, home)
	got := source.Read(now)
	if got.Status != MainCredentialExpired {
		t.Fatalf("status=%q", got.Status)
	}
	if !got.Refreshable {
		t.Fatal("expired native Main with a refresh grant was not marked refreshable")
	}
	if got.Credential != (MainCredential{}) {
		t.Fatalf("expired result exposed credential=%#v", got.Credential)
	}
}

func TestStaticEligibleAccountIDsIncludesExpiredMainWhenRefreshable(t *testing.T) {
	got := StaticEligibleAccountIDs(
		ManagedAccountConfig{},
		ManagedCredentialSnapshot{Status: ManagedCredentialStoreMissing},
		MainCredentialResult{Status: MainCredentialExpired, Refreshable: true},
		StaticEligibilityOptions{IncludeMain: true},
	)
	if len(got) != 1 || got[0] != MainAccountID {
		t.Fatalf("eligible=%#v", got)
	}
}

func TestPoolCredentialResolverRefreshesExpiredNativeMainBeforeRequestIO(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	home := t.TempDir()
	expired := jwtPayload(t, fmt.Sprintf(`{"exp":%d,"chatgpt_account_id":"acct_expired"}`, now.Add(-time.Second).Unix()))
	writeMainAuth(t, home, []byte(fmt.Sprintf(
		`{"tokens":{"access_token":%q,"refresh_token":"native-refresh-grant","account_id":"acct_expired","id_token":%q}}`,
		expired, jwtPayload(t, `{"chatgpt_account_id":"acct_expired"}`),
	)), 0o600)

	var refreshCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refreshCalls.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/oauth/token" {
			t.Errorf("unexpected refresh request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("refresh_token") != "native-refresh-grant" {
			t.Errorf("form=%v", r.Form)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"refreshed-main-access","refresh_token":"rotated-native-refresh","expires_in":3600}`)
	}))
	defer upstream.Close()

	source := mustMainCredentialSource(t, home)
	source.httpClient = upstream.Client()
	source.refreshURL = upstream.URL + "/oauth/token"

	selector := NewPoolSelector(PoolSelectorDependencies{})
	resolver, err := NewPoolCredentialResolver(PoolCredentialResolverConfig{
		Selector:           selector,
		ManagedTokens:      &fakeManagedTokenGetter{},
		ManagedCredentials: fakeManagedCredentialReader{snapshot: emptyManagedSnapshotForTest()},
		MainCredentials:    source,
	})
	if err != nil {
		t.Fatal(err)
	}

	input := poolSelectionBaseForTest(now)
	input.IncludeMain = true
	input.Main = source.Read(now)
	credential, err := resolver.Resolve(context.Background(), PoolCredentialRequest{Selection: input, WriterGeneration: 4})
	if err != nil {
		t.Fatalf("Resolve(): %v", err)
	}
	if credential.AccountID != MainAccountID || credential.AccessToken != "refreshed-main-access" {
		t.Fatalf("credential=%#v", credential)
	}
	if refreshCalls.Load() != 1 {
		t.Fatalf("refresh calls=%d", refreshCalls.Load())
	}
	if selector.Reauth.Needs(MainAccountID) {
		t.Fatal("successful native Main refresh marked reauth")
	}

	raw, err := os.ReadFile(filepath.Join(home, "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	var published struct {
		Tokens *struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
		} `json:"tokens"`
		Unrelated *bool `json:"unrelated"`
	}
	if err := json.Unmarshal(raw, &published); err != nil {
		t.Fatal(err)
	}
	if published.Tokens == nil || published.Tokens.AccessToken != "refreshed-main-access" || published.Tokens.RefreshToken != "rotated-native-refresh" {
		t.Fatalf("published tokens=%#v body=%s", published.Tokens, raw)
	}
}

func TestPoolCredentialResolverDoesNotRefreshWhenMainGrantIsMissing(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	home := t.TempDir()
	expired := jwtPayload(t, fmt.Sprintf(`{"exp":%d}`, now.Add(-time.Second).Unix()))
	writeMainAuth(t, home, []byte(fmt.Sprintf(`{"tokens":{"access_token":%q,"account_id":"acct_expired"}}`, expired)), 0o600)
	source := mustMainCredentialSource(t, home)

	selector := NewPoolSelector(PoolSelectorDependencies{})
	resolver, err := NewPoolCredentialResolver(PoolCredentialResolverConfig{
		Selector:           selector,
		ManagedTokens:      &fakeManagedTokenGetter{},
		ManagedCredentials: fakeManagedCredentialReader{snapshot: emptyManagedSnapshotForTest()},
		MainCredentials:    source,
	})
	if err != nil {
		t.Fatal(err)
	}
	input := poolSelectionBaseForTest(now)
	input.IncludeMain = true
	input.Main = MainCredentialResult{Status: MainCredentialOK, Credential: MainCredential{AccessToken: "stale-ok"}}
	_, err = resolver.Resolve(context.Background(), PoolCredentialRequest{Selection: input, WriterGeneration: 9})
	if !errors.Is(err, ErrPoolSelectedCredentialUnavailable) {
		t.Fatalf("err=%v", err)
	}
	if !selector.Reauth.Needs(MainAccountID) {
		t.Fatal("expired Main without a refresh grant did not mark reauth")
	}
}

func TestMainCredentialEnsureHonorsExternalWriterSnapshot(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	home := t.TempDir()
	expired := jwtPayload(t, fmt.Sprintf(`{"exp":%d}`, now.Add(-time.Second).Unix()))
	original := []byte(fmt.Sprintf(`{"tokens":{"access_token":%q,"refresh_token":"native-refresh-grant"}}`, expired))
	writeMainAuth(t, home, original, 0o600)

	source := mustMainCredentialSource(t, home)
	source.httpClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		concurrent := []byte(`{"tokens":{"access_token":"codex-cli-write","refresh_token":"cli-grant"}}`)
		if err := os.WriteFile(filepath.Join(home, "auth.json"), concurrent, 0o600); err != nil {
			t.Error(err)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"access_token":"refreshed-should-not-publish","refresh_token":"rotated","expires_in":3600}`)),
		}, nil
	})}
	source.refreshURL = "https://auth.openai.com/oauth/token"

	got, err := source.Ensure(context.Background(), now, false)
	if err != nil {
		t.Fatalf("Ensure(): %v", err)
	}
	if got.Credential.AccessToken != "codex-cli-write" {
		t.Fatalf("did not keep concurrent Codex write: %#v", got)
	}
	raw, readErr := os.ReadFile(filepath.Join(home, "auth.json"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if strings.Contains(string(raw), "refreshed-should-not-publish") {
		t.Fatalf("auth.json overwritten with refresh result: %s", raw)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
