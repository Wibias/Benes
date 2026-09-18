package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wibias/Benes/internal/catalog"
)

func TestModelsAndCatalogShareTheSameProjectionBytes(t *testing.T) {
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai": &fakeProvider{}},
		CatalogModels: []catalog.Model{{
			ID:                    "openai/gpt-5.6",
			Context:               catalog.ContextWindow{Tokens: 400000, Source: catalog.ContextOperator},
			AutoCompactTokenLimit: 360000,
			Availability:          catalog.Availability{Selectable: true, Reason: "ok"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	models := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	models.Header.Set("Authorization", "Bearer local-secret")
	modelsRR := httptest.NewRecorder()
	h.ServeHTTP(modelsRR, models)
	catalogReq := httptest.NewRequest(http.MethodGet, "/v1/catalog", nil)
	catalogReq.Header.Set("Authorization", "Bearer local-secret")
	catalogRR := httptest.NewRecorder()
	h.ServeHTTP(catalogRR, catalogReq)
	if modelsRR.Code != http.StatusOK || catalogRR.Code != http.StatusOK {
		t.Fatalf("models=%d catalog=%d", modelsRR.Code, catalogRR.Code)
	}
	if modelsRR.Body.String() != catalogRR.Body.String() {
		t.Fatalf("models=%s catalog=%s", modelsRR.Body.String(), catalogRR.Body.String())
	}
}
