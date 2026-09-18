package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/credentials"
)

func TestConfigAPIOmitsSecretsOnLoopback(t *testing.T) {
	store, err := credentials.NewFileStore(filepath.Join(t.TempDir(), "creds"), false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Put("openai-apikey", []byte("sk-secret")); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		Credentials:    store,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	blocked := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	blocked.Header.Set("Authorization", "Bearer local-secret")
	blockedRR := httptest.NewRecorder()
	h.ServeHTTP(blockedRR, blocked)
	if blockedRR.Code != http.StatusNotFound {
		t.Fatalf("data-plane status=%d body=%s", blockedRR.Code, blockedRR.Body.String())
	}
	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "sk-secret") || strings.Contains(rr.Body.String(), "apiKey") {
		t.Fatalf("secret leaked: %s", rr.Body.String())
	}
	var body struct {
		Providers map[string]struct {
			HasAPIKey bool `json:"hasApiKey"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Providers["openai-apikey"].HasAPIKey {
		t.Fatalf("body=%#v", body)
	}
}

func TestConfigMutationsAPIIsLoopbackAndNewestFirst(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"providers":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	store := config.NewTransactionStore(configPath, 1<<20)
	tx, err := store.Begin()
	if err != nil {
		t.Fatal(err)
	}
	tx.SetSource(config.MutationSource{Class: config.SourceAPI, Detail: "/api/credentials"})
	if err := tx.Set(config.JSONPath("port"), json.RawMessage(`23100`)); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Commit(); err != nil {
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
	blocked := httptest.NewRequest(http.MethodGet, "/api/config/mutations", nil)
	blocked.Header.Set("Authorization", "Bearer local-secret")
	blockedRR := httptest.NewRecorder()
	h.ServeHTTP(blockedRR, blocked)
	if blockedRR.Code != http.StatusNotFound {
		t.Fatalf("data-plane status=%d body=%s", blockedRR.Code, blockedRR.Body.String())
	}
	req := httptest.NewRequest(http.MethodGet, "/api/config/mutations?limit=1", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "sk-") {
		t.Fatalf("secret leaked: %s", rr.Body.String())
	}
	var records []config.MutationRecord
	if err := json.Unmarshal(rr.Body.Bytes(), &records); err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Source.Class != config.SourceAPI || records[0].Changes[0].Path != "/port" {
		t.Fatalf("records=%#v", records)
	}
}
