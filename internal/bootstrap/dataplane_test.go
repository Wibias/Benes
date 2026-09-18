package bootstrap

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/config"
)

func newResponsesUpstream(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Errorf("upstream path=%q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer provider-key" {
			t.Errorf("upstream Authorization=%q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\n")
		fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n")
	}))
}

func localResponsesProvider(upstream *httptest.Server) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"adapter":"openai-responses","baseUrl":%q,"apiKey":"provider-key","allowPrivateNetwork":true}`, upstream.URL))
}

func TestBuildDataPlaneConnectsPersistedLoopbackConfigWithoutClientCredential(t *testing.T) {
	upstream := newResponsesUpstream(t)
	defer upstream.Close()

	disk := config.DiskConfig{
		Raw:             json.RawMessage(`{"hostname":"127.0.0.1","port":23100}`),
		DataPlaneTokens: []string{"unused-on-loopback"},
		Providers: map[string]json.RawMessage{
			"local":  localResponsesProvider(upstream),
			"future": json.RawMessage(`{"adapter":"future-wire","baseUrl":"https://example.com","apiKey":"ignored"}`),
		},
	}

	plane, err := BuildDataPlane(t.Context(), disk, DataPlaneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if plane.Handler == nil || !plane.bindReady {
		t.Fatalf("handler=%v bindReady=%v", plane.Handler, plane.bindReady)
	}
	if plane.Listener.Hostname != "127.0.0.1" || plane.Listener.Port != 23100 {
		t.Fatalf("listener=%+v", plane.Listener)
	}
	if len(plane.Skipped) != 1 || plane.Skipped[0].ID != "future" {
		t.Fatalf("skipped=%#v", plane.Skipped)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"local/gpt-4o","store":false,"stream":true,"input":"hi"}`))
	req.Host = "127.0.0.1:23100"
	rr := httptest.NewRecorder()
	plane.Handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "response.completed") || !strings.Contains(rr.Body.String(), `"delta":"hello"`) {
		t.Fatalf("body=%s", rr.Body.String())
	}
}

func TestBuildDataPlaneServesCatalogModelsFromConfiguredProvider(t *testing.T) {
	upstream := newResponsesUpstream(t)
	defer upstream.Close()

	plane, err := BuildDataPlane(t.Context(), config.DiskConfig{
		Raw: json.RawMessage(`{"hostname":"127.0.0.1","port":23100,"modelContextWindows":{"local":{"gpt-5.6":{"tokens":200000,"mode":"override"}}}}`),
		Providers: map[string]json.RawMessage{
			"local": json.RawMessage(fmt.Sprintf(`{"adapter":"openai-responses","baseUrl":%q,"apiKey":"provider-key","allowPrivateNetwork":true,"models":["gpt-5.6"]}`, upstream.URL)),
		},
	}, DataPlaneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Host = "127.0.0.1:23100"
	rr := httptest.NewRecorder()
	plane.Handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"id":"local/gpt-5.6"`) || !strings.Contains(rr.Body.String(), `"context_window":200000`) {
		t.Fatalf("body=%s", rr.Body.String())
	}
}

func TestBuildDataPlaneConnectsPersistedRemoteConfigToDedicatedAdmission(t *testing.T) {
	upstream := newResponsesUpstream(t)
	defer upstream.Close()

	plane, err := BuildDataPlane(t.Context(), config.DiskConfig{
		Raw:             json.RawMessage(`{"hostname":"0.0.0.0","port":20200}`),
		DataPlaneTokens: []string{"client-key"},
		Providers:       map[string]json.RawMessage{"local": localResponsesProvider(upstream)},
	}, DataPlaneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if plane.Listener.Hostname != "0.0.0.0" || plane.Listener.Port != 20200 {
		t.Fatalf("listener=%+v", plane.Listener)
	}
	if plane.Handler == nil || !plane.bindReady {
		t.Fatalf("handler=%v bindReady=%v", plane.Handler, plane.bindReady)
	}

	body := `{"model":"local/gpt-4o","store":false,"stream":false,"input":"hi"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	req.Host = "gateway.example:20200"
	req.Header.Set("X-Benes-API-Key", "client-key")
	rr := httptest.NewRecorder()
	plane.Handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("dedicated status=%d body=%s", rr.Code, rr.Body.String())
	}

	bearer := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	bearer.Host = "gateway.example:20200"
	bearer.Header.Set("Authorization", "Bearer client-key")
	bearerRR := httptest.NewRecorder()
	plane.Handler.ServeHTTP(bearerRR, bearer)
	if bearerRR.Code != http.StatusUnauthorized {
		t.Fatalf("bearer status=%d body=%s", bearerRR.Code, bearerRR.Body.String())
	}
}

func TestBuildDataPlaneRejectsConfigWithoutMigratedProviders(t *testing.T) {
	plane, err := BuildDataPlane(t.Context(), config.DiskConfig{
		Raw: json.RawMessage(`{}`),
		Providers: map[string]json.RawMessage{
			"future": json.RawMessage(`{"adapter":"future-wire","baseUrl":"https://example.com","apiKey":"k"}`),
		},
	}, DataPlaneOptions{})
	if err == nil || plane.Handler != nil || plane.bindReady {
		t.Fatalf("plane=%#v err=%v", plane, err)
	}
}

func TestBuildDataPlaneRejectsRemoteBindWithoutCredentialBeforeProviderConstruction(t *testing.T) {
	plane, err := BuildDataPlane(t.Context(), config.DiskConfig{
		Raw: json.RawMessage(`{"hostname":"0.0.0.0"}`),
		Providers: map[string]json.RawMessage{
			"private": json.RawMessage(`{"adapter":"openai-chat","baseUrl":"http://127.0.0.1:1/v1","apiKey":"k"}`),
		},
	}, DataPlaneOptions{})
	if err == nil || plane.Handler != nil || plane.bindReady {
		t.Fatalf("plane=%#v err=%v", plane, err)
	}
	if !strings.Contains(err.Error(), "validate data-plane admission") || strings.Contains(err.Error(), "provider registry") {
		t.Fatalf("wrong failure order: %v", err)
	}
}

func TestBuildDataPlaneFailsClosedWhenProviderConstructionFailsWithoutLeakingCredential(t *testing.T) {
	secret := "TOP-SECRET-UPSTREAM-KEY"
	plane, err := BuildDataPlane(t.Context(), config.DiskConfig{
		Raw: json.RawMessage(`{}`),
		Providers: map[string]json.RawMessage{
			"private": json.RawMessage(`{"adapter":"openai-chat","baseUrl":"http://127.0.0.1:1/v1","apiKey":"` + secret + `"}`),
		},
	}, DataPlaneOptions{})
	if err == nil || plane.Handler != nil || plane.bindReady {
		t.Fatalf("plane=%#v err=%v", plane, err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("credential leaked in bootstrap error: %v", err)
	}
}

func TestBuildDataPlaneAcceptsRuntimeAdmissionTokenForRemoteBind(t *testing.T) {
	upstream := newResponsesUpstream(t)
	defer upstream.Close()

	plane, err := BuildDataPlane(t.Context(), config.DiskConfig{
		Raw:       json.RawMessage(`{"hostname":"0.0.0.0","port":20200}`),
		Providers: map[string]json.RawMessage{"local": localResponsesProvider(upstream)},
	}, DataPlaneOptions{RuntimeDataPlaneTokens: []string{"runtime-key"}})
	if err != nil {
		t.Fatal(err)
	}
	if !plane.bindReady {
		t.Fatal("plane not bind-ready")
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"local/gpt-4o","store":false,"stream":false,"input":"hi"}`))
	req.Host = "gateway.example:20200"
	req.Header.Set("X-Benes-API-Key", "runtime-key")
	rr := httptest.NewRecorder()
	plane.Handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}
