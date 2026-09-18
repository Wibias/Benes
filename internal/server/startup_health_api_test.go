package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStartupHealthAndShimActionOmitsSecrets(t *testing.T) {
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
	blocked := httptest.NewRequest(http.MethodGet, "/api/startup-health", nil)
	blocked.Header.Set("Authorization", "Bearer local-secret")
	blockedRR := httptest.NewRecorder()
	h.ServeHTTP(blockedRR, blocked)
	if blockedRR.Code != http.StatusNotFound {
		t.Fatalf("data-plane status=%d", blockedRR.Code)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/startup-health", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || strings.Contains(rr.Body.String(), "sk-secret") {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"routingKind"`) || !strings.Contains(rr.Body.String(), `"restoreNative"`) {
		t.Fatalf("health view missing dashboard fields: %s", rr.Body.String())
	}
	post := httptest.NewRequest(http.MethodPost, "/api/startup-action", strings.NewReader(`{"action":"install-shim"}`))
	post.Host = "127.0.0.1"
	postRR := httptest.NewRecorder()
	h.ServeHTTP(postRR, post)
	if postRR.Code != http.StatusOK || strings.Contains(postRR.Body.String(), "sk-secret") {
		t.Fatalf("post status=%d body=%s", postRR.Code, postRR.Body.String())
	}
}
