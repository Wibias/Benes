package githubcopilot

import (
	"context"
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

func TestGithubCopilotDeviceFlowPersistsWithoutLeakingSecrets(t *testing.T) {
	prevC, prevI := HTTPClient, Interval
	t.Cleanup(func() { HTTPClient, Interval = prevC, prevI })
	Interval = time.Nanosecond
	HTTPClient = rt(func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.Contains(req.URL.Path, "/login/device/code"):
			return jsonResp(200, map[string]any{"user_code": "ABCD-EFGH", "device_code": "dev-secret", "expires_in": 600, "interval": 1}), nil
		case strings.Contains(req.URL.Path, "/login/oauth/access_token"):
			return jsonResp(200, map[string]any{"access_token": "gho_secret", "refresh_token": "ghr_secret"}), nil
		case strings.Contains(req.URL.Path, "/copilot_internal/v2/token"):
			return jsonResp(200, map[string]any{"token": "copilot-secret", "expires_at": time.Now().Add(time.Hour).Unix()}), nil
		case strings.HasSuffix(req.URL.Path, "/user"):
			return jsonResp(200, map[string]any{"id": 42, "login": "octocat", "email": "octocat@github.com"}), nil
		default:
			t.Fatalf("url=%s", req.URL)
			return nil, nil
		}
	})
	pendingLogin, err := NewPendingLogin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if pendingLogin.VerifyURL != "https://github.com/login/device?user_code=ABCD-EFGH" || strings.Contains(pendingLogin.AuthURL(), "dev-secret") {
		t.Fatalf("%+v", pendingLogin)
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
	if done.Account.ID != "42" || done.Account.Email != "octocat@github.com" || done.Account.Token != "copilot-secret" {
		t.Fatalf("%+v", done.Account)
	}
	auth := filepath.Join(home, "auth.json")
	if err := Persist(auth, done.Account); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(auth)
	if strings.Contains(string(raw), "dev-secret") || !strings.Contains(string(raw), `"github-copilot"`) {
		t.Fatalf("%s", raw)
	}
	accounts, active, err := antigravity.LoadPublicAccountsFor(auth, ProviderID)
	if err != nil || active != "42" || len(accounts) != 1 {
		t.Fatalf("%#v %s %v", accounts, active, err)
	}
}
