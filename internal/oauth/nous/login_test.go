package nous

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

func jwt(claims map[string]any) string {
	payload, _ := json.Marshal(claims)
	return "eyJhbGciOiJub25lIn0." + base64.RawURLEncoding.EncodeToString(payload) + ".x"
}

func jsonResp(code int, body any) *http.Response {
	raw, _ := json.Marshal(body)
	return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(string(raw))), Header: make(http.Header)}
}

func TestNousDeviceFlowPersistsWithoutLeakingSecrets(t *testing.T) {
	access := jwt(map[string]any{"sub": "nous-1", "email": "Ada@Nous.com", "exp": time.Now().Add(time.Hour).Unix(), "scope": "inference:invoke"})
	refresh := "rt-rotated"
	prevC, prevI := HTTPClient, Interval
	t.Cleanup(func() { HTTPClient, Interval = prevC, prevI })
	Interval = time.Nanosecond
	HTTPClient = rt(func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Path, "/device/code") {
			return jsonResp(200, map[string]any{"user_code": "WXYZ", "device_code": "dev-secret", "verification_uri": "https://portal.nousresearch.com/device", "expires_in": 600, "interval": 1}), nil
		}
		return jsonResp(200, map[string]any{"access_token": access, "refresh_token": refresh, "expires_in": 3600}), nil
	})
	pendingLogin, err := NewPendingLogin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(pendingLogin.AuthURL(), "dev-secret") {
		t.Fatal(pendingLogin.AuthURL())
	}
	home := t.TempDir()
	file := PendingFile{Path: filepath.Join(home, PendingFileName)}
	if err := file.Save(pendingLogin); err != nil {
		t.Fatal(err)
	}
	done, err := file.Complete(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if done.Account.ID != "nous-1" || done.Account.Email != "ada@nous.com" {
		t.Fatalf("%+v", done.Account)
	}
	auth := filepath.Join(home, "auth.json")
	if err := Persist(auth, done.Account); err != nil {
		t.Fatal(err)
	}
	accounts, active, err := antigravity.LoadPublicAccountsFor(auth, ProviderID)
	if err != nil || active != "nous-1" || len(accounts) != 1 {
		t.Fatalf("%#v %s %v", accounts, active, err)
	}
	raw, _ := os.ReadFile(auth)
	if strings.Contains(string(raw), "dev-secret") {
		t.Fatal(string(raw))
	}
}
