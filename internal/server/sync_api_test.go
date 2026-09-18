package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSyncAPIInjectsLoopbackCodexRouting(t *testing.T) {
	codexHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(codexHome, "config.toml"), []byte("model = \"gpt-5\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		CodexHome:      codexHome,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	blocked := httptest.NewRequest(http.MethodPost, "/api/sync", nil)
	blocked.Header.Set("Authorization", "Bearer local-secret")
	blockedRR := httptest.NewRecorder()
	h.ServeHTTP(blockedRR, blocked)
	if blockedRR.Code != http.StatusNotFound {
		t.Fatalf("data-plane status=%d", blockedRR.Code)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/sync", nil)
	req.Host = "127.0.0.1:18080"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"ok":true`) {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"staleAppServerHint"`) {
		t.Fatalf("missing stale hint body=%s", rr.Body.String())
	}
	raw, err := os.ReadFile(filepath.Join(codexHome, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "http://127.0.0.1:18080/v1") {
		t.Fatalf("config=%s", raw)
	}

	second := httptest.NewRequest(http.MethodPost, "/api/sync", nil)
	second.Host = "127.0.0.1:18080"
	secondRR := httptest.NewRecorder()
	h.ServeHTTP(secondRR, second)
	if secondRR.Code != http.StatusOK || !strings.Contains(secondRR.Body.String(), `"changed":false`) {
		t.Fatalf("second status=%d body=%s", secondRR.Code, secondRR.Body.String())
	}
	if strings.Contains(secondRR.Body.String(), `"staleAppServerHint"`) {
		t.Fatalf("unchanged sync leaked hint body=%s", secondRR.Body.String())
	}
}
