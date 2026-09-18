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

func loopbackRequest(method, path, body string) *http.Request {
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	req.Host = "127.0.0.1"
	return req
}

func TestCredentialsAPIListsRefsWithoutSecrets(t *testing.T) {
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
	blocked := httptest.NewRequest(http.MethodGet, "/api/credentials", nil)
	blocked.Header.Set("Authorization", "Bearer local-secret")
	blockedRR := httptest.NewRecorder()
	h.ServeHTTP(blockedRR, blocked)
	if blockedRR.Code != http.StatusNotFound {
		t.Fatalf("data-plane status=%d body=%s", blockedRR.Code, blockedRR.Body.String())
	}
	req := loopbackRequest(http.MethodGet, "/api/credentials", "")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "sk-secret") {
		t.Fatalf("secret leaked: %s", rr.Body.String())
	}
	var body struct {
		Credentials []struct {
			ID        string `json:"id"`
			Source    string `json:"source"`
			Available bool   `json:"available"`
		} `json:"credentials"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Credentials) != 1 || body.Credentials[0].ID != "openai-apikey" || body.Credentials[0].Source != "secure-store" || !body.Credentials[0].Available {
		t.Fatalf("body=%#v", body)
	}
}

func TestCredentialsExportRefusesSecrets(t *testing.T) {
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
	req := loopbackRequest(http.MethodGet, "/api/credentials/export", "")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "sk-secret") {
		t.Fatalf("secret leaked: %s", rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "secrets_export_disabled") {
		t.Fatalf("body=%s", rr.Body.String())
	}
}

func TestCredentialsHistoryAndBackupRefuseSecrets(t *testing.T) {
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
	for _, path := range []string{"/api/credentials/history", "/api/credentials/backup"} {
		req := loopbackRequest(http.MethodGet, path, "")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Fatalf("%s status=%d body=%s", path, rr.Code, rr.Body.String())
		}
		if strings.Contains(rr.Body.String(), "sk-secret") {
			t.Fatalf("%s leaked: %s", path, rr.Body.String())
		}
		if !strings.Contains(rr.Body.String(), "secrets_export_disabled") {
			t.Fatalf("%s body=%s", path, rr.Body.String())
		}
	}
}

func TestCredentialsPOSTStoresSecretWithoutEcho(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "creds")
	store, err := credentials.NewFileStore(dir, false)
	if err != nil {
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
	req := loopbackRequest(http.MethodPost, "/api/credentials", `{"id":"openai-apikey","secret":"sk-secret"}`)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "sk-secret") {
		t.Fatalf("echoed secret: %s", rr.Body.String())
	}
	got, err := store.Get(credentials.Ref{ID: "openai-apikey", Source: credentials.SourceSecureStore})
	if err != nil || string(got) != "sk-secret" {
		t.Fatalf("stored=%q %v", got, err)
	}
}

func TestCredentialsPOSTPublishesRefAndScrubsConfig(t *testing.T) {
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
	req := loopbackRequest(http.MethodPost, "/api/credentials", `{"id":"openai-apikey","secret":"sk-secret"}`)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	committed, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(committed)
	if strings.Contains(got, "sk-secret") || strings.Contains(got, "sk-old") {
		t.Fatalf("plaintext remained: %s", got)
	}
	if !strings.Contains(got, `"credentialRef"`) {
		t.Fatalf("ref missing: %s", got)
	}
}
