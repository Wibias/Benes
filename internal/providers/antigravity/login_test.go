package antigravity

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAuthURLUsesPKCEAndOfflineConsent(t *testing.T) {
	pending, err := NewPendingLogin()
	if err != nil {
		t.Fatal(err)
	}
	authURL, err := pending.AuthURL()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatal(err)
	}
	q := parsed.Query()
	if parsed.Host != "accounts.google.com" || q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" || q.Get("access_type") != "offline" || q.Get("prompt") != "consent" {
		t.Fatalf("url=%s", authURL)
	}
	if q.Get("state") != pending.State || q.Get("code_challenge") == "" || q.Get("code_challenge") == pending.Verifier {
		t.Fatalf("pkce leaked or missing: %s", authURL)
	}
}

func TestCompleteRejectsMismatchedAndConsumedCallbacks(t *testing.T) {
	dir := t.TempDir()
	store := PendingFile{Path: filepath.Join(dir, "pending.json")}
	pending, err := NewPendingLogin()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(pending); err != nil {
		t.Fatal(err)
	}
	_, err = store.Complete(context.Background(), http.DefaultClient, "https://127.0.0.1/callback?code=abc&state=other")
	if err == nil || !strings.Contains(err.Error(), "state") {
		t.Fatalf("mismatch=%v", err)
	}
	if strings.Contains(err.Error(), pending.Verifier) {
		t.Fatalf("verifier leaked: %v", err)
	}
	pending.CreatedAt = time.Now().Add(-time.Hour)
	if err := store.Save(pending); err != nil {
		t.Fatal(err)
	}
	_, err = store.Complete(context.Background(), http.DefaultClient, "code")
	if err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expired=%v", err)
	}
	_, err = store.Complete(context.Background(), http.DefaultClient, "code")
	if err == nil || !strings.Contains(err.Error(), "consumed") {
		t.Fatalf("consumed=%v", err)
	}
}

func TestCompleteExchangesCodeAndDiscoversProject(t *testing.T) {
	var grant url.Values
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(r.URL.Path, "token") || r.Header.Get("Content-Type") == "application/x-www-form-urlencoded" {
			grant, _ = url.ParseQuery(string(body))
			io.WriteString(w, `{"access_token":"access","refresh_token":"refresh","expires_in":3600}`)
			return
		}
		io.WriteString(w, `{"cloudaicompanionProject":"proj-live"}`)
	}))
	defer upstream.Close()
	client := &http.Client{Transport: rewriteHost{base: upstream.URL, next: http.DefaultTransport}}
	dir := t.TempDir()
	store := PendingFile{Path: filepath.Join(dir, "pending.json")}
	pending, err := NewPendingLogin()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(pending); err != nil {
		t.Fatal(err)
	}
	completed, err := store.Complete(context.Background(), client, "https://127.0.0.1:51121/callback?code=auth-code&state="+pending.State)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Account.Token != "access" || completed.Account.ProjectID != "proj-live" || completed.Refresh != "refresh" || grant.Get("code") != "auth-code" || grant.Get("code_verifier") != pending.Verifier {
		t.Fatalf("completed=%#v grant=%v", completed, grant)
	}
	if grant.Get("client_id") != oauthClientID {
		t.Fatalf("client_id=%q", grant.Get("client_id"))
	}
	if _, ok := grant["client_secret"]; ok {
		t.Fatalf("authorization code exchange must not send client_secret: %v", grant)
	}
	if _, err := os.Stat(store.Path); !os.IsNotExist(err) {
		t.Fatal("pending state must be consumed")
	}
}

func TestAppendAccountPreservesOtherProviders(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(path, []byte(`{"cursor":{"access":"keep","refresh":"r","expires":1}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := AppendAccount(path, Account{ID: "a", Token: "tok", ProjectID: "p"}, "refresh"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"cursor"`) || !strings.Contains(string(raw), `"projectId":"p"`) || !strings.Contains(string(raw), `"access":"tok"`) {
		t.Fatalf("store=%s", raw)
	}
}
