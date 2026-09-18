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
	"github.com/Wibias/Benes/internal/sidecar/websearch"
)

func TestSidecarSettingsAPIPersistsWithoutSecrets(t *testing.T) {
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
	blocked := httptest.NewRequest(http.MethodGet, "/api/sidecar-settings", nil)
	blocked.Header.Set("Authorization", "Bearer local-secret")
	blockedRR := httptest.NewRecorder()
	h.ServeHTTP(blockedRR, blocked)
	if blockedRR.Code != http.StatusNotFound {
		t.Fatalf("data-plane status=%d", blockedRR.Code)
	}
	put := httptest.NewRequest(http.MethodPut, "/api/sidecar-settings", strings.NewReader(`{"webSearch":{"backend":"openai","streamRoutedModelOutput":true}}`))
	put.Host = "127.0.0.1"
	putRR := httptest.NewRecorder()
	h.ServeHTTP(putRR, put)
	if putRR.Code != http.StatusOK || strings.Contains(putRR.Body.String(), "sk-secret") {
		t.Fatalf("status=%d body=%s", putRR.Code, putRR.Body.String())
	}
	if !strings.Contains(putRR.Body.String(), `"backend":"openai"`) {
		t.Fatalf("body=%s", putRR.Body.String())
	}
}

func TestSidecarSettingsAcceptsCapabilityClassesAndRejectsBrandGuesses(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil), "xai": providerFunc(nil)},
		ConfigPath:     configPath,
		CatalogModels: []catalog.Model{
			{ID: "openai-apikey/gpt-4o", Vision: catalog.CapabilityTrue},
			{ID: "xai/grok-4", Vision: catalog.CapabilityFalse},
			{ID: "custom/looks-like-vision", Vision: catalog.CapabilityUnknown},
		},
		WebSearch: map[string]websearch.Config{
			"exa": {Enabled: true, ProviderID: "exa", Endpoint: "https://api.exa.ai/search", AuthClass: "key", Wire: "search-json", ModelID: "exa-search", AllowedModels: []string{"exa-search"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	ok := httptest.NewRequest(http.MethodPut, "/api/sidecar-settings", strings.NewReader(`{"webSearch":{"backend":"dedicated_search"},"vision":{"model":"openai-apikey/gpt-4o","backend":"vision_describe"}}`))
	ok.Host = "127.0.0.1"
	okRR := httptest.NewRecorder()
	h.ServeHTTP(okRR, ok)
	if okRR.Code != http.StatusOK || !strings.Contains(okRR.Body.String(), `"class":"dedicated_search"`) {
		t.Fatalf("ok status=%d body=%s", okRR.Code, okRR.Body.String())
	}
	if !strings.Contains(okRR.Body.String(), `"openai-apikey/gpt-4o"`) {
		t.Fatalf("missing proven vision backend: %s", okRR.Body.String())
	}
	brand := httptest.NewRequest(http.MethodPut, "/api/sidecar-settings", strings.NewReader(`{"webSearch":{"backend":"gemini"}}`))
	brand.Host = "127.0.0.1"
	brandRR := httptest.NewRecorder()
	h.ServeHTTP(brandRR, brand)
	if brandRR.Code != http.StatusBadRequest {
		t.Fatalf("gemini status=%d body=%s", brandRR.Code, brandRR.Body.String())
	}
	blind := httptest.NewRequest(http.MethodPut, "/api/sidecar-settings", strings.NewReader(`{"vision":{"model":"xai/grok-4"}}`))
	blind.Host = "127.0.0.1"
	blindRR := httptest.NewRecorder()
	h.ServeHTTP(blindRR, blind)
	if blindRR.Code != http.StatusBadRequest {
		t.Fatalf("blind status=%d body=%s", blindRR.Code, blindRR.Body.String())
	}
	unknown := httptest.NewRequest(http.MethodPut, "/api/sidecar-settings", strings.NewReader(`{"vision":{"model":"custom/looks-like-vision"}}`))
	unknown.Host = "127.0.0.1"
	unknownRR := httptest.NewRecorder()
	h.ServeHTTP(unknownRR, unknown)
	if unknownRR.Code != http.StatusBadRequest {
		t.Fatalf("unknown status=%d body=%s", unknownRR.Code, unknownRR.Body.String())
	}
}

func TestSidecarSettingsWebSearchEnabledDefaultsTrue(t *testing.T) {
	h := newSidecarSettingsHandler(t, `{}`)
	view := putSidecarSettings(t, h, `{}`)
	if sidecarWebSearch(view)["enabled"] != true {
		t.Fatalf("default enabled=%v view=%v", sidecarWebSearch(view)["enabled"], view)
	}
	get := getSidecarSettings(t, h)
	if sidecarWebSearch(get)["enabled"] != true {
		t.Fatalf("get enabled=%v view=%v", sidecarWebSearch(get)["enabled"], get)
	}
}

func TestSidecarSettingsWebSearchEnabledFalsePersists(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte(`{"webSearchSidecar":{"model":"gpt-5.6-luna","backend":"openai","streamRoutedModelOutput":true}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	h := newSidecarSettingsHandlerAt(t, configPath)
	view := putSidecarSettings(t, h, `{"webSearch":{"enabled":false}}`)
	ws := sidecarWebSearch(view)
	if ws["enabled"] != false {
		t.Fatalf("enabled=%v view=%v", ws["enabled"], view)
	}
	if ws["model"] != "gpt-5.6-luna" || ws["backend"] != "openai" || ws["streamRoutedModelOutput"] != true {
		t.Fatalf("toggle dropped sibling fields: %v", ws)
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var disk map[string]any
	if json.Unmarshal(raw, &disk) != nil {
		t.Fatalf("disk=%s", raw)
	}
	stored, _ := disk["webSearchSidecar"].(map[string]any)
	if stored["enabled"] != false {
		t.Fatalf("disk enabled=%v payload=%s", stored["enabled"], raw)
	}
}

func TestSidecarSettingsWebSearchEnabledTrueOmitsDiskKey(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte(`{"webSearchSidecar":{"enabled":false,"model":"gpt-5.6-luna"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	h := newSidecarSettingsHandlerAt(t, configPath)
	view := putSidecarSettings(t, h, `{"webSearch":{"enabled":true}}`)
	if sidecarWebSearch(view)["enabled"] != true {
		t.Fatalf("enabled=%v view=%v", sidecarWebSearch(view)["enabled"], view)
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var disk map[string]any
	if json.Unmarshal(raw, &disk) != nil {
		t.Fatalf("disk=%s", raw)
	}
	stored, _ := disk["webSearchSidecar"].(map[string]any)
	if _, exists := stored["enabled"]; exists {
		t.Fatalf("enabled true must delete the disk key: %s", raw)
	}
	if stored["model"] != "gpt-5.6-luna" {
		t.Fatalf("model dropped: %s", raw)
	}
}

func TestSidecarSettingsWebSearchPartialUpdatePreservesEnabled(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte(`{"webSearchSidecar":{"enabled":false,"model":"gpt-5.6-luna","backend":"dedicated_search"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	h := newSidecarSettingsHandlerAt(t, configPath)
	view := putSidecarSettings(t, h, `{"webSearch":{"streamRoutedModelOutput":true}}`)
	ws := sidecarWebSearch(view)
	if ws["enabled"] != false {
		t.Fatalf("partial put dropped enabled: %v", ws)
	}
	if ws["model"] != "gpt-5.6-luna" || ws["backend"] != "dedicated_search" || ws["streamRoutedModelOutput"] != true {
		t.Fatalf("partial put dropped siblings: %v", ws)
	}
}

func newSidecarSettingsHandler(t *testing.T, configJSON string) http.Handler {
	t.Helper()
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte(configJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	return newSidecarSettingsHandlerAt(t, configPath)
}

func newSidecarSettingsHandlerAt(t *testing.T, configPath string) http.Handler {
	t.Helper()
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		ConfigPath:     configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	return h
}

func putSidecarSettings(t *testing.T, h http.Handler, body string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/api/sidecar-settings", strings.NewReader(body))
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("put status=%d body=%s", rr.Code, rr.Body.String())
	}
	return decodeJSONMap(t, rr.Body.Bytes())
}

func getSidecarSettings(t *testing.T, h http.Handler) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/sidecar-settings", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", rr.Code, rr.Body.String())
	}
	return decodeJSONMap(t, rr.Body.Bytes())
}

func sidecarWebSearch(view map[string]any) map[string]any {
	ws, _ := view["webSearch"].(map[string]any)
	return ws
}

func decodeJSONMap(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if json.Unmarshal(raw, &out) != nil || out == nil {
		t.Fatalf("json=%s", raw)
	}
	return out
}
