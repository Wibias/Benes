package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestStorageCodexLogsStatusIsLoopback(t *testing.T) {
	codexHome := t.TempDir()
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		ConfigPath:     filepath.Join(t.TempDir(), "config.json"),
		CodexHome:      codexHome,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	blocked := httptest.NewRequest(http.MethodGet, "/api/storage/codex-logs", nil)
	blocked.Header.Set("Authorization", "Bearer local-secret")
	blockedRR := httptest.NewRecorder()
	h.ServeHTTP(blockedRR, blocked)
	if blockedRR.Code != http.StatusNotFound {
		t.Fatalf("data-plane status=%d", blockedRR.Code)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/storage/codex-logs", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"protection"`) {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}
