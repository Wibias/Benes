package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/oauth/kimi"
	"github.com/Wibias/Benes/internal/oauth/loginown"
)

type rt func(*http.Request) (*http.Response, error)

func (f rt) Do(req *http.Request) (*http.Response, error) { return f(req) }

func jsonResp(code int, body any) *http.Response {
	raw, _ := json.Marshal(body)
	return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(string(raw))), Header: make(http.Header)}
}

func TestOAuthLoginStartsKimiWithoutDeviceSecret(t *testing.T) {
	loginown.ResetForTest()
	t.Cleanup(loginown.ResetForTest)
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"apiKeys":[{"key":"sk-secret"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	prevC, prevI := kimi.HTTPClient, kimi.Interval
	t.Cleanup(func() { kimi.HTTPClient, kimi.Interval = prevC, prevI })
	kimi.Interval = time.Nanosecond
	kimi.HTTPClient = rt(func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Path, "device_authorization") {
			return jsonResp(200, map[string]any{"user_code": "ABCD", "device_code": "dev-secret", "verification_uri": "https://auth.kimi.com/device", "expires_in": 600, "interval": 1}), nil
		}
		return jsonResp(200, map[string]any{"access_token": "tok", "refresh_token": "ref", "expires_in": 3600}), nil
	})
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		ConfigPath:     configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodPost, "/api/oauth/login", strings.NewReader(`{"provider":"kimi"}`))
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || strings.Contains(rr.Body.String(), "dev-secret") || strings.Contains(rr.Body.String(), "sk-secret") || !strings.Contains(rr.Body.String(), "ABCD") {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}
