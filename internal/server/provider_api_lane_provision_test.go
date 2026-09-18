package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/credentials"
)

func TestOpenAIFirstAPIKeyProvisionsHiddenAPILane(t *testing.T) {
	store, err := credentials.NewFileStore(filepath.Join(t.TempDir(), "creds"), false)
	if err != nil {
		t.Fatal(err)
	}
	h, _ := newProvidersMutateHandlerWithCreds(t, `{
		"defaultProvider": "openai",
		"providers": {
			"openai": {
				"adapter": "openai-responses",
				"baseUrl": "https://chatgpt.com/backend-api/codex",
				"authMode": "forward",
				"codexAccountMode": "pool"
			}
		}
	}`, store, config.LogicalOpenAIID)

	req := loopbackJSON(http.MethodPost, "/api/providers/keys", `{
		"name": "openai-apikey",
		"key": "sk-first-secret",
		"label": "Primary"
	}`)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "sk-first-secret") {
		t.Fatalf("secret echoed: %s", rr.Body.String())
	}

	got := getConfig(t, h)
	api, ok := got.Providers[config.OpenAIAPIConnection]
	if !ok {
		t.Fatalf("API lane missing: %#v", got.Providers)
	}
	if api.Adapter != "openai-responses" || api.BaseURL != "https://api.openai.com/v1" || !api.HasAPIKey {
		t.Fatalf("API lane=%#v", api)
	}

	workspaceReq := httptest.NewRequest(http.MethodGet, "/api/providers/workspace", nil)
	workspaceReq.Host = "127.0.0.1"
	workspaceRR := httptest.NewRecorder()
	h.ServeHTTP(workspaceRR, workspaceReq)
	if workspaceRR.Code != http.StatusOK {
		t.Fatalf("workspace status=%d body=%s", workspaceRR.Code, workspaceRR.Body.String())
	}
	var workspace struct {
		Summary struct {
			TotalProviders int `json:"totalProviders"`
		} `json:"summary"`
		Providers []struct {
			ID     string   `json:"id"`
			Hidden []string `json:"hidden"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(workspaceRR.Body.Bytes(), &workspace); err != nil {
		t.Fatal(err)
	}
	if workspace.Summary.TotalProviders != 1 || len(workspace.Providers) != 1 || workspace.Providers[0].ID != config.LogicalOpenAIID {
		t.Fatalf("workspace=%s", workspaceRR.Body.String())
	}
	if len(workspace.Providers[0].Hidden) != 1 || workspace.Providers[0].Hidden[0] != config.OpenAIAPIConnection {
		t.Fatalf("hidden=%#v", workspace.Providers[0].Hidden)
	}
}
