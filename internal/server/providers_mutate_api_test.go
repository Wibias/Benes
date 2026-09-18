package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/credentials"
)

func TestProvidersPATCHPersistsDiscoveryAndPacingWithoutSecrets(t *testing.T) {
	h, configPath := newProvidersMutateHandler(t, `{
		"port": 23100,
		"defaultProvider": "openai",
		"apiKeys": [{"key": "sk-secret"}],
		"providers": {
			"openai": {
				"adapter": "openai-responses",
				"baseUrl": "https://chatgpt.com/backend-api/codex",
				"authMode": "forward",
				"codexAccountMode": "pool"
			}
		}
	}`, "openai")

	blocked := httptest.NewRequest(http.MethodPatch, "/api/providers?name=openai", strings.NewReader(`{"allowPrivateNetwork":false}`))
	blocked.Header.Set("Authorization", "Bearer local-secret")
	blockedRR := httptest.NewRecorder()
	h.ServeHTTP(blockedRR, blocked)
	if blockedRR.Code != http.StatusNotFound {
		t.Fatalf("data-plane status=%d body=%s", blockedRR.Code, blockedRR.Body.String())
	}

	patch := loopbackJSON(http.MethodPatch, "/api/providers?name=openai", `{
		"allowPrivateNetwork": false,
		"liveModels": true,
		"requestPacing": {"enabled": true, "requestsPerMinute": 30, "minIntervalMs": 250}
	}`)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, patch)
	if rr.Code != http.StatusOK || strings.Contains(rr.Body.String(), "sk-secret") {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	got := getConfig(t, h)
	prov := got.Providers["openai"]
	if prov.AllowPrivateNetwork || prov.LiveModels == nil || !*prov.LiveModels {
		t.Fatalf("discovery=%#v", prov)
	}
	if prov.RequestPacing == nil || !prov.RequestPacing.Enabled || prov.RequestPacing.MinIntervalMs != 250 || prov.RequestPacing.RequestsPerMinute != 30 {
		t.Fatalf("pacing=%#v", prov.RequestPacing)
	}
	if prov.RequestPacingMs != 250 {
		t.Fatalf("requestPacingMs=%d", prov.RequestPacingMs)
	}

	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "sk-secret") && strings.Contains(rr.Body.String(), "sk-secret") {
		t.Fatalf("secret leaked in response")
	}
}

func TestProvidersPATCHSetDefaultAlone(t *testing.T) {
	h, configPath := newProvidersMutateHandler(t, `{
		"defaultProvider": "openai",
		"providers": {
			"openai": {"adapter": "openai-responses", "baseUrl": "https://chatgpt.com/backend-api/codex", "authMode": "forward"},
			"openai-apikey": {"adapter": "openai-chat", "baseUrl": "https://api.openai.com/v1"}
		}
	}`, "openai", "openai-apikey")

	mixed := loopbackJSON(http.MethodPatch, "/api/providers?name=openai-apikey", `{"setDefault": true, "note": "nope"}`)
	mixedRR := httptest.NewRecorder()
	h.ServeHTTP(mixedRR, mixed)
	if mixedRR.Code != http.StatusBadRequest {
		t.Fatalf("mixed status=%d body=%s", mixedRR.Code, mixedRR.Body.String())
	}

	ok := loopbackJSON(http.MethodPatch, "/api/providers?name=openai-apikey", `{"setDefault": true}`)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, ok)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	got := getConfig(t, h)
	if got.DefaultProvider != "openai-apikey" {
		t.Fatalf("default=%q", got.DefaultProvider)
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"defaultProvider": "openai-apikey"`) && !strings.Contains(string(raw), `"defaultProvider":"openai-apikey"`) {
		t.Fatalf("disk=%s", raw)
	}
}

func TestProvidersPATCHDisableDefaultReturnsCode(t *testing.T) {
	h, _ := newProvidersMutateHandler(t, `{
		"defaultProvider": "openai",
		"providers": {
			"openai": {"adapter": "openai-responses", "baseUrl": "https://chatgpt.com/backend-api/codex", "authMode": "forward"},
			"openai-apikey": {"adapter": "openai-chat", "baseUrl": "https://api.openai.com/v1"}
		}
	}`, "openai", "openai-apikey")

	req := loopbackJSON(http.MethodPatch, "/api/providers?name=openai", `{"disabled": true}`)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != "default_provider_disabled" {
		t.Fatalf("body=%s", rr.Body.String())
	}
}

func TestProvidersDELETEGuardsLastProviderAndCombos(t *testing.T) {
	h, _ := newProvidersMutateHandler(t, `{
		"defaultProvider": "openai",
		"providers": {
			"openai": {"adapter": "openai-responses", "baseUrl": "https://chatgpt.com/backend-api/codex", "authMode": "forward"}
		}
	}`, "openai")

	last := httptest.NewRequest(http.MethodDelete, "/api/providers?name=openai", nil)
	last.Host = "127.0.0.1"
	lastRR := httptest.NewRecorder()
	h.ServeHTTP(lastRR, last)
	if lastRR.Code != http.StatusConflict {
		t.Fatalf("last status=%d body=%s", lastRR.Code, lastRR.Body.String())
	}
	var lastBody struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(lastRR.Body.Bytes(), &lastBody); err != nil || lastBody.Code != "last_provider" {
		t.Fatalf("last body=%s", lastRR.Body.String())
	}

	comboH, _ := newProvidersMutateHandler(t, `{
		"defaultProvider": "openai",
		"providers": {
			"openai": {"adapter": "openai-responses", "baseUrl": "https://chatgpt.com/backend-api/codex", "authMode": "forward"},
			"openai-apikey": {"adapter": "openai-chat", "baseUrl": "https://api.openai.com/v1"}
		},
		"combos": {
			"backup": {"strategy": "failover", "targets": [{"provider": "openai-apikey", "model": "gpt-4o"}]}
		}
	}`, "openai", "openai-apikey")
	combo := httptest.NewRequest(http.MethodDelete, "/api/providers?name=openai-apikey", nil)
	combo.Host = "127.0.0.1"
	comboRR := httptest.NewRecorder()
	comboH.ServeHTTP(comboRR, combo)
	if comboRR.Code != http.StatusConflict {
		t.Fatalf("combo status=%d body=%s", comboRR.Code, comboRR.Body.String())
	}
	var comboBody struct {
		Code   string   `json:"code"`
		Combos []string `json:"combos"`
	}
	if err := json.Unmarshal(comboRR.Body.Bytes(), &comboBody); err != nil {
		t.Fatal(err)
	}
	if comboBody.Code != "provider_has_dependent_combos" || strings.Join(comboBody.Combos, ",") != "backup" {
		t.Fatalf("combo body=%s", comboRR.Body.String())
	}
}

func TestProvidersDELETEClearsOAuthSessionsAndAPIKeys(t *testing.T) {
	store, err := credentials.NewFileStore(filepath.Join(t.TempDir(), "creds"), false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Put("command-code", []byte("sk-leftover-secret")); err != nil {
		t.Fatal(err)
	}
	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "config.json")
	authPath := filepath.Join(configDir, "auth.json")
	pendingPath := filepath.Join(configDir, "oauth-pending-command-code.json")
	if err := os.WriteFile(configPath, []byte(`{
		"defaultProvider": "openai",
		"providers": {
			"openai": {"adapter": "openai-responses", "baseUrl": "https://chatgpt.com/backend-api/codex", "authMode": "forward"},
			"command-code": {"adapter": "openai-chat", "baseUrl": "https://api.commandcode.ai", "authMode": "oauth", "credentialRef": {"id": "command-code", "source": "secure-store"}}
		}
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(authPath, []byte(`{
		"command-code": {"activeAccountId": "acc-1", "accounts": [{"id": "acc-1", "credential": {"access": "tok-secret", "refresh": "rt-secret"}}]},
		"anthropic": {"activeAccountId": "a", "accounts": [{"id": "a", "credential": {"access": "keep-me", "refresh": "keep-rt"}}]}
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pendingPath, []byte(`{"state":"pending"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai": providerFunc(nil), "command-code": providerFunc(nil)},
		ConfigPath:     configPath,
		Credentials:    store,
		AuthStorePath:  authPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)

	req := httptest.NewRequest(http.MethodDelete, "/api/providers?name=command-code", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	raw, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "command-code") || strings.Contains(string(raw), "tok-secret") {
		t.Fatalf("oauth leftover: %s", raw)
	}
	if !strings.Contains(string(raw), "anthropic") {
		t.Fatalf("cleared unrelated oauth: %s", raw)
	}
	if _, err := os.Stat(pendingPath); !os.IsNotExist(err) {
		t.Fatalf("pending login leftover err=%v", err)
	}
	if _, err := store.Get(credentials.Ref{ID: "command-code", Source: credentials.SourceSecureStore}); err == nil {
		t.Fatal("api key leftover")
	}
}

func TestProvidersPOSTAddsProviderAndOmitsSecrets(t *testing.T) {
	store, err := credentials.NewFileStore(filepath.Join(t.TempDir(), "creds"), false)
	if err != nil {
		t.Fatal(err)
	}
	h, configPath := newProvidersMutateHandlerWithCreds(t, `{
		"defaultProvider": "openai",
		"providers": {
			"openai": {"adapter": "openai-responses", "baseUrl": "https://chatgpt.com/backend-api/codex", "authMode": "forward"}
		}
	}`, store, "openai")

	dup := loopbackJSON(http.MethodPost, "/api/providers", `{
		"name": "openai",
		"provider": {"adapter": "openai-chat", "baseUrl": "https://api.openai.com/v1"}
	}`)
	dupRR := httptest.NewRecorder()
	h.ServeHTTP(dupRR, dup)
	if dupRR.Code != http.StatusConflict {
		t.Fatalf("dup status=%d body=%s", dupRR.Code, dupRR.Body.String())
	}

	req := loopbackJSON(http.MethodPost, "/api/providers", `{
		"name": "openai-apikey",
		"provider": {
			"adapter": "openai-chat",
			"baseUrl": "https://api.openai.com/v1",
			"authMode": "key",
			"apiKey": "sk-new-secret",
			"defaultModel": "gpt-4o"
		}
	}`)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated && rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "sk-new-secret") {
		t.Fatalf("secret echoed: %s", rr.Body.String())
	}

	got := getConfig(t, h)
	prov := got.Providers["openai-apikey"]
	if prov.Adapter != "openai-chat" || prov.BaseURL != "https://api.openai.com/v1" || prov.DefaultModel != "gpt-4o" || !prov.HasAPIKey {
		t.Fatalf("added=%#v", prov)
	}

	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "sk-new-secret") {
		t.Fatalf("plaintext key persisted: %s", raw)
	}
}

func TestProvidersPOSTSeedsOpenRouterPresetWithoutEmptyAllowlist(t *testing.T) {
	h, configPath := newProvidersMutateHandler(t, `{
		"defaultProvider": "openai",
		"providers": {
			"openai": {"adapter": "openai-responses", "baseUrl": "https://chatgpt.com/backend-api/codex", "authMode": "forward"}
		}
	}`, "openai")

	req := loopbackJSON(http.MethodPost, "/api/providers", `{
		"name": "openrouter",
		"provider": {
			"adapter": "openai-chat",
			"baseUrl": "https://openrouter.ai/api/v1",
			"models": ["openai/gpt-5.6-sol", "openai/gpt-4o"]
		}
	}`)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, `"openai/gpt-5.6-sol"`) {
		t.Fatalf("preset seed missing: %s", text)
	}
	if strings.Contains(text, `"selectedModels": []`) || strings.Contains(text, `"selectedModels":[]`) {
		t.Fatalf("empty allowlist written: %s", text)
	}
	if !strings.Contains(text, `"mode": "preset"`) && !strings.Contains(text, `"mode":"preset"`) {
		t.Fatalf("preset marker missing: %s", text)
	}
}

func TestConfigAPIReturnsPublicProviderFieldsFromDisk(t *testing.T) {
	h, _ := newProvidersMutateHandler(t, `{
		"port": 23100,
		"defaultProvider": "openai",
		"providers": {
			"openai": {
				"adapter": "openai-responses",
				"baseUrl": "https://chatgpt.com/backend-api/codex",
				"authMode": "forward",
				"note": "pool",
				"codexAccountMode": "pool",
				"apiKey": "sk-secret"
			}
		}
	}`, "openai")

	got := getConfig(t, h)
	if got.Port != 23100 || got.DefaultProvider != "openai" {
		t.Fatalf("root=%#v", got)
	}
	prov := got.Providers["openai"]
	if prov.Adapter != "openai-responses" || prov.BaseURL != "https://chatgpt.com/backend-api/codex" || prov.Note != "pool" || prov.CodexAccountMode != "pool" {
		t.Fatalf("provider=%#v", prov)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if strings.Contains(rr.Body.String(), "sk-secret") {
		t.Fatalf("secret leaked: %s", rr.Body.String())
	}
}

func TestProvidersPATCHCodexAccountModeAlone(t *testing.T) {
	h, _ := newProvidersMutateHandler(t, `{
		"defaultProvider": "openai",
		"providers": {
			"openai": {
				"adapter": "openai-responses",
				"baseUrl": "https://chatgpt.com/backend-api/codex",
				"authMode": "forward",
				"codexAccountMode": "pool"
			}
		}
	}`, "openai")

	req := loopbackJSON(http.MethodPatch, "/api/providers?name=openai", `{"codexAccountMode": "direct"}`)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	got := getConfig(t, h)
	if got.Providers["openai"].CodexAccountMode != "direct" {
		t.Fatalf("mode=%#v", got.Providers["openai"])
	}
}

func TestProvidersDELETERemovesNonDefault(t *testing.T) {
	h, _ := newProvidersMutateHandler(t, `{
		"defaultProvider": "openai",
		"providers": {
			"openai": {"adapter": "openai-responses", "baseUrl": "https://chatgpt.com/backend-api/codex", "authMode": "forward"},
			"openai-apikey": {"adapter": "openai-chat", "baseUrl": "https://api.openai.com/v1"}
		}
	}`, "openai", "openai-apikey")

	req := httptest.NewRequest(http.MethodDelete, "/api/providers?name=openai-apikey", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	got := getConfig(t, h)
	if _, ok := got.Providers["openai-apikey"]; ok {
		t.Fatalf("still present: %#v", got.Providers)
	}
}

type configAPIBody struct {
	Port            int    `json:"port"`
	DefaultProvider string `json:"defaultProvider"`
	Providers       map[string]struct {
		Adapter             string `json:"adapter"`
		BaseURL             string `json:"baseUrl"`
		DefaultModel        string `json:"defaultModel"`
		HasAPIKey           bool   `json:"hasApiKey"`
		LiveModels          *bool  `json:"liveModels"`
		AuthMode            string `json:"authMode"`
		Disabled            bool   `json:"disabled"`
		Note                string `json:"note"`
		AllowPrivateNetwork bool   `json:"allowPrivateNetwork"`
		RequestPacingMs     int    `json:"requestPacingMs"`
		CodexAccountMode    string `json:"codexAccountMode"`
		RequestPacing       *struct {
			Enabled           bool `json:"enabled"`
			RequestsPerMinute int  `json:"requestsPerMinute"`
			MinIntervalMs     int  `json:"minIntervalMs"`
		} `json:"requestPacing"`
	} `json:"providers"`
}

func getConfig(t *testing.T, h http.Handler) configAPIBody {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/config status=%d body=%s", rr.Code, rr.Body.String())
	}
	var got configAPIBody
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func loopbackJSON(method, path, body string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Host = "127.0.0.1"
	req.Header.Set("Content-Type", "application/json")
	return req
}

func newProvidersMutateHandler(t *testing.T, configJSON string, names ...string) (http.Handler, string) {
	t.Helper()
	return newProvidersMutateHandlerWithCreds(t, configJSON, nil, names...)
}

func newProvidersMutateHandlerWithCreds(t *testing.T, configJSON string, store credentials.Store, names ...string) (http.Handler, string) {
	t.Helper()
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte(configJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	providers := map[string]Provider{}
	for _, name := range names {
		providers[name] = providerFunc(nil)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      providers,
		ConfigPath:     configPath,
		Credentials:    store,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	return h, configPath
}
