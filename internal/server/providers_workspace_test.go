package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/catalog"
	"github.com/Wibias/Benes/internal/credentials"
)

func TestProvidersWorkspaceFoldsOpenAIAndMatchesLifecycle(t *testing.T) {
	h, _ := newProvidersMutateHandler(t, `{
		"defaultProvider": "openai",
		"accountPoolStrategy": "quota",
		"autoSwitchThreshold": 80,
		"providers": {
			"openai": {
				"adapter": "openai-responses",
				"baseUrl": "https://chatgpt.com/backend-api/codex",
				"authMode": "forward",
				"codexAccountMode": "pool"
			},
			"openai-apikey": {
				"adapter": "openai-chat",
				"baseUrl": "https://api.openai.com/v1",
				"credentialRef": {"id": "k", "source": "secure-store"}
			},
			"anthropic": {
				"adapter": "anthropic",
				"baseUrl": "https://api.anthropic.com",
				"authMode": "oauth"
			},
			"local": {
				"adapter": "local",
				"baseUrl": "http://127.0.0.1:11434",
				"authMode": "local"
			}
		}
	}`, "openai", "openai-apikey", "anthropic", "local")

	inner, ok := h.(*handler)
	if !ok {
		t.Fatal("handler type")
	}
	inner.catalogModels = []catalog.Model{
		{ID: "openai/gpt-5"},
		{ID: "openai-apikey/gpt-4o"},
		{ID: "anthropic/claude-sonnet-4"},
	}

	blocked := httptest.NewRequest(http.MethodGet, "/api/providers/workspace", nil)
	blocked.Header.Set("Authorization", "Bearer local-secret")
	blockedRR := httptest.NewRecorder()
	h.ServeHTTP(blockedRR, blocked)
	if blockedRR.Code != http.StatusNotFound {
		t.Fatalf("data-plane status=%d body=%s", blockedRR.Code, blockedRR.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/api/providers/workspace", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "sk-") {
		t.Fatalf("secret leaked: %s", rr.Body.String())
	}

	var body struct {
		Summary struct {
			TotalProviders int `json:"totalProviders"`
			Healthy        int `json:"healthy"`
			Attention      int `json:"attention"`
			Disabled       int `json:"disabled"`
			ExposedModels  int `json:"exposedModels"`
		} `json:"summary"`
		Providers []struct {
			ID         string   `json:"id"`
			Lifecycle  string   `json:"lifecycle"`
			ModelCount int      `json:"modelCount"`
			Hidden     []string `json:"hidden"`
			Access     struct {
				Methods []struct {
					Kind string `json:"kind"`
				} `json:"methods"`
				DefaultAccess bool `json:"defaultAccess"`
				Selection     *struct {
					ShowAutoSwitch  bool `json:"showAutoSwitch"`
					ShowStickyLimit bool `json:"showStickyLimit"`
				} `json:"selection"`
			} `json:"access"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Summary.TotalProviders != 3 || body.Summary.Healthy+body.Summary.Attention+body.Summary.Disabled != 3 {
		t.Fatalf("summary=%#v", body.Summary)
	}
	if len(body.Providers) != 3 {
		t.Fatalf("providers=%#v", body.Providers)
	}
	openai := body.Providers[0]
	if openai.ID != "openai" {
		t.Fatalf("first=%s", openai.ID)
	}
	if len(openai.Hidden) != 1 || openai.Hidden[0] != "openai-apikey" {
		t.Fatalf("hidden=%#v", openai.Hidden)
	}
	if openai.ModelCount != 2 {
		t.Fatalf("openai models=%d", openai.ModelCount)
	}
	if len(openai.Access.Methods) != 2 || openai.Access.Methods[0].Kind != "oauth" || openai.Access.Methods[1].Kind != "api-key" {
		t.Fatalf("methods=%#v", openai.Access.Methods)
	}
	if !openai.Access.DefaultAccess || openai.Access.Selection == nil || !openai.Access.Selection.ShowAutoSwitch {
		t.Fatalf("access=%#v", openai.Access)
	}
	ids := []string{body.Providers[0].ID, body.Providers[1].ID, body.Providers[2].ID}
	if strings.Join(ids, ",") != "openai,anthropic,local" && strings.Join(ids, ",") != "openai,local,anthropic" {
		if body.Providers[1].ID == "openai-apikey" || body.Providers[2].ID == "openai-apikey" {
			t.Fatalf("openai-apikey leaked into rail: %v", ids)
		}
	}
	for _, row := range body.Providers {
		if row.ID == "openai-apikey" {
			t.Fatal("openai-apikey visible")
		}
		if row.ID == "local" && len(row.Access.Methods) != 0 {
			t.Fatalf("local methods=%#v", row.Access.Methods)
		}
		if row.ID == "anthropic" && (len(row.Access.Methods) != 1 || row.Access.Methods[0].Kind != "oauth") {
			t.Fatalf("anthropic access=%#v", row.Access)
		}
	}
}

func TestProvidersPATCHDefaultAccessPersists(t *testing.T) {
	h, configPath := newProvidersMutateHandler(t, `{
		"defaultProvider": "openai",
		"providers": {
			"openai": {
				"adapter": "openai-responses",
				"baseUrl": "https://chatgpt.com/backend-api/codex",
				"authMode": "forward"
			}
		}
	}`, "openai")

	bad := loopbackJSON(http.MethodPatch, "/api/providers?name=openai", `{"defaultAccess":"keys"}`)
	badRR := httptest.NewRecorder()
	h.ServeHTTP(badRR, bad)
	if badRR.Code != http.StatusBadRequest {
		t.Fatalf("invalid status=%d body=%s", badRR.Code, badRR.Body.String())
	}

	ok := loopbackJSON(http.MethodPatch, "/api/providers?name=openai", `{"defaultAccess":"api"}`)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, ok)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"defaultAccess": "api"`) && !strings.Contains(string(raw), `"defaultAccess":"api"`) {
		t.Fatalf("disk=%s", raw)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/providers/workspace", nil)
	req.Host = "127.0.0.1"
	ws := httptest.NewRecorder()
	h.ServeHTTP(ws, req)
	if !strings.Contains(ws.Body.String(), `"defaultMethodId":"api"`) && !strings.Contains(ws.Body.String(), `"defaultMethodId": "api"`) {
		t.Fatalf("workspace=%s", ws.Body.String())
	}
	if !strings.Contains(ws.Body.String(), "default_access_changed") {
		t.Fatalf("activity missing: %s", ws.Body.String())
	}
}

func TestProvidersWorkspaceOmitsCredentialMaterial(t *testing.T) {
	store, err := credentials.NewFileStore(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Put("k", []byte("sk-secret-value")); err != nil {
		t.Fatal(err)
	}
	h, _ := newProvidersMutateHandlerWithCreds(t, `{
		"providers": {
			"openai-apikey": {
				"adapter": "openai-chat",
				"baseUrl": "https://api.openai.com/v1",
				"apiKey": "sk-secret-value",
				"credentialRef": {"id": "k", "source": "secure-store"}
			}
		}
	}`, store, "openai-apikey")
	req := httptest.NewRequest(http.MethodGet, "/api/providers/workspace", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "sk-secret") {
		t.Fatalf("secret leaked: %s", rr.Body.String())
	}
}

func TestProvidersWorkspaceEncodesEmptyHiddenAsArray(t *testing.T) {
	h, _ := newProvidersMutateHandler(t, `{
		"defaultProvider": "openai",
		"providers": {
			"openai": {
				"adapter": "openai-responses",
				"baseUrl": "https://chatgpt.com/backend-api/codex",
				"authMode": "forward"
			}
		}
	}`, "openai")
	req := httptest.NewRequest(http.MethodGet, "/api/providers/workspace", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	raw := rr.Body.String()
	if strings.Contains(raw, `"hidden":null`) {
		t.Fatalf("nil hidden encodes as JSON null: %s", raw)
	}
	if !strings.Contains(raw, `"hidden":[]`) && !strings.Contains(raw, `"hidden": []`) {
		t.Fatalf("empty hidden missing: %s", raw)
	}
	if strings.Contains(raw, `"recentEvents":null`) {
		t.Fatalf("nil recentEvents encodes as JSON null: %s", raw)
	}
}
