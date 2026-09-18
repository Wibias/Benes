package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDashboardServesIndexAndLeavesDataPlaneJSON404(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<!doctype html>benes-dashboard"), 0o644); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai": &fakeProvider{}},
		DashboardDir:   dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	index := httptest.NewRequest(http.MethodGet, "/", nil)
	indexRR := httptest.NewRecorder()
	h.ServeHTTP(indexRR, index)
	if indexRR.Code != http.StatusOK || !strings.Contains(indexRR.Body.String(), "benes-dashboard") {
		t.Fatalf("index status=%d body=%s", indexRR.Code, indexRR.Body.String())
	}
	unknown := httptest.NewRequest(http.MethodGet, "/v1/nope", nil)
	unknown.Header.Set("Authorization", "Bearer local-secret")
	unknownRR := httptest.NewRecorder()
	h.ServeHTTP(unknownRR, unknown)
	if unknownRR.Code != http.StatusNotFound || !strings.Contains(unknownRR.Body.String(), `"code":"not_found"`) {
		t.Fatalf("v1 status=%d body=%s", unknownRR.Code, unknownRR.Body.String())
	}
	api := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	apiRR := httptest.NewRecorder()
	h.ServeHTTP(apiRR, api)
	if apiRR.Code != http.StatusNotFound {
		t.Fatalf("api status=%d body=%s", apiRR.Code, apiRR.Body.String())
	}
}
