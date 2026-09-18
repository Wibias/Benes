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

func TestProvidersAPIOmitsSecrets(t *testing.T) {
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
	blocked := httptest.NewRequest(http.MethodGet, "/api/providers", nil)
	blocked.Header.Set("Authorization", "Bearer local-secret")
	blockedRR := httptest.NewRecorder()
	h.ServeHTTP(blockedRR, blocked)
	if blockedRR.Code != http.StatusNotFound {
		t.Fatalf("data-plane status=%d body=%s", blockedRR.Code, blockedRR.Body.String())
	}
	req := httptest.NewRequest(http.MethodGet, "/api/providers", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "sk-secret") {
		t.Fatalf("secret leaked: %s", rr.Body.String())
	}
	var body []struct {
		Name      string `json:"name"`
		HasAPIKey bool   `json:"hasApiKey"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body) != 1 || body[0].Name != "openai-apikey" || !body[0].HasAPIKey {
		t.Fatalf("body=%#v", body)
	}
}

func TestProvidersKeysAPIOmitsSecrets(t *testing.T) {
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
	req := httptest.NewRequest(http.MethodGet, "/api/providers/keys?name=openai-apikey", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "sk-secret") {
		t.Fatalf("secret leaked: %s", rr.Body.String())
	}
	var body struct {
		ActiveID string `json:"activeId"`
		Keys     []struct {
			ID     string `json:"id"`
			Masked string `json:"masked"`
			Active bool   `json:"active"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.ActiveID != "openai-apikey" || len(body.Keys) != 1 || body.Keys[0].ID != "openai-apikey" || !body.Keys[0].Active || body.Keys[0].Masked == "sk-secret" {
		t.Fatalf("body=%#v", body)
	}
}

func TestProvidersKeysPOSTStoresWithoutEchoAndScrubsConfig(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"providers":{"openai-apikey":{"adapter":"openai-chat","baseUrl":"https://api.openai.com/v1","apiKey":"sk-old"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := credentials.NewFileStore(filepath.Join(home, "creds"), false)
	if err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		Credentials:    store,
		ConfigPath:     configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodPost, "/api/providers/keys", strings.NewReader(`{"name":"openai-apikey","key":"sk-secret"}`))
	req.Host = "127.0.0.1"
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "sk-secret") {
		t.Fatalf("echoed secret: %s", rr.Body.String())
	}
	got, err := store.Get(credentials.Ref{ID: "746b4ad1", Source: credentials.SourceSecureStore})
	if err != nil || string(got) != "sk-secret" {
		t.Fatalf("stored=%q %v", got, err)
	}
	committed, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(committed)
	if strings.Contains(text, "sk-secret") || strings.Contains(text, "sk-old") {
		t.Fatalf("plaintext remained: %s", text)
	}
	if !strings.Contains(text, `"credentialRef"`) {
		t.Fatalf("ref missing: %s", text)
	}
}

func TestProvidersKeysDELETERemovesStoredKeyWithoutEcho(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"providers":{"openai-apikey":{"adapter":"openai-chat","baseUrl":"https://api.openai.com/v1"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := credentials.NewFileStore(filepath.Join(home, "creds"), false)
	if err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		Credentials:    store,
		ConfigPath:     configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	post := httptest.NewRequest(http.MethodPost, "/api/providers/keys", strings.NewReader(`{"name":"openai-apikey","key":"sk-secret"}`))
	post.Host = "127.0.0.1"
	post.Header.Set("Content-Type", "application/json")
	postRR := httptest.NewRecorder()
	h.ServeHTTP(postRR, post)
	if postRR.Code != http.StatusCreated {
		t.Fatalf("post status=%d body=%s", postRR.Code, postRR.Body.String())
	}
	req := httptest.NewRequest(http.MethodDelete, "/api/providers/keys?name=openai-apikey&id=746b4ad1", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "sk-secret") {
		t.Fatalf("echoed secret: %s", rr.Body.String())
	}
	if _, err := store.Get(credentials.Ref{ID: "746b4ad1", Source: credentials.SourceSecureStore}); err == nil {
		t.Fatal("key still stored")
	}
	committed, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(committed), "sk-secret") || strings.Contains(string(committed), `"credentialRef"`) {
		t.Fatalf("ref remained: %s", committed)
	}
}

func TestProvidersKeysPUTActiveAndAlias(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"providers":{"openai-apikey":{"adapter":"openai-chat","baseUrl":"https://api.openai.com/v1"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := credentials.NewFileStore(filepath.Join(home, "creds"), false)
	if err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		Credentials:    store,
		ConfigPath:     configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	first := httptest.NewRequest(http.MethodPost, "/api/providers/keys", strings.NewReader(`{"name":"openai-apikey","key":"sk-secret","label":"one"}`))
	first.Host = "127.0.0.1"
	first.Header.Set("Content-Type", "application/json")
	firstRR := httptest.NewRecorder()
	h.ServeHTTP(firstRR, first)
	if firstRR.Code != http.StatusCreated {
		t.Fatalf("first=%d %s", firstRR.Code, firstRR.Body.String())
	}
	second := httptest.NewRequest(http.MethodPost, "/api/providers/keys", strings.NewReader(`{"name":"openai-apikey","key":"sk-other-key"}`))
	second.Host = "127.0.0.1"
	second.Header.Set("Content-Type", "application/json")
	secondRR := httptest.NewRecorder()
	h.ServeHTTP(secondRR, second)
	if secondRR.Code != http.StatusCreated {
		t.Fatalf("second=%d %s", secondRR.Code, secondRR.Body.String())
	}
	if strings.Contains(secondRR.Body.String(), "sk-other-key") {
		t.Fatal("echoed second secret")
	}
	active := httptest.NewRequest(http.MethodPut, "/api/providers/keys/active", strings.NewReader(`{"name":"openai-apikey","id":"746b4ad1"}`))
	active.Host = "127.0.0.1"
	active.Header.Set("Content-Type", "application/json")
	activeRR := httptest.NewRecorder()
	h.ServeHTTP(activeRR, active)
	if activeRR.Code != http.StatusOK {
		t.Fatalf("active=%d %s", activeRR.Code, activeRR.Body.String())
	}
	alias := httptest.NewRequest(http.MethodPut, "/api/providers/keys/alias", strings.NewReader(`{"name":"openai-apikey","id":"746b4ad1","alias":"prod"}`))
	alias.Host = "127.0.0.1"
	alias.Header.Set("Content-Type", "application/json")
	aliasRR := httptest.NewRecorder()
	h.ServeHTTP(aliasRR, alias)
	if aliasRR.Code != http.StatusOK {
		t.Fatalf("alias=%d %s", aliasRR.Code, aliasRR.Body.String())
	}
	list := httptest.NewRequest(http.MethodGet, "/api/providers/keys?name=openai-apikey", nil)
	list.Host = "127.0.0.1"
	listRR := httptest.NewRecorder()
	h.ServeHTTP(listRR, list)
	if listRR.Code != http.StatusOK {
		t.Fatalf("list=%d %s", listRR.Code, listRR.Body.String())
	}
	body := listRR.Body.String()
	if strings.Contains(body, "sk-secret") || strings.Contains(body, "sk-other-key") {
		t.Fatalf("secret leaked: %s", body)
	}
	if !strings.Contains(body, `"746b4ad1"`) || !strings.Contains(body, `"prod"`) || !strings.Contains(body, `"activeId":"746b4ad1"`) {
		t.Fatalf("list=%s", body)
	}
}

func TestOAuthProvidersCatalogIsLoopbackAndPublic(t *testing.T) {
	h, err := NewHandler(Options{DataPlaneToken: "local-secret", Providers: map[string]Provider{"openai-apikey": providerFunc(nil)}})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	blocked := httptest.NewRequest(http.MethodGet, "/api/oauth/providers", nil)
	blocked.Header.Set("Authorization", "Bearer local-secret")
	blockedRR := httptest.NewRecorder()
	h.ServeHTTP(blockedRR, blocked)
	if blockedRR.Code != http.StatusNotFound {
		t.Fatalf("data-plane status=%d body=%s", blockedRR.Code, blockedRR.Body.String())
	}
	req := httptest.NewRequest(http.MethodGet, "/api/oauth/providers", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var body struct {
		Providers []string `json:"providers"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	want := []string{"command-code", "xai", "anthropic", "kimi", "nous", "kiro", "google-antigravity", "cursor", "github-copilot"}
	if strings.Join(body.Providers, ",") != strings.Join(want, ",") {
		t.Fatalf("providers=%v", body.Providers)
	}
	for _, id := range body.Providers {
		if id == "chatgpt" {
			t.Fatal("chatgpt is not a public OAuth login")
		}
	}
}

func TestKeyProvidersCatalogOmitsSecrets(t *testing.T) {
	h, err := NewHandler(Options{DataPlaneToken: "local-secret", Providers: map[string]Provider{"openai-apikey": providerFunc(nil)}})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodGet, "/api/key-providers", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), `"apiKey"`) || strings.Contains(rr.Body.String(), "sk-") {
		t.Fatalf("secret leaked: %s", rr.Body.String())
	}
	var body struct {
		Providers []keyLoginProvider `json:"providers"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range body.Providers {
		if p.ID == "openai-apikey" {
			found = true
			if p.DashboardURL == "" || p.Adapter == "" {
				t.Fatalf("openai-apikey incomplete: %+v", p)
			}
		}
		if p.ID == "cursor" || p.ID == "xai" {
			t.Fatalf("oauth id in key catalog: %s", p.ID)
		}
	}
	if !found {
		t.Fatal("openai-apikey missing")
	}
}
