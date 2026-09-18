package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wibias/Benes/internal/catalog"
)

func TestDataPlaneCatalogCredentialCannotReadManagementRoutes(t *testing.T) {
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai": &fakeProvider{}},
		CatalogModels:  []catalog.Model{{ID: "openai/gpt-5.6"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	catalogReq := httptest.NewRequest(http.MethodGet, "/v1/catalog", nil)
	catalogReq.Header.Set("Authorization", "Bearer local-secret")
	catalogRR := httptest.NewRecorder()
	h.ServeHTTP(catalogRR, catalogReq)
	if catalogRR.Code != http.StatusOK {
		t.Fatalf("catalog status=%d body=%s", catalogRR.Code, catalogRR.Body.String())
	}
	for _, path := range []string{"/api/config", "/api/config/mutations", "/api/providers", "/api/providers/workspace", "/api/providers/keys", "/api/auth", "/api/client-integrations", "/api/harnesses", "/api/harnesses/settings", "/api/harnesses/reveal", "/api/credentials", "/api/stop", "/api/debug", "/api/debug/logs", "/api/providers/test", "/api/provider-presets", "/api/logs", "/api/diagnostics/requests", "/api/sessions", "/api/sessions/filters", "/api/system/memory", "/api/claude/inbound-debug", "/api/oauth/providers", "/api/key-providers", "/api/routing-profiles", "/api/routing-analytics", "/api/combos", "/api/lab/status", "/api/lab/verdicts", "/api/lab/catalog", "/api/fabric/status", "/api/fabric-settings", "/api/storage", "/api/storage/trash", "/api/storage/cleanup-policy", "/api/grok", "/api/usage", "/api/usage/retention", "/api/usage/retention/preview", "/api/usage/retention/run", "/api/oauth/accounts", "/api/oauth/accounts/import", "/api/native-main-profiles", "/api/oauth/status", "/api/codex-auth/active", "/api/codex-auth/auto-switch", "/api/codex-auth/login-status", "/api/codex-auth/reset-credits", "/api/claude-code", "/api/models", "/api/models/probe", "/api/disabled-models", "/api/client-config", "/api/keys", "/api/injection-model", "/api/effort-caps", "/api/subagent-models", "/api/subagent-model-fallback", "/api/custom-models", "/api/selected-models", "/api/model-presets", "/api/model-discovery", "/api/sidecar-settings", "/api/context-projection", "/api/shadow-call-settings", "/api/settings", "/api/update/check", "/api/update/status", "/api/update/badge", "/api/sync", "/api/native-integrations", "/api/startup-health", "/api/github/star", "/api/windows-tray", "/api/diagnostics/project-config", "/api/claude-desktop", "/api/provider-request-pacing", "/api/provider-context-caps", "/api/catalog", "/api/system/codex-app-server", "/api/system/codex-restart"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer local-secret")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("path=%s status=%d body=%s", path, rr.Code, rr.Body.String())
		}
	}
}
