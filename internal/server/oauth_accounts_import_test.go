package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/providers/antigravity"
)

type importRT func(*http.Request) (*http.Response, error)

func (f importRT) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestOAuthAccountsImportAdmitsProviderFormatAndPersists(t *testing.T) {
	prev := antigravity.HTTPClient
	t.Cleanup(func() { antigravity.HTTPClient = prev })
	antigravity.HTTPClient = &http.Client{Transport: importRT(func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.Contains(req.URL.Host, "oauth2.googleapis.com"):
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"access_token":"ya29-access","expires_in":3600}`)), Header: make(http.Header)}, nil
		case strings.Contains(req.URL.Path, "userinfo"):
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"email":"user@example.com","id":"gid-1"}`)), Header: make(http.Header)}, nil
		default:
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"projectId":"proj-1"}`)), Header: make(http.Header)}, nil
		}
	})}
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"listen":"127.0.0.1:0"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai": providerFunc(nil)},
		ConfigPath:     configPath,
		AuthStorePath:  filepath.Join(home, "auth.json"),
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	blocked := httptest.NewRequest(http.MethodPost, "/api/oauth/accounts/import", strings.NewReader(`{"provider":"google-antigravity","format":"cockpit-tools","document":[]}`))
	blocked.Header.Set("Authorization", "Bearer local-secret")
	blockedRR := httptest.NewRecorder()
	h.ServeHTTP(blockedRR, blocked)
	if blockedRR.Code != http.StatusNotFound {
		t.Fatalf("catalog token should not import: %d", blockedRR.Code)
	}

	unsupported := httptest.NewRequest(http.MethodPost, "/api/oauth/accounts/import", strings.NewReader(`{"provider":"openai","format":"cockpit-tools","document":[]}`))
	unsupported.Host = "127.0.0.1"
	uRR := httptest.NewRecorder()
	h.ServeHTTP(uRR, unsupported)
	if uRR.Code != 400 || !strings.Contains(uRR.Body.String(), "unsupported_provider") {
		t.Fatalf("unsupported=%s", uRR.Body.String())
	}

	payload := `{"provider":"google-antigravity","format":"cockpit-tools","document":[{"email":"user@example.com","refresh_token":"rt-safe"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/oauth/accounts/import", strings.NewReader(payload))
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var result map[string]any
	if json.Unmarshal(rr.Body.Bytes(), &result) != nil || result["importedCount"] != 1.0 {
		t.Fatalf("result=%s", rr.Body.String())
	}
	raw, _ := os.ReadFile(filepath.Join(home, "auth.json"))
	if !strings.Contains(string(raw), "user@example.com") || strings.Contains(rr.Body.String(), "rt-safe") {
		t.Fatalf("persist=%s response=%s", raw, rr.Body.String())
	}
}
