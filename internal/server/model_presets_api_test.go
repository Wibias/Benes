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

func TestModelPresetsAPIAppliesOpenRouterSeedWithoutSecrets(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	body := `{
		"apiKeys":[{"key":"sk-secret"}],
		"providers":{
			"openrouter":{
				"adapter":"openai-chat",
				"baseUrl":"https://openrouter.ai/api/v1",
				"apiKey":"sk-secret",
				"models":["openai/gpt-5.6-sol","openai/gpt-4o"]
			}
		}
	}`
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openrouter": providerFunc(nil)},
		CatalogModels: []catalog.Model{
			{ID: "openrouter/openai/gpt-5.6-sol"},
			{ID: "openrouter/openai/gpt-4o"},
		},
		ConfigPath: configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	blocked := httptest.NewRequest(http.MethodGet, "/api/model-presets", nil)
	blocked.Header.Set("Authorization", "Bearer local-secret")
	blockedRR := httptest.NewRecorder()
	h.ServeHTTP(blockedRR, blocked)
	if blockedRR.Code != http.StatusNotFound {
		t.Fatalf("data-plane status=%d", blockedRR.Code)
	}

	put := httptest.NewRequest(http.MethodPut, "/api/model-presets", strings.NewReader(`{"provider":"openrouter","mode":"preset"}`))
	put.Host = "127.0.0.1"
	putRR := httptest.NewRecorder()
	h.ServeHTTP(putRR, put)
	if putRR.Code != http.StatusOK || strings.Contains(putRR.Body.String(), "sk-secret") {
		t.Fatalf("status=%d body=%s", putRR.Code, putRR.Body.String())
	}
	var applied struct {
		OK       bool     `json:"ok"`
		Mode     string   `json:"mode"`
		Selected []string `json:"selected"`
	}
	if err := json.Unmarshal(putRR.Body.Bytes(), &applied); err != nil {
		t.Fatal(err)
	}
	if !applied.OK || applied.Mode != "preset" || strings.Join(applied.Selected, ",") != "openai/gpt-5.6-sol" {
		t.Fatalf("%+v body=%s", applied, putRR.Body.String())
	}

	get := httptest.NewRequest(http.MethodGet, "/api/model-presets", nil)
	get.Host = "127.0.0.1"
	getRR := httptest.NewRecorder()
	h.ServeHTTP(getRR, get)
	if getRR.Code != http.StatusOK || strings.Contains(getRR.Body.String(), "sk-secret") {
		t.Fatalf("get status=%d body=%s", getRR.Code, getRR.Body.String())
	}
	if !strings.Contains(getRR.Body.String(), `"mode":"preset"`) && !strings.Contains(getRR.Body.String(), `"mode": "preset"`) {
		t.Fatalf("get body=%s", getRR.Body.String())
	}

	selectPut := httptest.NewRequest(http.MethodPut, "/api/selected-models", strings.NewReader(`{"provider":"openrouter","models":["openai/gpt-4o"]}`))
	selectPut.Host = "127.0.0.1"
	selectRR := httptest.NewRecorder()
	h.ServeHTTP(selectRR, selectPut)
	if selectRR.Code != http.StatusOK {
		t.Fatalf("selected status=%d body=%s", selectRR.Code, selectRR.Body.String())
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"mode": "custom"`) && !strings.Contains(string(raw), `"mode":"custom"`) {
		t.Fatalf("selected write did not mark custom: %s", raw)
	}
}

func TestModelPresetsAPIZeroMatchKeepsSelection(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	body := `{
		"providers":{
			"openrouter":{
				"adapter":"openai-chat",
				"baseUrl":"https://openrouter.ai/api/v1",
				"apiKey":"k",
				"models":["openai/gpt-4o"],
				"selectedModels":["openai/gpt-4o"]
			}
		}
	}`
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openrouter": providerFunc(nil)},
		CatalogModels:  []catalog.Model{{ID: "openrouter/openai/gpt-4o"}},
		ConfigPath:     configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	put := httptest.NewRequest(http.MethodPut, "/api/model-presets", strings.NewReader(`{"provider":"openrouter","mode":"preset"}`))
	put.Host = "127.0.0.1"
	putRR := httptest.NewRecorder()
	h.ServeHTTP(putRR, put)
	if putRR.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", putRR.Code, putRR.Body.String())
	}
	if !strings.Contains(putRR.Body.String(), "0 catalog") {
		t.Fatalf("body=%s", putRR.Body.String())
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"selectedModels"`) {
		t.Fatalf("selection dropped: %s", raw)
	}
}

func TestModelPresetsAPIMatchesUnprefixedOpenRouterCatalogIDs(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	body := `{
		"providers":{
			"openrouter":{
				"adapter":"openai-chat",
				"baseUrl":"https://openrouter.ai/api/v1",
				"apiKey":"k"
			}
		}
	}`
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openrouter": providerFunc(nil)},
		CatalogModels:  []catalog.Model{{ID: "openai/gpt-5.6-sol"}, {ID: "openai/gpt-4o"}},
		ConfigPath:     configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	put := httptest.NewRequest(http.MethodPut, "/api/model-presets", strings.NewReader(`{"provider":"openrouter","mode":"preset"}`))
	put.Host = "127.0.0.1"
	putRR := httptest.NewRecorder()
	h.ServeHTTP(putRR, put)
	if putRR.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", putRR.Code, putRR.Body.String())
	}
	if !strings.Contains(putRR.Body.String(), "openai/gpt-5.6-sol") {
		t.Fatalf("unprefixed catalog id was not matched: %s", putRR.Body.String())
	}
}

func TestCustomModelPOSTWhilePresetKeepsAllowlistAndMarker(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	body := `{
		"providers":{
			"openrouter":{
				"adapter":"openai-chat",
				"baseUrl":"https://openrouter.ai/api/v1",
				"apiKey":"k",
				"selectedModels":["openai/gpt-5.6-sol"],
				"modelPreset":{"mode":"preset","appliedVersion":1}
			}
		}
	}`
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openrouter": providerFunc(nil)},
		ConfigPath:     configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	post := httptest.NewRequest(http.MethodPost, "/api/custom-models", strings.NewReader(`{"provider":"openrouter","modelId":"lab/fixture"}`))
	post.Host = "127.0.0.1"
	postRR := httptest.NewRecorder()
	h.ServeHTTP(postRR, post)
	if postRR.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", postRR.Code, postRR.Body.String())
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, `"lab/fixture"`) {
		t.Fatalf("custom id missing from allowlist: %s", text)
	}
	if !strings.Contains(text, `"mode": "preset"`) && !strings.Contains(text, `"mode":"preset"`) {
		t.Fatalf("preset marker flipped: %s", text)
	}
}
