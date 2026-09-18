package chatgpt

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
)

type rt func(*http.Request) (*http.Response, error)

func (f rt) Do(req *http.Request) (*http.Response, error) { return f(req) }

func jwt(email, chatgptID string) string {
	payload, _ := json.Marshal(map[string]any{"email": email, "chatgpt_account_id": chatgptID, "exp": time.Now().Add(time.Hour).Unix()})
	return "eyJhbGciOiJub25lIn0." + base64.RawURLEncoding.EncodeToString(payload) + ".x"
}

func TestChatGPTCallbackPersistsIdentityWithoutLeakingVerifier(t *testing.T) {
	prev := HTTPClient
	t.Cleanup(func() { HTTPClient = prev })
	idToken := jwt("ada@example.com", "chatgpt-ada")
	HTTPClient = rt(func(req *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(req.Body)
		if !strings.Contains(string(body), "code_verifier=") || !strings.Contains(string(body), "authorization_code") {
			t.Fatalf("form=%s", body)
		}
		payload, _ := json.Marshal(map[string]any{
			"access_token":  idToken,
			"refresh_token": "rt",
			"expires_in":    3600,
			"id_token":      idToken,
		})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(payload))), Header: make(http.Header)}, nil
	})
	login, err := NewPendingLogin("pool-1", true)
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	pending := PendingFile{Path: filepath.Join(home, PendingFileName)}
	if err := pending.Save(login); err != nil {
		t.Fatal(err)
	}
	completed, err := pending.Complete(context.Background(), "http://localhost:1455/auth/callback?code=abc&state="+login.State)
	if err != nil {
		t.Fatal(err)
	}
	if completed.ChatGPTAccountID != "chatgpt-ada" || completed.Email != "ada@example.com" || completed.RefreshToken != "rt" {
		t.Fatalf("%+v", completed)
	}
	if _, err := os.Stat(pending.Path); !os.IsNotExist(err) {
		t.Fatal("pending file should be consumed")
	}
}
