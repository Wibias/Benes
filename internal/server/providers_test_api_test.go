package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestProviderTestStaticCatalogAndDisabled(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	body := `{"providers":{"static":{"adapter":"openai-chat","baseUrl":"https://example.invalid/v1","liveModels":false},"off":{"adapter":"openai-chat","baseUrl":"https://example.invalid/v1","disabled":true}}}`
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"static": providerFunc(nil), "off": providerFunc(nil)},
		ConfigPath:     configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)

	static := httptest.NewRequest(http.MethodPost, "/api/providers/test?name=static", nil)
	static.Host = "127.0.0.1"
	staticRR := httptest.NewRecorder()
	h.ServeHTTP(staticRR, static)
	if staticRR.Code != http.StatusOK {
		t.Fatalf("static status=%d body=%s", staticRR.Code, staticRR.Body.String())
	}
	var staticBody struct {
		Applicable bool   `json:"applicable"`
		Reason     string `json:"reason"`
	}
	if err := json.Unmarshal(staticRR.Body.Bytes(), &staticBody); err != nil {
		t.Fatal(err)
	}
	if staticBody.Applicable != false || staticBody.Reason != "static_catalog" {
		t.Fatalf("static=%s", staticRR.Body.String())
	}

	off := httptest.NewRequest(http.MethodPost, "/api/providers/test?name=off", nil)
	off.Host = "127.0.0.1"
	offRR := httptest.NewRecorder()
	h.ServeHTTP(offRR, off)
	var offBody struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(offRR.Body.Bytes(), &offBody); err != nil {
		t.Fatal(err)
	}
	if offBody.OK || offBody.Error != "Provider is disabled" {
		t.Fatalf("off=%s", offRR.Body.String())
	}
}

func TestProviderTestPublishesWorkspaceHealthEvidence(t *testing.T) {
	originalDo := providerTestDo
	t.Cleanup(func() { providerTestDo = originalDo })

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	body := `{"providers":{"custom":{"adapter":"openai-chat","baseUrl":"https://example.invalid/v1","authMode":"key","apiKey":"test-key"}}}`
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"custom": providerFunc(nil)},
		ConfigPath:     configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	inner := h.(*handler)

	providerTestDo = func(string, string) (int, error) { return http.StatusServiceUnavailable, nil }
	failed := httptest.NewRequest(http.MethodPost, "/api/providers/test?name=custom", nil)
	failed.Host = "127.0.0.1"
	failedRR := httptest.NewRecorder()
	h.ServeHTTP(failedRR, failed)
	if failedRR.Code != http.StatusOK {
		t.Fatalf("failure status=%d body=%s", failedRR.Code, failedRR.Body.String())
	}
	failedView := inner.providersWorkspaceView()
	if len(failedView.Providers) != 1 || failedView.Providers[0].Lifecycle != "attention" {
		t.Fatalf("failure lifecycle=%#v", failedView.Providers)
	}
	if len(failedView.Attention) != 1 || failedView.Attention[0].Code != "provider_health_failure" {
		t.Fatalf("failure attention=%#v", failedView.Attention)
	}

	providerTestDo = func(string, string) (int, error) { return http.StatusOK, nil }
	succeeded := httptest.NewRequest(http.MethodPost, "/api/providers/test?name=custom", nil)
	succeeded.Host = "127.0.0.1"
	succeededRR := httptest.NewRecorder()
	h.ServeHTTP(succeededRR, succeeded)
	if succeededRR.Code != http.StatusOK {
		t.Fatalf("success status=%d body=%s", succeededRR.Code, succeededRR.Body.String())
	}
	succeededView := inner.providersWorkspaceView()
	if len(succeededView.Providers) != 1 || succeededView.Providers[0].Lifecycle != "healthy" {
		t.Fatalf("success lifecycle=%#v attention=%#v", succeededView.Providers, succeededView.Attention)
	}
	if succeededView.Providers[0].LastValidated == nil || *succeededView.Providers[0].LastValidated <= 0 {
		t.Fatalf("success lastValidated=%v", succeededView.Providers[0].LastValidated)
	}

	events := inner.activity.Recent(10)
	if len(events) != 3 ||
		events[0].Type != "provider_health_failure" ||
		events[1].Type != "provider_health_recovered" ||
		events[2].Type != "connection_validated" {
		t.Fatalf("events=%#v", events)
	}
}

func TestProviderTestSuccessWithoutPriorFailureDoesNotInventRecovery(t *testing.T) {
	originalDo := providerTestDo
	t.Cleanup(func() { providerTestDo = originalDo })
	providerTestDo = func(string, string) (int, error) { return http.StatusOK, nil }

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	body := `{"providers":{"custom":{"adapter":"openai-chat","baseUrl":"https://example.invalid/v1","authMode":"key","apiKey":"test-key"}}}`
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"custom": providerFunc(nil)},
		ConfigPath:     configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	inner := h.(*handler)

	req := httptest.NewRequest(http.MethodPost, "/api/providers/test?name=custom", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	events := inner.activity.Recent(10)
	if len(events) != 1 || events[0].Type != "connection_validated" {
		t.Fatalf("events=%#v", events)
	}
}

func TestProviderTestUnknownIsNotFound(t *testing.T) {
	h, err := NewHandler(Options{DataPlaneToken: "local-secret", Providers: map[string]Provider{"p": providerFunc(nil)}})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodPost, "/api/providers/test?name=missing", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}
