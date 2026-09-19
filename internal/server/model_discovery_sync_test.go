package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/catalog"
	"github.com/Wibias/Benes/internal/modeldiscovery"
	"github.com/Wibias/Benes/internal/providers/antigravity"
	"github.com/Wibias/Benes/internal/providers/cursor"
)

func TestModelDiscoverySyncStoresFetchedIDsOnForwardOpenAI(t *testing.T) {
	original := discoverProviderModels
	t.Cleanup(func() { discoverProviderModels = original })
	discoverProviderModels = func(DiscoverModelsRequest) ([]modeldiscovery.CatalogEntry, error) {
		return modeldiscovery.EntriesFromIDs([]string{"gpt-5.4", "gpt-5.4-mini"}), nil
	}

	configPath := filepath.Join(t.TempDir(), "config.json")
	body := `{"providers":{"openai":{"adapter":"openai-responses","baseUrl":"https://chatgpt.com/backend-api/codex","authMode":"forward"}}}`
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai": providerFunc(nil)},
		ConfigPath:     configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)

	req := httptest.NewRequest(http.MethodPost, "/api/model-discovery", strings.NewReader(`{"provider":"openai"}`))
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("sync status=%d body=%s", rr.Code, rr.Body.String())
	}
	var syncBody struct {
		OK     bool     `json:"ok"`
		Models []string `json:"models"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &syncBody); err != nil {
		t.Fatal(err)
	}
	if !syncBody.OK || len(syncBody.Models) != 2 {
		t.Fatalf("sync=%s", rr.Body.String())
	}

	list := httptest.NewRequest(http.MethodGet, "/api/models", nil)
	list.Host = "127.0.0.1"
	listRR := httptest.NewRecorder()
	h.ServeHTTP(listRR, list)
	if listRR.Code != http.StatusOK {
		t.Fatalf("models status=%d body=%s", listRR.Code, listRR.Body.String())
	}
	if !strings.Contains(listRR.Body.String(), "gpt-5.4") || !strings.Contains(listRR.Body.String(), "gpt-5.4-mini") {
		t.Fatalf("models body=%s", listRR.Body.String())
	}

	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"gpt-5.4"`) {
		t.Fatalf("config missing models: %s", raw)
	}
}

func TestModelsListURLUsesCodexEndpointForForwardOpenAI(t *testing.T) {
	got := modelsListURL(DiscoverModelsRequest{
		AuthMode: "forward",
		BaseURL:  "https://chatgpt.com/backend-api/codex",
	})
	if got != codexModelsURL {
		t.Fatalf("url=%s", got)
	}
	keyURL := modelsListURL(DiscoverModelsRequest{
		AuthMode: "key",
		BaseURL:  "https://openrouter.ai/api/v1",
	})
	if keyURL != "https://openrouter.ai/api/v1/models" {
		t.Fatalf("key url=%s", keyURL)
	}
	anthropicURL := modelsListURL(DiscoverModelsRequest{
		Provider: "anthropic",
		AuthMode: "oauth",
		BaseURL:  "https://api.anthropic.com",
	})
	if anthropicURL != "https://api.anthropic.com/v1/models" {
		t.Fatalf("anthropic url=%s", anthropicURL)
	}
	commandCodeOAuth := modelsListURL(DiscoverModelsRequest{
		Provider: "command-code",
		AuthMode: "oauth",
		BaseURL:  "https://api.commandcode.ai",
	})
	if commandCodeOAuth != commandCodeModelsURL {
		t.Fatalf("command-code oauth url=%s", commandCodeOAuth)
	}
	commandCodeKey := modelsListURL(DiscoverModelsRequest{
		Provider: "commandcode",
		AuthMode: "key",
		BaseURL:  "https://api.commandcode.ai/provider/v1",
	})
	if commandCodeKey != commandCodeModelsURL {
		t.Fatalf("commandcode key url=%s", commandCodeKey)
	}
	googleURL := modelsListURL(DiscoverModelsRequest{
		Provider: "google",
		Adapter:  "google",
		AuthMode: "key",
		BaseURL:  "https://generativelanguage.googleapis.com",
	})
	if googleURL != "https://generativelanguage.googleapis.com/v1beta/models" {
		t.Fatalf("google url=%s", googleURL)
	}
	umansURL := modelsListURL(DiscoverModelsRequest{
		Provider: "umans",
		Adapter:  "anthropic",
		AuthMode: "key",
		BaseURL:  "https://api.code.umans.ai",
	})
	if umansURL != "https://api.code.umans.ai/v1/models" {
		t.Fatalf("umans url=%s", umansURL)
	}
	if modelsListURL(DiscoverModelsRequest{
		Provider: "cursor",
		Adapter:  "cursor",
		AuthMode: "oauth",
		BaseURL:  "https://api2.cursor.sh",
	}) != "" {
		t.Fatal("cursor must not use GET /models")
	}
}

func TestModelDiscoverySyncUsesOAuthStoreToken(t *testing.T) {
	original := discoverProviderModels
	t.Cleanup(func() { discoverProviderModels = original })
	var got DiscoverModelsRequest
	discoverProviderModels = func(req DiscoverModelsRequest) ([]modeldiscovery.CatalogEntry, error) {
		got = req
		return modeldiscovery.EntriesFromIDs([]string{"claude-sonnet-4-20250514"}), nil
	}

	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	authPath := filepath.Join(home, "auth.json")
	body := `{"providers":{"anthropic":{"adapter":"anthropic","baseUrl":"https://api.anthropic.com","authMode":"oauth"}}}`
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := antigravity.AppendStoredAccount(authPath, "anthropic", antigravity.StoredAccount{
		ID:    "acc-1",
		Token: "oauth-secret",
	}); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"anthropic": providerFunc(nil)},
		ConfigPath:     configPath,
		AuthStorePath:  authPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodPost, "/api/model-discovery", strings.NewReader(`{"provider":"anthropic"}`))
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if got.AccessToken != "oauth-secret" {
		t.Fatalf("access token=%q body=%s", got.AccessToken, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "claude-sonnet-4-20250514") {
		t.Fatalf("body=%s", rr.Body.String())
	}
}

func TestDefaultDiscoverProviderModelsSendsAnthropicOAuthHeaders(t *testing.T) {
	var gotAuth, gotKey, gotBeta, gotVersion, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotKey = r.Header.Get("x-api-key")
		gotBeta = r.Header.Get("anthropic-beta")
		gotVersion = r.Header.Get("anthropic-version")
		_, _ = w.Write([]byte(`{"data":[{"id":"claude-sonnet-4-20250514"}]}`))
	}))
	t.Cleanup(srv.Close)
	ids, err := defaultDiscoverProviderModels(DiscoverModelsRequest{
		Provider:    "anthropic",
		AuthMode:    "oauth",
		BaseURL:     srv.URL,
		AccessToken: "oauth-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0].ID != "claude-sonnet-4-20250514" {
		t.Fatalf("ids=%v", ids)
	}
	if gotPath != "/v1/models" {
		t.Fatalf("path=%s", gotPath)
	}
	if gotAuth != "Bearer oauth-secret" || gotKey != "" || gotBeta != "oauth-2025-04-20" || gotVersion != "2023-06-01" {
		t.Fatalf("auth=%q key=%q beta=%q version=%q", gotAuth, gotKey, gotBeta, gotVersion)
	}
}

func TestModelDiscoverySyncHonorsStaticCatalog(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	body := `{"providers":{"custom":{"adapter":"openai-chat","baseUrl":"https://example.invalid/v1","liveModels":false}}}`
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"custom": providerFunc(nil)},
		CatalogModels:  []catalog.Model{{ID: "custom/static-1"}},
		ConfigPath:     configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodPost, "/api/model-discovery", strings.NewReader(`{"provider":"custom"}`))
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"applicable":false`) {
		t.Fatalf("body=%s", rr.Body.String())
	}
	got := readWorkspaceAvailability(t, h)
	if got.StaleProviderCatalogues != nil || got.LastModelSync != nil {
		t.Fatalf("static catalog recorded a sync: %#v", got)
	}
}

func TestModelDiscoverySyncUpdatesWorkspaceAvailabilityWithoutRestart(t *testing.T) {
	original := discoverProviderModels
	t.Cleanup(func() { discoverProviderModels = original })
	discoverProviderModels = func(DiscoverModelsRequest) ([]modeldiscovery.CatalogEntry, error) {
		return modeldiscovery.EntriesFromIDs([]string{"gpt-5.4", "gpt-5.4-mini"}), nil
	}

	configPath := filepath.Join(t.TempDir(), "config.json")
	body := `{"providers":{"openai":{"adapter":"openai-responses","baseUrl":"https://chatgpt.com/backend-api/codex","authMode":"forward"}}}`
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai": providerFunc(nil)},
		CatalogModels:  []catalog.Model{{ID: "openai/gpt-old"}},
		ConfigPath:     configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)

	before := readWorkspaceAvailability(t, h)
	if before.ModelsAvailable != 1 || before.ModelsUnavailable != 0 {
		t.Fatalf("before available=%d unavailable=%d", before.ModelsAvailable, before.ModelsUnavailable)
	}
	if before.LastModelSync != nil {
		t.Fatalf("lastModelSync was set before sync: %v", *before.LastModelSync)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/model-discovery", strings.NewReader(`{"provider":"openai"}`))
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("sync status=%d body=%s", rr.Code, rr.Body.String())
	}

	after := readWorkspaceAvailability(t, h)
	if after.ModelsAvailable != 2 || after.ModelsUnavailable != 0 {
		t.Fatalf("after available=%d unavailable=%d body=%s", after.ModelsAvailable, after.ModelsUnavailable, workspaceAvailabilityBody(t, h))
	}
	if after.LastModelSync == nil || *after.LastModelSync <= 0 {
		t.Fatalf("lastModelSync=%v", after.LastModelSync)
	}
	if after.StaleProviderCatalogues != nil {
		t.Fatalf("successful sync left catalogues stale: %v", *after.StaleProviderCatalogues)
	}
}

func TestModelDiscoverySyncFailureMarksCatalogueStaleWithoutRestart(t *testing.T) {
	original := discoverProviderModels
	t.Cleanup(func() { discoverProviderModels = original })
	discoverProviderModels = func(DiscoverModelsRequest) ([]modeldiscovery.CatalogEntry, error) {
		return nil, errors.New("upstream model discovery failed")
	}

	configPath := filepath.Join(t.TempDir(), "config.json")
	body := `{"providers":{"openai":{"adapter":"openai-responses","baseUrl":"https://chatgpt.com/backend-api/codex","authMode":"forward"}}}`
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai": providerFunc(nil)},
		CatalogModels:  []catalog.Model{{ID: "openai/gpt-old"}},
		ConfigPath:     configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)

	req := httptest.NewRequest(http.MethodPost, "/api/model-discovery", strings.NewReader(`{"provider":"openai"}`))
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	got := readWorkspaceAvailability(t, h)
	if got.ModelsAvailable != 1 || got.ModelsUnavailable != 0 {
		t.Fatalf("failed sync mutated catalog: available=%d unavailable=%d", got.ModelsAvailable, got.ModelsUnavailable)
	}
	if got.StaleProviderCatalogues == nil || *got.StaleProviderCatalogues != 1 {
		t.Fatalf("stale=%v", got.StaleProviderCatalogues)
	}
	if got.LastModelSync != nil {
		t.Fatalf("failed sync set lastModelSync=%v", *got.LastModelSync)
	}
}

type workspaceAvailability struct {
	ModelsAvailable         int    `json:"modelsAvailable"`
	ModelsUnavailable       int    `json:"modelsUnavailable"`
	StaleProviderCatalogues *int   `json:"staleProviderCatalogues"`
	LastModelSync           *int64 `json:"lastModelSync"`
}

func readWorkspaceAvailability(t *testing.T, h http.Handler) workspaceAvailability {
	t.Helper()
	raw := workspaceAvailabilityBody(t, h)
	var body struct {
		Availability workspaceAvailability `json:"availability"`
	}
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		t.Fatal(err)
	}
	return body.Availability
}

func workspaceAvailabilityBody(t *testing.T, h http.Handler) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/providers/workspace", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("workspace status=%d body=%s", rr.Code, rr.Body.String())
	}
	return rr.Body.String()
}

func TestModelDiscoverySyncStoresAdvertisedContextWindows(t *testing.T) {
	original := discoverProviderModels
	t.Cleanup(func() { discoverProviderModels = original })
	discoverProviderModels = func(DiscoverModelsRequest) ([]modeldiscovery.CatalogEntry, error) {
		return []modeldiscovery.CatalogEntry{
			{ID: "claude-sonnet-5", ContextWindow: 1_000_000},
			{ID: "gpt-5.6-sol", ContextWindow: 1_050_000, MaxInput: 922_000},
		}, nil
	}

	configPath := filepath.Join(t.TempDir(), "config.json")
	body := `{"providers":{"command-code":{"adapter":"command-code","baseUrl":"https://api.commandcode.ai","authMode":"oauth","models":["deepseek/deepseek-v4-flash"]}}}`
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"command-code": providerFunc(nil)},
		ConfigPath:     configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)

	req := httptest.NewRequest(http.MethodPost, "/api/model-discovery", strings.NewReader(`{"provider":"command-code"}`))
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("sync status=%d body=%s", rr.Code, rr.Body.String())
	}

	list := httptest.NewRequest(http.MethodGet, "/api/models", nil)
	list.Host = "127.0.0.1"
	listRR := httptest.NewRecorder()
	h.ServeHTTP(listRR, list)
	if listRR.Code != http.StatusOK {
		t.Fatalf("models status=%d body=%s", listRR.Code, listRR.Body.String())
	}
	var rows []struct {
		ID            string `json:"id"`
		ContextWindow int    `json:"contextWindow"`
	}
	if err := json.Unmarshal(listRR.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, row := range rows {
		got[row.ID] = row.ContextWindow
	}
	if got["claude-sonnet-5"] != 1_000_000 || got["gpt-5.6-sol"] != 1_050_000 {
		t.Fatalf("context=%v body=%s", got, listRR.Body.String())
	}

	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"contextWindow": 1000000`) && !strings.Contains(string(raw), `"contextWindow":1000000`) {
		t.Fatalf("config missing advertised windows: %s", raw)
	}
	if !strings.Contains(string(raw), `"contextWindow": 1050000`) && !strings.Contains(string(raw), `"contextWindow":1050000`) {
		t.Fatalf("config missing gpt-5.6 window: %s", raw)
	}
}

func TestModelDiscoverySyncUsesAuthStoreWhenConfigAuthModeIsKey(t *testing.T) {
	original := discoverProviderModels
	t.Cleanup(func() { discoverProviderModels = original })
	var got DiscoverModelsRequest
	discoverProviderModels = func(req DiscoverModelsRequest) ([]modeldiscovery.CatalogEntry, error) {
		got = req
		return modeldiscovery.EntriesFromIDs([]string{"claude-sonnet-5"}), nil
	}

	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	authPath := filepath.Join(home, "auth.json")
	body := `{"providers":{"command-code":{"adapter":"command-code","baseUrl":"https://api.commandcode.ai","authMode":"key"}}}`
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := antigravity.AppendStoredAccount(authPath, "command-code", antigravity.StoredAccount{
		ID:    "acc-1",
		Token: "oauth-secret",
	}); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"command-code": providerFunc(nil)},
		ConfigPath:     configPath,
		AuthStorePath:  authPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodPost, "/api/model-discovery", strings.NewReader(`{"provider":"command-code"}`))
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if got.AccessToken != "oauth-secret" || got.AuthMode != "oauth" {
		t.Fatalf("got=%#v body=%s", got, rr.Body.String())
	}
}

func TestDefaultDiscoverProviderModelsParsesLiveContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-goog-api-key") != "gk-test" {
			t.Fatalf("google key=%q", r.Header.Get("x-goog-api-key"))
		}
		_, _ = w.Write([]byte(`{"models":[{"name":"models/gemini-3.7-flash","inputTokenLimit":1048576}]}`))
	}))
	t.Cleanup(srv.Close)
	entries, err := defaultDiscoverProviderModels(DiscoverModelsRequest{
		Provider: "google",
		Adapter:  "google",
		AuthMode: "key",
		BaseURL:  srv.URL,
		APIKey:   "gk-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].ID != "gemini-3.7-flash" || entries[0].ContextWindow != 1048576 {
		t.Fatalf("entries=%#v", entries)
	}
}

func TestDefaultDiscoverProviderModelsUsesCursorUsableModels(t *testing.T) {
	var gotPath, gotType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotType = r.Header.Get("Content-Type")
		_, _ = w.Write(cursor.EncodeProtoMessage(1, cursor.EncodeProtoString(1, "gpt-5.6-sol")))
	}))
	t.Cleanup(srv.Close)
	entries, err := defaultDiscoverProviderModels(DiscoverModelsRequest{
		Provider:    "cursor",
		Adapter:     "cursor",
		AuthMode:    "oauth",
		BaseURL:     srv.URL,
		AccessToken: "cursor-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != cursor.UsableModelsPath || gotType != cursor.DiscoveryContentType {
		t.Fatalf("path=%s type=%s", gotPath, gotType)
	}
	if len(entries) != 1 || entries[0].ID != "gpt-5.6-sol" {
		t.Fatalf("entries=%#v", entries)
	}
}

func TestReplaceProviderCatalogUsesCuratedContextOnlyWhenDiscoveryOmitsIt(t *testing.T) {
	h := &handler{}
	h.replaceProviderCatalog("opencode-go", []modeldiscovery.CatalogEntry{
		{ID: "qwen3.8-max"},
		{ID: "gpt-5.6-luna", ContextWindow: 900_000},
		{ID: "unknown-model"},
	})

	qwen := requireCatalogModel(t, h.catalogModels, "opencode-go/qwen3.8-max")
	if qwen.Context.Tokens != 1_000_000 || string(qwen.Context.Source) != "curated_metadata" {
		t.Fatalf("qwen context=%#v", qwen.Context)
	}
	requireDiscoveryContextEvidence(t, qwen.Context, "models.dev", "dff014f67f04d7f17b6a7024510c77dd4a5b47e5", "models/alibaba/qwen3.8-max.toml")

	luna := requireCatalogModel(t, h.catalogModels, "opencode-go/gpt-5.6-luna")
	if luna.Context.Tokens != 900_000 || luna.Context.Source != catalog.ContextDiscovered {
		t.Fatalf("live context lost to curated metadata: %#v", luna.Context)
	}
	requireDiscoveryNoContextEvidence(t, luna.Context)

	unknown := requireCatalogModel(t, h.catalogModels, "opencode-go/unknown-model")
	if unknown.Context.Tokens != catalog.ConservativeContextWindow || unknown.Context.Source != catalog.ContextConservativeDefault {
		t.Fatalf("unknown context=%#v", unknown.Context)
	}
	requireDiscoveryNoContextEvidence(t, unknown.Context)

	h.replaceProviderCatalog("other", []modeldiscovery.CatalogEntry{{ID: "qwen3.8-max"}})
	other := requireCatalogModel(t, h.catalogModels, "other/qwen3.8-max")
	if other.Context.Tokens != catalog.ConservativeContextWindow || other.Context.Source != catalog.ContextConservativeDefault {
		t.Fatalf("curated metadata crossed provider boundary: %#v", other.Context)
	}
}

func requireCatalogModel(t *testing.T, models []catalog.Model, id string) catalog.Model {
	t.Helper()
	for _, model := range models {
		if model.ID == id {
			return model
		}
	}
	t.Fatalf("model %q missing from %#v", id, models)
	return catalog.Model{}
}

func requireDiscoveryContextEvidence(t *testing.T, ctx catalog.ContextWindow, source, revision, path string) {
	t.Helper()
	raw, err := json.Marshal(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Evidence *struct {
			Source   string `json:"source"`
			Revision string `json:"revision"`
			Path     string `json:"path"`
		} `json:"evidence"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if body.Evidence == nil || body.Evidence.Source != source || body.Evidence.Revision != revision || body.Evidence.Path != path {
		t.Fatalf("context evidence=%s", raw)
	}
}

func requireDiscoveryNoContextEvidence(t *testing.T, ctx catalog.ContextWindow) {
	t.Helper()
	raw, err := json.Marshal(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if _, ok := body["evidence"]; ok {
		t.Fatalf("unexpected context evidence=%s", raw)
	}
}

