package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/providers/antigravity"
)

type rt func(*http.Request) (*http.Response, error)

func (f rt) Do(req *http.Request) (*http.Response, error) { return f(req) }

func TestAnthropicCallbackPersistsWithoutLeakingVerifier(t *testing.T) {
	prev := HTTPClient
	t.Cleanup(func() { HTTPClient = prev })
	HTTPClient = rt(func(req *http.Request) (*http.Response, error) {
		body, _ := json.Marshal(map[string]any{"access_token": "at", "refresh_token": "rt", "expires_in": 3600, "account": map[string]string{"uuid": "anth-1", "email_address": "ada@claude.ai"}})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body))), Header: make(http.Header)}, nil
	})
	login, err := NewPendingLogin()
	if err != nil {
		t.Fatal(err)
	}
	authURL, err := login.AuthURL()
	if err != nil || strings.Contains(authURL, login.Verifier) || !strings.Contains(authURL, "claude.ai") {
		t.Fatalf("url=%s err=%v", authURL, err)
	}
	home := t.TempDir()
	file := PendingFile{Path: filepath.Join(home, PendingFileName)}
	if err := file.Save(login); err != nil {
		t.Fatal(err)
	}
	done, err := file.Complete(context.Background(), "http://localhost:54545/callback?code=abc&state="+login.State)
	if err != nil {
		t.Fatal(err)
	}
	if done.Account.ID != "anth-1" {
		t.Fatalf("%+v", done.Account)
	}
	auth := filepath.Join(home, "auth.json")
	if err := Persist(auth, done.Account); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(auth)
	if !strings.Contains(string(raw), `"anthropic"`) || strings.Contains(string(raw), login.Verifier) {
		t.Fatalf("%s", raw)
	}
	accounts, active, err := antigravity.LoadPublicAccountsFor(auth, ProviderID)
	if err != nil || active != "anth-1" {
		t.Fatalf("%#v %s %v", accounts, active, err)
	}
}
