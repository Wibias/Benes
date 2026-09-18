package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/router"
)

func TestResponsesResolvesProviderAndModelAliases(t *testing.T) {
	provider := &capturingOpenProvider{events: []protocol.Event{
		{Type: protocol.EventTextDelta, Text: "hello"},
		{Type: protocol.EventDone},
	}}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"google-antigravity": provider},
		Aliases: router.AliasTable{
			Providers:       map[string]struct{}{"google-antigravity": {}},
			ProviderByAlias: map[string]string{"agy": "google-antigravity"},
			ModelByProvider: map[string]map[string]string{
				"google-antigravity": {"opus": "claude-opus-5"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"agy/opus","store":false,"stream":true}`))
	req.Header.Set("Authorization", "Bearer local-secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if provider.opens != 1 || provider.model != "claude-opus-5" {
		t.Fatalf("opens=%d model=%q", provider.opens, provider.model)
	}
}

func TestResponsesAmbiguousBareModelAliasIsBadRequest(t *testing.T) {
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers: map[string]Provider{
			"google-antigravity": &capturingOpenProvider{},
			"openrouter":         &capturingOpenProvider{},
		},
		Aliases: router.AliasTable{
			Providers: map[string]struct{}{"google-antigravity": {}, "openrouter": {}},
			ModelByProvider: map[string]map[string]string{
				"google-antigravity": {"opus": "claude-opus-5"},
				"openrouter":         {"opus": "anthropic/claude-opus-5"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"opus","store":false}`))
	req.Header.Set("Authorization", "Bearer local-secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, "ambiguous") || !strings.Contains(body, "google-antigravity/claude-opus-5") || !strings.Contains(body, "openrouter/anthropic/claude-opus-5") {
		t.Fatalf("body=%s", body)
	}
}
