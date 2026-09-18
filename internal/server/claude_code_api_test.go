package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/catalog"
)

func TestClaudeCodeAPIGetPutIsLoopbackAndOmitsSecrets(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"claudeCode":{"enabled":true,"authMode":"proxy"},"apiKeys":[{"key":"sk-secret"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		CatalogModels:  []catalog.Model{{ID: "openai-apikey/gpt-5.5"}},
		ConfigPath:     configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	blocked := httptest.NewRequest(http.MethodGet, "/api/claude-code", nil)
	blocked.Header.Set("Authorization", "Bearer local-secret")
	blockedRR := httptest.NewRecorder()
	h.ServeHTTP(blockedRR, blocked)
	if blockedRR.Code != http.StatusNotFound {
		t.Fatalf("data-plane status=%d body=%s", blockedRR.Code, blockedRR.Body.String())
	}
	req := httptest.NewRequest(http.MethodGet, "/api/claude-code", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "sk-secret") || strings.Contains(rr.Body.String(), `"apiKeys"`) {
		t.Fatalf("secret leaked: %s", rr.Body.String())
	}
	var got struct {
		Enabled   bool     `json:"enabled"`
		AuthMode  string   `json:"authMode"`
		Available []string `json:"available"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Enabled || got.AuthMode != "proxy" || strings.Join(got.Available, ",") != "openai-apikey/gpt-5.5" {
		t.Fatalf("%+v", got)
	}
	put := httptest.NewRequest(http.MethodPut, "/api/claude-code", strings.NewReader(`{"enabled":false,"authMode":"auto"}`))
	put.Host = "127.0.0.1"
	put.Header.Set("Content-Type", "application/json")
	putRR := httptest.NewRecorder()
	h.ServeHTTP(putRR, put)
	if putRR.Code != http.StatusOK {
		t.Fatalf("put status=%d body=%s", putRR.Code, putRR.Body.String())
	}
	body, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.Contains(text, `"enabled": false`) || strings.Contains(text, `"authMode"`) {
		t.Fatalf("config=%s", text)
	}
	if !strings.Contains(text, "sk-secret") {
		t.Fatal("unrelated apiKeys must remain")
	}
}
