package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
)

func TestResponsesAcceptsAnyConfiguredDataPlaneToken(t *testing.T) {
	provider := &fakeProvider{events: []protocol.Event{{Type: protocol.EventDone}}}
	h, err := NewHandler(Options{
		DataPlaneTokens: []string{"first-secret", "second-secret"},
		Providers:       map[string]Provider{"provider": provider},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	for _, token := range []string{"first-secret", "second-secret"} {
		req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"provider/model","store":false}`))
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code == http.StatusUnauthorized {
			t.Fatalf("token %q was rejected", token)
		}
	}
}

func TestNewHandlerHashesAndDeduplicatesDataPlaneTokens(t *testing.T) {
	h, err := NewHandler(Options{
		DataPlaneTokens: []string{"first-secret", "second-secret", "first-secret"},
		Providers:       map[string]Provider{"provider": &fakeProvider{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	concrete := h.(*handler)
	if len(concrete.dataPlaneTokenHashes) != 2 {
		t.Fatalf("hash count=%d", len(concrete.dataPlaneTokenHashes))
	}
	for _, hash := range concrete.dataPlaneTokenHashes {
		if bytes.Contains(hash[:], []byte("secret")) {
			t.Fatal("raw credential material appears in stored digest")
		}
	}
}

func TestNewHandlerRejectsBlankDataPlaneToken(t *testing.T) {
	for _, tokens := range [][]string{nil, {}, {""}, {"ok", " "}, {"ok", "\t"}} {
		if _, err := NewHandler(Options{DataPlaneTokens: tokens, Providers: map[string]Provider{"provider": &fakeProvider{}}}); err == nil {
			t.Fatalf("accepted tokens %#v", tokens)
		}
	}
}

func TestNewHandlerRejectsAmbiguousLegacyAndMultiKeyAuth(t *testing.T) {
	_, err := NewHandler(Options{
		DataPlaneToken:  "legacy",
		DataPlaneTokens: []string{"new"},
		Providers:       map[string]Provider{"provider": &fakeProvider{}},
	})
	if err == nil || !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("err=%v", err)
	}
}
