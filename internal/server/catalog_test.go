package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wibias/Benes/internal/catalog"
)

func TestCatalogIsReadableWithDataPlaneCredential(t *testing.T) {
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai": &fakeProvider{}},
		CatalogModels: []catalog.Model{{
			ID:                    "openai/gpt-5.6",
			Context:               catalog.ContextWindow{Tokens: 400000, Source: catalog.ContextOperator},
			AutoCompactTokenLimit: 360000,
			Availability:          catalog.Availability{Selectable: true},
		}},
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
	if rr.Header().Get("ETag") == "" {
		t.Fatal("missing etag")
	}
	var body struct {
		Object string `json:"object"`
		Data   []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Object != "list" || len(body.Data) != 1 || body.Data[0].ID != "openai/gpt-5.6" {
		t.Fatalf("body=%s", rr.Body.String())
	}
}

func TestCatalogHEADUsesTheSameETagWithoutABody(t *testing.T) {
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai": &fakeProvider{}},
		CatalogModels:  []catalog.Model{{ID: "openai/gpt-5.6"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	get := httptest.NewRequest(http.MethodGet, "/v1/catalog", nil)
	get.Header.Set("Authorization", "Bearer local-secret")
	getRR := httptest.NewRecorder()
	h.ServeHTTP(getRR, get)
	head := httptest.NewRequest(http.MethodHead, "/v1/catalog", nil)
	head.Header.Set("Authorization", "Bearer local-secret")
	headRR := httptest.NewRecorder()
	h.ServeHTTP(headRR, head)
	if headRR.Code != http.StatusOK || headRR.Body.Len() != 0 {
		t.Fatalf("head status=%d body=%q", headRR.Code, headRR.Body.String())
	}
	if headRR.Header().Get("ETag") == "" || headRR.Header().Get("ETag") != getRR.Header().Get("ETag") {
		t.Fatalf("etag get=%q head=%q", getRR.Header().Get("ETag"), headRR.Header().Get("ETag"))
	}
}

func TestCatalogIfNoneMatchReturnsNotModified(t *testing.T) {
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai": &fakeProvider{}},
		CatalogModels:  []catalog.Model{{ID: "openai/gpt-5.6"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	first := httptest.NewRequest(http.MethodGet, "/v1/catalog", nil)
	first.Header.Set("Authorization", "Bearer local-secret")
	firstRR := httptest.NewRecorder()
	h.ServeHTTP(firstRR, first)
	etag := firstRR.Header().Get("ETag")
	if etag == "" {
		t.Fatal("missing etag")
	}
	again := httptest.NewRequest(http.MethodGet, "/v1/catalog", nil)
	again.Header.Set("Authorization", "Bearer local-secret")
	again.Header.Set("If-None-Match", etag)
	againRR := httptest.NewRecorder()
	h.ServeHTTP(againRR, again)
	if againRR.Code != http.StatusNotModified || againRR.Body.Len() != 0 {
		t.Fatalf("status=%d body=%q", againRR.Code, againRR.Body.String())
	}
	if againRR.Header().Get("ETag") != etag {
		t.Fatalf("etag=%q", againRR.Header().Get("ETag"))
	}
}

func TestCatalogRejectsMutationMethodsAndMissingAuth(t *testing.T) {
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai": &fakeProvider{}},
		CatalogModels:  []catalog.Model{{ID: "openai/gpt-5.6"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	missing := httptest.NewRequest(http.MethodGet, "/v1/catalog", nil)
	missRR := httptest.NewRecorder()
	h.ServeHTTP(missRR, missing)
	if missRR.Code != http.StatusUnauthorized {
		t.Fatalf("missing auth status=%d", missRR.Code)
	}
	post := httptest.NewRequest(http.MethodPost, "/v1/catalog", nil)
	post.Header.Set("Authorization", "Bearer local-secret")
	postRR := httptest.NewRecorder()
	h.ServeHTTP(postRR, post)
	if postRR.Code != http.StatusNotFound {
		t.Fatalf("post status=%d body=%s", postRR.Code, postRR.Body.String())
	}
}
