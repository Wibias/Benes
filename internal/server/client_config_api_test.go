package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/catalog"
)

func TestClientConfigAPIIsLoopbackAndOmitsSecrets(t *testing.T) {
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		CatalogModels:  []catalog.Model{{ID: "openai-apikey/gpt-5.5", Context: catalog.ContextWindow{Tokens: 128000}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	blocked := httptest.NewRequest(http.MethodGet, "/api/client-config?client=opencode", nil)
	blocked.Header.Set("Authorization", "Bearer local-secret")
	blockedRR := httptest.NewRecorder()
	h.ServeHTTP(blockedRR, blocked)
	if blockedRR.Code != http.StatusNotFound {
		t.Fatalf("data-plane status=%d body=%s", blockedRR.Code, blockedRR.Body.String())
	}
	req := httptest.NewRequest(http.MethodGet, "/api/client-config?client=opencode", nil)
	req.Host = "127.0.0.1:18080"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "sk-") || strings.Contains(strings.ToLower(rr.Body.String()), "secret") {
		t.Fatalf("secret leaked: %s", rr.Body.String())
	}
	var got struct {
		Client     string `json:"client"`
		Format     string `json:"format"`
		ModelCount int    `json:"modelCount"`
		Text       string `json:"text"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Client != "opencode" || got.Format != "json" || got.ModelCount != 1 || !strings.Contains(got.Text, "openai-apikey/gpt-5.5") {
		t.Fatalf("%+v", got)
	}
}

func TestClientConfigAPIRejectsRelativePiCodingAgentDir(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", "pi-elsewhere")
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodGet, "/api/client-config?client=pi", nil)
	req.Host = "127.0.0.1:18080"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestClientConfigAPIRejectsRelativePrimeCodingAgentDir(t *testing.T) {
	t.Setenv("PRIME_AGENT_CODING_AGENT_DIR", "prime-elsewhere")
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodGet, "/api/client-config?client=prime", nil)
	req.Host = "127.0.0.1:18080"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "absolute") {
		t.Fatalf("body=%s", rr.Body.String())
	}
}

func TestClientConfigAPIRejectsUnknownClient(t *testing.T) {
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodGet, "/api/client-config?client=nope", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}
