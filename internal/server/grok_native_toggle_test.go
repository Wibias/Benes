package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGrokNativeToggleDisableOmitsSecrets(t *testing.T) {
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodPut, "/api/native-integrations/grok", strings.NewReader(`{"enabled":false}`))
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || strings.Contains(rr.Body.String(), "local-secret") {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}
