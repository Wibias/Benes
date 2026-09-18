package server

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/oauth/chatgpt"
	"github.com/Wibias/Benes/internal/oauth/loginown"
)

type chatgptRT func(*http.Request) (*http.Response, error)

func (f chatgptRT) Do(req *http.Request) (*http.Response, error) { return f(req) }

func chatgptJWT(email, id string) string {
	payload, _ := json.Marshal(map[string]any{"email": email, "chatgpt_account_id": id, "exp": time.Now().Add(time.Hour).Unix()})
	return "eyJhbGciOiJub25lIn0." + base64.RawURLEncoding.EncodeToString(payload) + ".x"
}

func TestCodexAuthLoginCodePersistsPoolAccount(t *testing.T) {
	loginown.ResetForTest()
	t.Cleanup(loginown.ResetForTest)
	prev := chatgpt.HTTPClient
	t.Cleanup(func() { chatgpt.HTTPClient = prev })
	token := chatgptJWT("ada@example.com", "chatgpt-ada")
	chatgpt.HTTPClient = chatgptRT(func(req *http.Request) (*http.Response, error) {
		body, _ := json.Marshal(map[string]any{"access_token": token, "refresh_token": "rt", "expires_in": 3600, "id_token": token})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body))), Header: make(http.Header)}, nil
	})

	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"listen":"127.0.0.1:0"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai": providerFunc(nil)},
		ConfigPath:     configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)

	blocked := httptest.NewRequest(http.MethodPost, "/api/codex-auth/login", strings.NewReader(`{"id":"pool-1"}`))
	blocked.Header.Set("Authorization", "Bearer local-secret")
	blockedRR := httptest.NewRecorder()
	h.ServeHTTP(blockedRR, blocked)
	if blockedRR.Code != http.StatusNotFound {
		t.Fatalf("catalog token should not start login: %d %s", blockedRR.Code, blockedRR.Body.String())
	}

	start := httptest.NewRequest(http.MethodPost, "/api/codex-auth/login", strings.NewReader(`{"id":"pool-1"}`))
	start.Host = "127.0.0.1"
	startRR := httptest.NewRecorder()
	h.ServeHTTP(startRR, start)
	if startRR.Code != http.StatusOK {
		t.Fatalf("start status=%d body=%s", startRR.Code, startRR.Body.String())
	}
	var started map[string]any
	if err := json.Unmarshal(startRR.Body.Bytes(), &started); err != nil {
		t.Fatal(err)
	}
	flowID, _ := started["flowId"].(string)
	url, _ := started["url"].(string)
	if flowID == "" || !strings.Contains(url, "auth.openai.com") {
		t.Fatalf("start %+v", started)
	}

	code := httptest.NewRequest(http.MethodPost, "/api/codex-auth/login/code", strings.NewReader(`{"flowId":"`+flowID+`","input":"abc"}`))
	code.Host = "127.0.0.1"
	codeRR := httptest.NewRecorder()
	h.ServeHTTP(codeRR, code)
	if codeRR.Code != http.StatusAccepted && codeRR.Code != http.StatusOK {
		t.Fatalf("code status=%d body=%s", codeRR.Code, codeRR.Body.String())
	}

	status := httptest.NewRequest(http.MethodGet, "/api/codex-auth/login-status?flowId="+flowID, nil)
	status.Host = "127.0.0.1"
	statusRR := httptest.NewRecorder()
	h.ServeHTTP(statusRR, status)
	if !strings.Contains(statusRR.Body.String(), `"status":"done"`) {
		t.Fatalf("status=%s", statusRR.Body.String())
	}

	cfg, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cfg), `"id": "pool-1"`) && !strings.Contains(string(cfg), `"id":"pool-1"`) {
		t.Fatalf("config missing pool row: %s", cfg)
	}
	store, err := os.ReadFile(filepath.Join(home, "codex-accounts.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(store), "chatgptAccountId") || !strings.Contains(string(store), "pool-1") {
		t.Fatalf("store=%s", store)
	}
	if strings.Contains(string(cfg), "rt") {
		t.Fatalf("refresh token leaked into config: %s", cfg)
	}
}

func loginVerifierLeak(home string) string {
	raw, _ := os.ReadFile(filepath.Join(home, chatgpt.PendingFileName))
	return string(raw)
}
