package bootstrap

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/config"
)

func TestBuildDataPlaneListsSynthesizedComboOnTheModelsRoute(t *testing.T) {
	primary := newResponsesUpstream(t)
	defer primary.Close()
	fallback := newResponsesUpstream(t)
	defer fallback.Close()
	plane, err := BuildDataPlane(t.Context(), config.DiskConfig{
		Raw: json.RawMessage(`{"hostname":"127.0.0.1","port":23100,"combos":{"fast":{"targets":[{"provider":"primary","model":"gpt-a"},{"provider":"fallback","model":"gpt-b"}]}}}`),
		Providers: map[string]json.RawMessage{
			"primary":  json.RawMessage(mustProviderJSON(primary, []string{"gpt-a"})),
			"fallback": json.RawMessage(mustProviderJSON(fallback, []string{"gpt-b"})),
		},
	}, DataPlaneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Host = "127.0.0.1:23100"
	rr := httptest.NewRecorder()
	plane.Handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, `"id":"combo/fast"`) {
		t.Fatalf("missing combo/fast: %s", body)
	}
	if strings.Contains(body, `"owned_by":"combo"`) {
		t.Fatalf("combo ownership leaked: %s", body)
	}
	if !strings.Contains(body, `"owned_by":"openai"`) {
		t.Fatalf("missing compatibility owner: %s", body)
	}
}

func mustProviderJSON(upstream *httptest.Server, models []string) string {
	raw, _ := json.Marshal(map[string]any{
		"adapter":             "openai-responses",
		"baseUrl":             upstream.URL,
		"apiKey":              "provider-key",
		"allowPrivateNetwork": true,
		"models":              models,
	})
	return string(raw)
}
