package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodexNativeToggleInjectsWithoutSecrets(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("model = \"gpt-5\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		CodexHome:      home,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodPut, "/api/native-integrations/codex", strings.NewReader(`{"enabled":true}`))
	req.Host = "127.0.0.1:18080"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || strings.Contains(rr.Body.String(), "local-secret") {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	raw, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "http://127.0.0.1:18080/v1") {
		t.Fatalf("config=%s", raw)
	}
}
