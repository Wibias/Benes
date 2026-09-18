package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResponsesPreflightAllowsConfiguredOriginWithoutCredentialAndEchoesRequestedHeaders(t *testing.T) {
	h, err := NewHandler(Options{
		AdmissionPolicy: &DataPlaneAdmissionPolicy{
			BindHostname:      "0.0.0.0",
			DataPlaneTokens:   []string{"secret"},
			CORSAllowOrigins:  []string{"https://dashboard.example"},
			CORSDefaultOrigin: "http://localhost:20200",
		},
		Providers: map[string]Provider{"p": admissionTestProvider()},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)

	req := httptest.NewRequest(http.MethodOptions, "/v1/responses", nil)
	req.Host = "gateway.example:20200"
	req.Header.Set("Origin", "https://dashboard.example/path")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "content-type, X-Stainless-Lang, x-stainless-lang, X-Custom")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "https://dashboard.example/path" {
		t.Fatalf("allow origin=%q", got)
	}
	headers := strings.ToLower(rr.Header().Get("Access-Control-Allow-Headers"))
	for _, want := range []string{"x-stainless-lang", "x-custom"} {
		if !strings.Contains(headers, want) {
			t.Fatalf("allow headers=%q missing %q", headers, want)
		}
	}
	if strings.Count(headers, "x-stainless-lang") != 1 {
		t.Fatalf("duplicate requested header echo: %q", headers)
	}
	if rr.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatalf("X-Frame-Options=%q", rr.Header().Get("X-Frame-Options"))
	}
	if rr.Header().Get("Content-Security-Policy") != "frame-ancestors 'none'" {
		t.Fatalf("Content-Security-Policy=%q", rr.Header().Get("Content-Security-Policy"))
	}
}

func TestResponsesPreflightRejectsForeignOriginWithoutDynamicHeaderEcho(t *testing.T) {
	h, err := NewHandler(Options{
		AdmissionPolicy: &DataPlaneAdmissionPolicy{
			BindHostname:      "127.0.0.1",
			CORSDefaultOrigin: "http://localhost:20200",
		},
		Providers: map[string]Provider{"p": admissionTestProvider()},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)

	req := httptest.NewRequest(http.MethodOptions, "/v1/responses", nil)
	req.Host = "127.0.0.1:20200"
	req.Header.Set("Origin", "https://attacker.example")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "x-stainless-lang")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:20200" {
		t.Fatalf("fallback origin=%q", got)
	}
	if strings.Contains(strings.ToLower(rr.Header().Get("Access-Control-Allow-Headers")), "x-stainless-lang") {
		t.Fatalf("rejected requested header echoed: %q", rr.Header().Get("Access-Control-Allow-Headers"))
	}
}

func TestResponsesUnauthorizedReplyStillEchoesAllowedOrigin(t *testing.T) {
	h, err := NewHandler(Options{
		AdmissionPolicy: &DataPlaneAdmissionPolicy{
			BindHostname:      "0.0.0.0",
			DataPlaneTokens:   []string{"secret"},
			CORSDefaultOrigin: "http://localhost:20200",
		},
		Providers: map[string]Provider{"p": admissionTestProvider()},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"p/model","store":false,"stream":false}`))
	req.Host = "gateway.example:20200"
	req.Header.Set("Origin", "http://gateway.example:20200")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "http://gateway.example:20200" {
		t.Fatalf("allow origin=%q", got)
	}
}

func TestPreflightDoesNotTurnUnknownRouteIntoMigratedSurface(t *testing.T) {
	h, err := NewHandler(Options{
		AdmissionPolicy: &DataPlaneAdmissionPolicy{BindHostname: "127.0.0.1"},
		Providers:       map[string]Provider{"p": admissionTestProvider()},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)

	req := httptest.NewRequest(http.MethodOptions, "/v1/live", nil)
	req.Host = "127.0.0.1:23100"
	req.Header.Set("Origin", "http://localhost:23100")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}
