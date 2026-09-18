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

func TestModelDiscoveryAPIBootstrapsThenDisablesArrival(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	body := `{
		"apiKeys":[{"key":"sk-secret"}],
		"providers":{
			"openrouter":{
				"adapter":"openai-chat",
				"baseUrl":"https://openrouter.ai/api/v1",
				"apiKey":"sk-secret",
				"models":["openai/gpt-5.6-sol"]
			}
		}
	}`
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openrouter": providerFunc(nil)},
		CatalogModels:  []catalog.Model{{ID: "openai/gpt-5.6-sol"}},
		ConfigPath:     configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	blocked := httptest.NewRequest(http.MethodGet, "/api/model-discovery", nil)
	blocked.Header.Set("Authorization", "Bearer local-secret")
	blockedRR := httptest.NewRecorder()
	h.ServeHTTP(blockedRR, blocked)
	if blockedRR.Code != http.StatusNotFound {
		t.Fatalf("data-plane status=%d", blockedRR.Code)
	}

	put := httptest.NewRequest(http.MethodPut, "/api/model-discovery", strings.NewReader(`{"newModelPolicy":"off"}`))
	put.Host = "127.0.0.1"
	putRR := httptest.NewRecorder()
	h.ServeHTTP(putRR, put)
	if putRR.Code != http.StatusOK || strings.Contains(putRR.Body.String(), "sk-secret") {
		t.Fatalf("status=%d body=%s", putRR.Code, putRR.Body.String())
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"disabledModels"`) {
		t.Fatalf("bootstrap hid models: %s", raw)
	}

	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	providers, _ := doc["providers"].(map[string]any)
	owned, _ := providers["openrouter"].(map[string]any)
	owned["models"] = []any{"openai/gpt-5.6-sol", "x-ai/grok-5"}
	providers["openrouter"] = owned
	doc["providers"] = providers
	updated, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, updated, 0o600); err != nil {
		t.Fatal(err)
	}

	put = httptest.NewRequest(http.MethodPut, "/api/model-discovery", strings.NewReader(`{"newModelPolicy":"off"}`))
	put.Host = "127.0.0.1"
	putRR = httptest.NewRecorder()
	h.ServeHTTP(putRR, put)
	if putRR.Code != http.StatusOK {
		t.Fatalf("second put status=%d body=%s", putRR.Code, putRR.Body.String())
	}
	if !strings.Contains(putRR.Body.String(), "x-ai/grok-5") || !strings.Contains(putRR.Body.String(), "auto-disabled") {
		t.Fatalf("body=%s", putRR.Body.String())
	}
	if strings.Contains(putRR.Body.String(), "sk-secret") {
		t.Fatal("secret leaked")
	}

	list := httptest.NewRequest(http.MethodGet, "/api/models", nil)
	list.Host = "127.0.0.1"
	listRR := httptest.NewRecorder()
	h.ServeHTTP(listRR, list)
	if listRR.Code != http.StatusOK {
		t.Fatalf("models status=%d body=%s", listRR.Code, listRR.Body.String())
	}
	if !strings.Contains(listRR.Body.String(), "x-ai/grok-5") || !strings.Contains(listRR.Body.String(), `"disabled":true`) {
		t.Fatalf("models body=%s", listRR.Body.String())
	}
}
