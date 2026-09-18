package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wibias/Benes/internal/catalog"
)

func TestCatalogProjectsDeterministicGeneratorErrorsWithoutSecrets(t *testing.T) {
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai": &fakeProvider{}},
		CatalogModels:  []catalog.Model{{ID: "openai/gpt-5.6"}},
		CatalogErrors:  []CatalogError{{ID: "future", Code: "unsupported_adapter"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodGet, "/v1/catalog", nil)
	req.Header.Set("Authorization", "Bearer local-secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
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
	if len(body.Errors) != 1 || body.Errors[0].ID != "future" || body.Errors[0].Code != "unsupported_adapter" {
		t.Fatalf("body=%s", rr.Body.String())
	}
}
