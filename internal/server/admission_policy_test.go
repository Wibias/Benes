package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
)

func TestExplicitLoopbackAdmissionNeedsNoCredentialButEnforcesHostBoundary(t *testing.T) {
	provider := admissionTestProvider()
	h, err := NewHandler(Options{
		AdmissionPolicy: &DataPlaneAdmissionPolicy{BindHostname: "127.0.0.1"},
		Providers:       map[string]Provider{"p": provider},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)

	for _, host := range []string{"127.0.0.1:23100", "localhost:20100", "localhost.:23100", "[::1]:23100"} {
		rr := serveAdmissionRequest(h, host, "", "", "")
		if rr.Code != http.StatusOK {
			t.Fatalf("host=%q status=%d body=%s", host, rr.Code, rr.Body.String())
		}
	}

	rr := serveAdmissionRequest(h, "evil.example:23100", "", "", "")
	if rr.Code != http.StatusForbidden {
		t.Fatalf("remote Host on loopback listener status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestExplicitLoopbackAdmissionEnforcesOriginBoundaryAcrossForwardedPorts(t *testing.T) {
	h, err := NewHandler(Options{
		AdmissionPolicy: &DataPlaneAdmissionPolicy{BindHostname: "localhost"},
		Providers:       map[string]Provider{"p": admissionTestProvider()},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)

	for _, origin := range []string{"http://localhost:20100", "https://127.0.0.1:4443", "http://[::1]:3000"} {
		rr := serveAdmissionRequest(h, "localhost:23100", origin, "", "")
		if rr.Code != http.StatusOK {
			t.Fatalf("origin=%q status=%d body=%s", origin, rr.Code, rr.Body.String())
		}
	}

	rr := serveAdmissionRequest(h, "localhost:23100", "https://attacker.example", "", "")
	if rr.Code != http.StatusForbidden {
		t.Fatalf("foreign Origin status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestExplicitLoopbackAdmissionDoesNotRetainConfiguredTokens(t *testing.T) {
	h, err := NewHandler(Options{
		AdmissionPolicy: &DataPlaneAdmissionPolicy{
			BindHostname:    "127.0.0.1",
			DataPlaneTokens: []string{"unused-local-secret"},
		},
		Providers: map[string]Provider{"p": admissionTestProvider()},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	concrete := h.(*handler)
	if len(concrete.admission.tokenHashes) != 0 {
		t.Fatalf("loopback handler retained token digests: %#v", concrete.admission.tokenHashes)
	}
}

func TestExplicitRemoteAdmissionRequiresDedicatedHeader(t *testing.T) {
	h, err := NewHandler(Options{
		AdmissionPolicy: &DataPlaneAdmissionPolicy{
			BindHostname:    "0.0.0.0",
			DataPlaneTokens: []string{"remote-secret", "second-secret", "remote-secret"},
		},
		Providers: map[string]Provider{"p": admissionTestProvider()},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	concrete := h.(*handler)
	if len(concrete.admission.tokenHashes) != 2 {
		t.Fatalf("deduplicated token hashes=%d", len(concrete.admission.tokenHashes))
	}

	valid := serveAdmissionRequest(h, "gateway.example:23100", "", "remote-secret", "")
	if valid.Code != http.StatusOK {
		t.Fatalf("dedicated header status=%d body=%s", valid.Code, valid.Body.String())
	}

	for name, headers := range map[string]struct {
		dedicated string
		authorize string
		xAPIKey   string
	}{
		"missing":            {},
		"wrong dedicated":    {dedicated: "wrong"},
		"bearer reserved":    {authorize: "Bearer remote-secret"},
		"x-api-key reserved": {xAPIKey: "remote-secret"},
	} {
		rr := serveAdmissionRequest(h, "gateway.example:23100", "", headers.dedicated, headers.authorize)
		if headers.xAPIKey != "" {
			rr = serveAdmissionRequestWithXAPIKey(h, "gateway.example:23100", headers.xAPIKey)
		}
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("%s status=%d body=%s", name, rr.Code, rr.Body.String())
		}
	}
}

func TestExplicitRemoteAdmissionAppliesOriginPolicyAfterCredential(t *testing.T) {
	h, err := NewHandler(Options{
		AdmissionPolicy: &DataPlaneAdmissionPolicy{
			BindHostname:    "0.0.0.0",
			DataPlaneTokens: []string{"remote-secret"},
		},
		Providers: map[string]Provider{"p": admissionTestProvider()},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)

	for _, origin := range []string{"http://gateway.example:23100", "http://localhost:20100"} {
		rr := serveAdmissionRequest(h, "gateway.example:23100", origin, "remote-secret", "")
		if rr.Code != http.StatusOK {
			t.Fatalf("origin=%q status=%d body=%s", origin, rr.Code, rr.Body.String())
		}
	}

	rr := serveAdmissionRequest(h, "gateway.example:23100", "https://attacker.example", "remote-secret", "")
	if rr.Code != http.StatusForbidden {
		t.Fatalf("foreign Origin status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestExplicitAdmissionPolicyFailsClosedAtConstruction(t *testing.T) {
	provider := admissionTestProvider()
	for name, options := range map[string]Options{
		"blank bind": {
			AdmissionPolicy: &DataPlaneAdmissionPolicy{},
			Providers:       map[string]Provider{"p": provider},
		},
		"remote without credentials": {
			AdmissionPolicy: &DataPlaneAdmissionPolicy{BindHostname: "0.0.0.0"},
			Providers:       map[string]Provider{"p": provider},
		},
		"legacy singular combined": {
			AdmissionPolicy: &DataPlaneAdmissionPolicy{BindHostname: "127.0.0.1"},
			DataPlaneToken:  "legacy",
			Providers:       map[string]Provider{"p": provider},
		},
		"legacy plural combined": {
			AdmissionPolicy: &DataPlaneAdmissionPolicy{BindHostname: "127.0.0.1"},
			DataPlaneTokens: []string{"legacy"},
			Providers:       map[string]Provider{"p": provider},
		},
	} {
		if _, err := NewHandler(options); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
}

func admissionTestProvider() *fakeProvider {
	return &fakeProvider{events: []protocol.Event{
		{Type: protocol.EventTextDelta, Text: "ok"},
		{Type: protocol.EventDone},
	}}
}

func serveAdmissionRequest(h http.Handler, host, origin, dedicated, authorization string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"p/model","store":false,"stream":false}`))
	req.Host = host
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if dedicated != "" {
		req.Header.Set("X-Benes-API-Key", dedicated)
	}
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func serveAdmissionRequestWithXAPIKey(h http.Handler, host, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"p/model","store":false,"stream":false}`))
	req.Host = host
	req.Header.Set("X-Api-Key", token)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}
