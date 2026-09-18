package xai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/providers/antigravity"
)

type rt func(*http.Request) (*http.Response, error)

func (f rt) Do(req *http.Request) (*http.Response, error) { return f(req) }

func jsonResp(code int, body any) *http.Response {
	raw, _ := json.Marshal(body)
	return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(string(raw))), Header: make(http.Header)}
}

func TestXaiCallbackPersistsWithoutLeakingVerifier(t *testing.T) {
	payload, _ := json.Marshal(map[string]any{"sub": "xai-1", "email": "Ada@X.AI"})
	access := "eyJhbGciOiJub25lIn0." + base64.RawURLEncoding.EncodeToString(payload) + ".x"
	prev := HTTPClient
	t.Cleanup(func() { HTTPClient = prev })
	HTTPClient = rt(func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Path, ".well-known") {
			return jsonResp(200, map[string]string{"authorization_endpoint": "https://auth.x.ai/authorize", "token_endpoint": "https://auth.x.ai/oauth/token"}), nil
		}
		return jsonResp(200, map[string]any{"access_token": access, "refresh_token": "rt", "expires_in": 3600}), nil
	})
	login, err := NewPendingLogin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	authURL, err := login.AuthURL()
	if err != nil || !strings.Contains(authURL, "code_challenge") || strings.Contains(authURL, login.Verifier) {
		t.Fatalf("url=%s err=%v", authURL, err)
	}
	home := t.TempDir()
	file := PendingFile{Path: filepath.Join(home, PendingFileName)}
	if err := file.Save(login); err != nil {
		t.Fatal(err)
	}
	done, err := file.Complete(context.Background(), "http://127.0.0.1:56121/callback?code=abc&state="+login.State)
	if err != nil {
		t.Fatal(err)
	}
	if done.Account.ID != "xai-1" || done.Account.Email != "ada@x.ai" {
		t.Fatalf("%+v", done.Account)
	}
	auth := filepath.Join(home, "auth.json")
	if err := Persist(auth, done.Account); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(auth)
	if !strings.Contains(string(raw), `"xai"`) || strings.Contains(string(raw), login.Verifier) {
		t.Fatalf("%s", raw)
	}
	accounts, active, err := antigravity.LoadPublicAccountsFor(auth, ProviderID)
	if err != nil || active != "xai-1" || len(accounts) != 1 {
		t.Fatalf("%#v %s %v", accounts, active, err)
	}
	_ = time.Now
}
