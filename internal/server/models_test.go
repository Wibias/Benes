package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wibias/Benes/internal/catalog"
)

func TestModelsListsCatalogProjection(t *testing.T) {
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai": &fakeProvider{}},
		CatalogModels: []catalog.Model{{
			ID:                    "gpt-5.6",
			Context:               catalog.ContextWindow{Tokens: 400000, Source: catalog.ContextOperator},
			AutoCompactTokenLimit: 360000,
			ReasoningEfforts:      []string{"low", "medium", "high"},
			Availability:          catalog.Availability{Selectable: true},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer local-secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var body struct {
		Object string `json:"object"`
		Data   []struct {
			ID            string   `json:"id"`
			Object        string   `json:"object"`
			OwnedBy       string   `json:"owned_by"`
			ContextWindow int      `json:"context_window"`
			ContextSource string   `json:"context_source"`
			Reasoning     []string `json:"reasoning_efforts"`
			AutoCompact   int      `json:"auto_compact_token_limit"`
			Selectable    bool     `json:"selectable"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Object != "list" || len(body.Data) != 1 || body.Data[0].ID != "openai/gpt-5.6" {
		t.Fatalf("body=%s", rr.Body.String())
	}
	if body.Data[0].ContextWindow != 400000 || body.Data[0].ContextSource != string(catalog.ContextOperator) {
		t.Fatalf("context=%s", rr.Body.String())
	}
}

func TestModelsKeepsComboIdentityWithoutComboOwnership(t *testing.T) {
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai": &fakeProvider{}},
		CatalogModels: []catalog.Model{{
			ID:           "combo/fast",
			Availability: catalog.Availability{Selectable: true},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer local-secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var body struct {
		Data []struct {
			ID         string `json:"id"`
			OwnedBy    string `json:"owned_by"`
			Selectable bool   `json:"selectable"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Data) != 1 || body.Data[0].ID != "combo/fast" || !body.Data[0].Selectable {
		t.Fatalf("row=%s", rr.Body.String())
	}
	if body.Data[0].OwnedBy == "combo" || body.Data[0].OwnedBy != "openai" {
		t.Fatalf("owned_by=%q body=%s", body.Data[0].OwnedBy, rr.Body.String())
	}
}

func TestModelsRequiresDataPlaneAdmission(t *testing.T) {
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai": &fakeProvider{}},
		CatalogModels:  []catalog.Model{{ID: "gpt-5.6"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}
