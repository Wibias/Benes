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

func TestAgentSettingsAPIsAreLoopbackAndOmitSecrets(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte(`{"apiKeys":[{"key":"sk-secret"}],"injectionModel":"openai-apikey/gpt-5.5"}`), 0o600); err != nil {
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
	blocked := httptest.NewRequest(http.MethodGet, "/api/injection-model", nil)
	blocked.Header.Set("Authorization", "Bearer local-secret")
	blockedRR := httptest.NewRecorder()
	h.ServeHTTP(blockedRR, blocked)
	if blockedRR.Code != http.StatusNotFound {
		t.Fatalf("data-plane status=%d body=%s", blockedRR.Code, blockedRR.Body.String())
	}
	req := httptest.NewRequest(http.MethodGet, "/api/injection-model", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || strings.Contains(rr.Body.String(), "sk-secret") {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	put := httptest.NewRequest(http.MethodPut, "/api/effort-caps", strings.NewReader(`{"effortCap":"high"}`))
	put.Host = "127.0.0.1"
	putRR := httptest.NewRecorder()
	h.ServeHTTP(putRR, put)
	if putRR.Code != http.StatusOK {
		t.Fatalf("put status=%d body=%s", putRR.Code, putRR.Body.String())
	}
	var got struct {
		EffortCap string `json:"effortCap"`
	}
	if err := json.Unmarshal(putRR.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.EffortCap != "high" {
		t.Fatalf("%+v", got)
	}
	stored, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(stored), `"effortCap": "high"`) && !strings.Contains(string(stored), `"effortCap":"high"`) {
		t.Fatalf("stored=%s", stored)
	}
	if !strings.Contains(string(stored), "sk-secret") {
		t.Fatal("config rewrite dropped existing key field")
	}
}
