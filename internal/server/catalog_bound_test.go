package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wibias/Benes/internal/catalog"
)

func TestCatalogRejectsAnOversizedProjection(t *testing.T) {
	h, err := NewHandler(Options{
		DataPlaneToken:  "local-secret",
		Providers:       map[string]Provider{"openai": &fakeProvider{}},
		CatalogModels:   []catalog.Model{{ID: "openai/gpt-5.6"}},
		CatalogMaxBytes: 16,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodGet, "/v1/catalog", nil)
	req.Header.Set("Authorization", "Bearer local-secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}
