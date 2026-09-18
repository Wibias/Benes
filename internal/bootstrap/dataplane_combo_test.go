package bootstrap

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/config"
)

func TestBuildDataPlaneWalksConfiguredComboOnTheLiveHandler(t *testing.T) {
	primaryHits := 0
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		primaryHits++
		http.Error(w, `{"error":{"message":"unavailable"}}`, http.StatusServiceUnavailable)
	}))
	defer primary.Close()
	fallback := newResponsesUpstream(t)
	defer fallback.Close()

	plane, err := BuildDataPlane(t.Context(), config.DiskConfig{
		Raw: json.RawMessage(`{"hostname":"127.0.0.1","port":23100,"combos":{"fast":{"targets":[{"provider":"primary","model":"gpt-a"},{"provider":"fallback","model":"gpt-b"}]}}}`),
		Providers: map[string]json.RawMessage{
			"primary":  localResponsesProvider(primary),
			"fallback": localResponsesProvider(fallback),
		},
	}, DataPlaneOptions{})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"combo/fast","store":false,"stream":true,"input":"hi"}`))
	req.Host = "127.0.0.1:23100"
	rr := httptest.NewRecorder()
	plane.Handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if primaryHits != 1 {
		t.Fatalf("primaryHits=%d", primaryHits)
	}
	if !strings.Contains(rr.Body.String(), `"delta":"hello"`) {
		t.Fatalf("body=%s", rr.Body.String())
	}
}

func TestBuildDataPlaneUnknownComboFailsClosed(t *testing.T) {
	upstream := newResponsesUpstream(t)
	defer upstream.Close()
	plane, err := BuildDataPlane(t.Context(), config.DiskConfig{
		Raw: json.RawMessage(`{"hostname":"127.0.0.1","port":23100,"combos":{"fast":{"targets":[{"provider":"local","model":"gpt-4o"}]}}}`),
		Providers: map[string]json.RawMessage{
			"local": localResponsesProvider(upstream),
		},
	}, DataPlaneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"combo/missing","store":false}`))
	req.Host = "127.0.0.1:23100"
	rr := httptest.NewRecorder()
	plane.Handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestBuildDataPlaneDoesNotInventComboWithoutConfig(t *testing.T) {
	upstream := newResponsesUpstream(t)
	defer upstream.Close()
	plane, err := BuildDataPlane(t.Context(), config.DiskConfig{
		Raw: json.RawMessage(`{"hostname":"127.0.0.1","port":23100}`),
		Providers: map[string]json.RawMessage{
			"local": localResponsesProvider(upstream),
		},
	}, DataPlaneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"combo/fast","store":false}`))
	req.Host = "127.0.0.1:23100"
	rr := httptest.NewRecorder()
	plane.Handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotImplemented {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}
