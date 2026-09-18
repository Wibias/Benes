package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/oauth/loginown"
)

func TestOAuthLoginStartsAntigravityWithoutSecrets(t *testing.T) {
	loginown.ResetForTest()
	t.Cleanup(loginown.ResetForTest)
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte(`{"apiKeys":[{"key":"sk-secret"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		ConfigPath:     configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	blocked := httptest.NewRequest(http.MethodPost, "/api/oauth/login", strings.NewReader(`{"provider":"google-antigravity"}`))
	blocked.Header.Set("Authorization", "Bearer local-secret")
	blockedRR := httptest.NewRecorder()
	h.ServeHTTP(blockedRR, blocked)
	if blockedRR.Code != http.StatusNotFound {
		t.Fatalf("data-plane status=%d", blockedRR.Code)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/oauth/login", strings.NewReader(`{"provider":"google-antigravity"}`))
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || strings.Contains(rr.Body.String(), "sk-secret") || !strings.Contains(rr.Body.String(), `"url"`) {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	other := httptest.NewRequest(http.MethodPost, "/api/oauth/login", strings.NewReader(`{"provider":"not-a-provider"}`))
	other.Host = "127.0.0.1"
	otherRR := httptest.NewRecorder()
	h.ServeHTTP(otherRR, other)
	if otherRR.Code != http.StatusBadRequest {
		t.Fatalf("other status=%d body=%s", otherRR.Code, otherRR.Body.String())
	}
}

func TestOAuthLoginStartsCursorWithoutSecrets(t *testing.T) {
	loginown.ResetForTest()
	t.Cleanup(loginown.ResetForTest)
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte(`{"apiKeys":[{"key":"sk-secret"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		ConfigPath:     configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodPost, "/api/oauth/login", strings.NewReader(`{"provider":"cursor"}`))
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || strings.Contains(rr.Body.String(), "sk-secret") || !strings.Contains(rr.Body.String(), "cursor.com") {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "verifier") {
		t.Fatalf("verifier leaked: %s", rr.Body.String())
	}
}
