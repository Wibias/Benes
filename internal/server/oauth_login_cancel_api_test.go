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

func TestOAuthLoginCancelRemovesPendingFile(t *testing.T) {
	loginown.ResetForTest()
	t.Cleanup(loginown.ResetForTest)
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	pending := filepath.Join(dir, "oauth-pending-google-antigravity.json")
	if err := os.WriteFile(configPath, []byte(`{"apiKeys":[{"key":"sk-secret"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pending, []byte(`{"state":"x"}`), 0o600); err != nil {
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
	req := httptest.NewRequest(http.MethodPost, "/api/oauth/login/cancel", strings.NewReader(`{"provider":"google-antigravity"}`))
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || strings.Contains(rr.Body.String(), "sk-secret") {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if _, err := os.Stat(pending); !os.IsNotExist(err) {
		t.Fatalf("pending still present: %v", err)
	}
}
