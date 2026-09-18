package bootstrap

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/config"
)

func TestBuildDataPlaneCatalogIncludesSkippedProviderCodes(t *testing.T) {
	upstream := newResponsesUpstream(t)
	defer upstream.Close()
	plane, err := BuildDataPlane(t.Context(), config.DiskConfig{
		Raw: json.RawMessage(`{"hostname":"127.0.0.1","port":23100}`),
		Providers: map[string]json.RawMessage{
			"local":  localResponsesProvider(upstream),
			"future": json.RawMessage(`{"adapter":"future-wire","baseUrl":"https://example.com","apiKey":"ignored"}`),
		},
	}, DataPlaneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/catalog", nil)
	req.Host = "127.0.0.1:23100"
	rr := httptest.NewRecorder()
	plane.Handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !json.Valid(rr.Body.Bytes()) {
		t.Fatalf("invalid json %s", rr.Body.String())
	}
	var body struct {
		Errors []struct {
			ID   string `json:"id"`
			Code string `json:"code"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Errors) != 1 || body.Errors[0].ID != "future" || body.Errors[0].Code == "" {
		t.Fatalf("errors=%#v body=%s", body.Errors, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "ignored") || strings.Contains(rr.Body.String(), "apiKey") {
		t.Fatalf("leaked secret material: %s", rr.Body.String())
	}
}
