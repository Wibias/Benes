package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/codexfeatures"
)

func TestV2APIReportsEnabledWithoutSecrets(t *testing.T) {
	codexHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(codexHome, "config.toml"), []byte("[features]\nmulti_agent_v2 = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte(`{"multiAgentMode":"v2","keepNativeChatGptOnV1":true,"apiKeys":[{"key":"sk-secret"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		ConfigPath:     configPath,
		CodexHome:      codexHome,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	blocked := httptest.NewRequest(http.MethodGet, "/api/v2", nil)
	blocked.Header.Set("Authorization", "Bearer local-secret")
	blockedRR := httptest.NewRecorder()
	h.ServeHTTP(blockedRR, blocked)
	if blockedRR.Code != http.StatusNotFound {
		t.Fatalf("data-plane status=%d", blockedRR.Code)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v2", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	body := rr.Body.String()
	if rr.Code != http.StatusOK || !strings.Contains(body, `"enabled":true`) || !strings.Contains(body, `"multiAgentMode":"v2"`) || strings.Contains(body, "sk-secret") {
		t.Fatalf("status=%d body=%s", rr.Code, body)
	}
}

func TestV2APIPutTogglesWithoutSecrets(t *testing.T) {
	codexHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(codexHome, "config.toml"), []byte("[features]\nmulti_agent_v2 = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte(`{"apiKeys":[{"key":"sk-secret"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		ConfigPath:     configPath,
		CodexHome:      codexHome,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	orig := codexfeatures.Run
	codexfeatures.Run = func(action string) error {
		enabled := "false"
		if action == "enable" {
			enabled = "true"
		}
		return os.WriteFile(filepath.Join(codexHome, "config.toml"), []byte("[features]\nmulti_agent_v2 = "+enabled+"\n"), 0o600)
	}
	t.Cleanup(func() { codexfeatures.Run = orig })
	req := httptest.NewRequest(http.MethodPut, "/api/v2", strings.NewReader(`{"enabled":true,"multiAgentMode":"v1","keepNativeChatGptOnV1":true}`))
	req.Host = "127.0.0.1"
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	body := rr.Body.String()
	if rr.Code != http.StatusOK || !strings.Contains(body, `"enabled":true`) || !strings.Contains(body, `"multiAgentMode":"v1"`) || strings.Contains(body, "sk-secret") {
		t.Fatalf("status=%d body=%s", rr.Code, body)
	}
}
