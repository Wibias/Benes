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
	"github.com/Wibias/Benes/internal/grok"
)

// The loopback Grok API publishes a derived projection summary and one apply action. It
// publishes no per-Grok model list, because the Benes catalogue is the only place model
// membership is decided.
func TestGrokAPIStatusReportsTheCatalogueProjection(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GROK_HOME", home)
	models := []grok.Model{{ID: "openai/gpt-5.6-sol"}, {ID: "xai/grok-4.6"}}
	if !grok.Inject(23100, models, grok.InjectOptions{GrokHome: home, Hostname: "127.0.0.1"}).OK {
		t.Fatal("inject")
	}
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		CatalogModels: []catalog.Model{
			{ID: "openai/gpt-5.6-sol"},
			{ID: "xai/grok-4.6"},
		},
		ConfigPath: configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)

	blocked := httptest.NewRequest(http.MethodGet, "/api/grok", nil)
	blocked.Header.Set("Authorization", "Bearer local-secret")
	blockedRR := httptest.NewRecorder()
	h.ServeHTTP(blockedRR, blocked)
	if blockedRR.Code != http.StatusNotFound {
		t.Fatalf("data-plane status=%d body=%s", blockedRR.Code, blockedRR.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/api/grok", nil)
	req.Host = "127.0.0.1:23100"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), grokLoopbackKey()) || strings.Contains(rr.Body.String(), "api_key") {
		t.Fatalf("secret leaked: %s", rr.Body.String())
	}
	var got struct {
		Present    bool `json:"present"`
		Catalogue  int  `json:"catalogue"`
		Registered int  `json:"registered"`
		Current    bool `json:"current"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Present || !got.Current || got.Catalogue != 2 || got.Registered != 2 {
		t.Fatalf("got=%+v body=%s", got, rr.Body.String())
	}
	// No second model authority may appear on this route.
	for _, forbidden := range []string{"candidates", "excluded", "grokExcludedModels"} {
		if strings.Contains(rr.Body.String(), forbidden) {
			t.Fatalf("%s is still published: %s", forbidden, rr.Body.String())
		}
	}
}

func TestGrokSelectionRouteIsGone(t *testing.T) {
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodPut, "/api/grok/selection", strings.NewReader(`{"excluded":["a"]}`))
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestGrokApplyWritesTheCatalogueAndKeepsUserConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GROK_HOME", home)
	configPath := filepath.Join(home, "config.toml")
	userOwned := "[model.user-owned]\nmodel = \"mine/model\"\n\n[ui]\ntheme = \"dark\"\n"
	if err := os.WriteFile(configPath, []byte(userOwned), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		CatalogModels: []catalog.Model{
			{ID: "openai/gpt-5.6-sol"},
			{ID: "xai/grok-4.6"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodPost, "/api/grok/apply", nil)
	req.Host = "127.0.0.1:23100"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	body, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, id := range []string{"openai/gpt-5.6-sol", "xai/grok-4.6"} {
		if !strings.Contains(text, "model = \""+id+"\"") {
			t.Fatalf("catalogue model %s missing: %s", id, text)
		}
	}
	if !strings.Contains(text, "model = \"mine/model\"") || !strings.Contains(text, "[ui]") {
		t.Fatalf("user-owned config was not preserved: %s", text)
	}
	status := grok.ReadStatus(grok.InjectOptions{GrokHome: home})
	if !status.Present || status.BaseURL != "http://127.0.0.1:23100/v1" {
		t.Fatalf("status=%#v", status)
	}
}

func grokLoopbackKey() string {
	return "benes-loopback"
}

