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

func TestSelectedModelsAPIPersistsAllowlistWithoutSecrets(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte(`{"apiKeys":[{"key":"sk-secret"}],"providers":{"openai-apikey":{"adapter":"openai-chat"}}}`), 0o600); err != nil {
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
	blocked := httptest.NewRequest(http.MethodGet, "/api/selected-models", nil)
	blocked.Header.Set("Authorization", "Bearer local-secret")
	blockedRR := httptest.NewRecorder()
	h.ServeHTTP(blockedRR, blocked)
	if blockedRR.Code != http.StatusNotFound {
		t.Fatalf("data-plane status=%d", blockedRR.Code)
	}
	put := httptest.NewRequest(http.MethodPut, "/api/selected-models", strings.NewReader(`{"provider":"openai-apikey","models":["gpt-5.5"]}`))
	put.Host = "127.0.0.1"
	putRR := httptest.NewRecorder()
	h.ServeHTTP(putRR, put)
	if putRR.Code != http.StatusOK || strings.Contains(putRR.Body.String(), "sk-secret") {
		t.Fatalf("status=%d body=%s", putRR.Code, putRR.Body.String())
	}
	var got struct {
		OK       bool     `json:"ok"`
		Selected []string `json:"selected"`
	}
	if err := json.Unmarshal(putRR.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.OK || strings.Join(got.Selected, ",") != "gpt-5.5" {
		t.Fatalf("%+v", got)
	}
}
