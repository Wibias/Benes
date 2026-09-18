package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProviderPresetsAreLoopbackAndSecretFree(t *testing.T) {
	h, err := NewHandler(Options{DataPlaneToken: "local-secret", Providers: map[string]Provider{"p": providerFunc(nil)}})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	blocked := httptest.NewRequest(http.MethodGet, "/api/provider-presets", nil)
	blocked.Header.Set("Authorization", "Bearer local-secret")
	blockedRR := httptest.NewRecorder()
	h.ServeHTTP(blockedRR, blocked)
	if blockedRR.Code != http.StatusNotFound {
		t.Fatalf("data-plane status=%d body=%s", blockedRR.Code, blockedRR.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/api/provider-presets", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), `"apiKey"`) || strings.Contains(rr.Body.String(), "local-secret") {
		t.Fatalf("secret leaked: %s", rr.Body.String())
	}
	var envelope struct {
		Providers []struct {
			ID string `json:"id"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Providers) == 0 {
		t.Fatal("empty presets")
	}
	found := false
	for _, row := range envelope.Providers {
		if row.ID == "openai" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("missing openai preset: %s", rr.Body.String()[:200])
	}
}
