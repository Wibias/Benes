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

func TestModelsAPIListsCatalogAndHonorsDisabled(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"disabledModels":["openai-apikey/hidden"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		CatalogModels: []catalog.Model{
			{ID: "openai-apikey/gpt-5.5"},
			{ID: "openai-apikey/hidden"},
		},
		ConfigPath: configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	blocked := httptest.NewRequest(http.MethodGet, "/api/models", nil)
	blocked.Header.Set("Authorization", "Bearer local-secret")
	blockedRR := httptest.NewRecorder()
	h.ServeHTTP(blockedRR, blocked)
	if blockedRR.Code != http.StatusNotFound {
		t.Fatalf("data-plane status=%d body=%s", blockedRR.Code, blockedRR.Body.String())
	}
	req := httptest.NewRequest(http.MethodGet, "/api/models", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var rows []struct {
		Namespaced string `json:"namespaced"`
		Disabled   bool   `json:"disabled"`
		Provider   string `json:"provider"`
		ID         string `json:"id"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows=%v", rows)
	}
	byID := map[string]bool{}
	for _, row := range rows {
		byID[row.Namespaced] = row.Disabled
		if row.Provider != "openai-apikey" {
			t.Fatalf("provider=%s", row.Provider)
		}
	}
	if byID["openai-apikey/gpt-5.5"] || !byID["openai-apikey/hidden"] {
		t.Fatalf("disabled=%v", byID)
	}
}

func TestDisabledModelsPUTPersistsWithoutSecrets(t *testing.T) {
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
	req := httptest.NewRequest(http.MethodPut, "/api/disabled-models", strings.NewReader(`{"models":["b","a","b"]}`))
	req.Host = "127.0.0.1"
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "sk-secret") {
		t.Fatalf("secret leaked: %s", rr.Body.String())
	}
	body, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.Contains(text, `"a"`) || !strings.Contains(text, `"b"`) || !strings.Contains(text, "sk-secret") {
		t.Fatalf("config=%s", text)
	}
}
