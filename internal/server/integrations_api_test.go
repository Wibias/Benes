package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/catalog"
)

func TestClientIntegrationsToggleApplyAndJournal(t *testing.T) {
	home := t.TempDir()
	xdg := filepath.Join(home, "xdg")
	if err := os.MkdirAll(filepath.Join(xdg, "opencode"), 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"hostname":"127.0.0.1","port":18080}`), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		ConfigPath:     configPath,
		CatalogModels:  []catalog.Model{{ID: "openai-apikey/gpt-5.5", Context: catalog.ContextWindow{Tokens: 128000}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	t.Setenv("XDG_CONFIG_HOME", xdg)
	put := httptest.NewRequest(http.MethodPut, "/api/client-integrations/opencode", strings.NewReader(`{"enabled":true}`))
	put.Host = "127.0.0.1"
	putRR := httptest.NewRecorder()
	h.ServeHTTP(putRR, put)
	if putRR.Code != http.StatusOK {
		t.Fatalf("put=%d %s", putRR.Code, putRR.Body.String())
	}
	if strings.Contains(putRR.Body.String(), "sk-") || strings.Contains(putRR.Body.String(), "local-secret") {
		t.Fatalf("secret leaked: %s", putRR.Body.String())
	}
	journal := httptest.NewRequest(http.MethodGet, "/api/client-integrations/journal?client=opencode", nil)
	journal.Host = "127.0.0.1"
	journalRR := httptest.NewRecorder()
	h.ServeHTTP(journalRR, journal)
	if journalRR.Code != http.StatusOK || !strings.Contains(journalRR.Body.String(), `"kind":"apply"`) {
		t.Fatalf("journal=%d %s", journalRR.Code, journalRR.Body.String())
	}
}

func TestClientIntegrationsAPIOmitsSecretsOnLoopback(t *testing.T) {
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	blocked := httptest.NewRequest(http.MethodGet, "/api/client-integrations", nil)
	blocked.Header.Set("Authorization", "Bearer local-secret")
	blockedRR := httptest.NewRecorder()
	h.ServeHTTP(blockedRR, blocked)
	if blockedRR.Code != http.StatusNotFound {
		t.Fatalf("data-plane status=%d body=%s", blockedRR.Code, blockedRR.Body.String())
	}
	req := httptest.NewRequest(http.MethodGet, "/api/client-integrations", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "local-secret") || strings.Contains(rr.Body.String(), "sk-") || !strings.Contains(rr.Body.String(), `"clientId":"opencode"`) {
		t.Fatalf("body=%s", rr.Body.String())
	}
}
