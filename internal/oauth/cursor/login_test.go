package cursor

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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) Do(req *http.Request) (*http.Response, error) { return f(req) }

func jwt(sub, email string) string {
	payload, _ := json.Marshal(map[string]any{"sub": sub, "email": email, "exp": time.Now().Add(time.Hour).Unix()})
	return "eyJhbGciOiJub25lIn0." + base64.RawURLEncoding.EncodeToString(payload) + ".x"
}

func TestPollPersistsCursorAccountWithoutLeakingSecrets(t *testing.T) {
	access := jwt("user-1", "Ada@Example.com")
	refresh := jwt("user-1", "ada@example.com")
	prevClient, prevAttempts, prevDelay := HTTPClient, MaxAttempts, BaseDelay
	t.Cleanup(func() {
		HTTPClient, MaxAttempts, BaseDelay = prevClient, prevAttempts, prevDelay
	})
	MaxAttempts = 2
	BaseDelay = 0
	HTTPClient = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if !strings.Contains(req.URL.String(), "api2.cursor.sh/auth/poll") {
			t.Fatalf("url=%s", req.URL)
		}
		body, _ := json.Marshal(map[string]string{"accessToken": access, "refreshToken": refresh})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body))), Header: make(http.Header)}, nil
	})
	home := t.TempDir()
	pending := PendingFile{Path: filepath.Join(home, PendingFileName)}
	login, err := NewPendingLogin()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(login.AuthURL(), "cursor.com/loginDeepControl") || strings.Contains(login.AuthURL(), login.Verifier) {
		t.Fatalf("url=%s", login.AuthURL())
	}
	if err := pending.Save(login); err != nil {
		t.Fatal(err)
	}
	completed, err := pending.Complete(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if completed.Account.ID != "user-1" || completed.Account.Email != "ada@example.com" {
		t.Fatalf("%+v", completed.Account)
	}
	authPath := filepath.Join(home, "auth.json")
	if err := Persist(authPath, completed.Account); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"cursor"`) || !strings.Contains(string(raw), access) {
		t.Fatalf("%s", raw)
	}
	accounts, active, err := antigravity.LoadPublicAccountsFor(authPath, ProviderID)
	if err != nil || active != "user-1" || len(accounts) != 1 || accounts[0].Email != "ada@example.com" {
		t.Fatalf("accounts=%#v active=%s err=%v", accounts, active, err)
	}
	if _, err := os.Stat(pending.Path); !os.IsNotExist(err) {
		t.Fatal("pending file should be consumed")
	}
}

func TestAuthURLOmitsVerifier(t *testing.T) {
	login, err := NewPendingLogin()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(login.AuthURL(), login.Verifier) {
		t.Fatal(login.AuthURL())
	}
}
