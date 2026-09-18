package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	providercontract "github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/sidecar/websearch"
)

const alphaSearchBody = `{"id":"search-session","model":"openai-apikey/gpt-5.6","commands":{"search_query":[{"q":"benes docs"}]}}`

func TestAlphaSearchUsesConfiguredEligibleBackend(t *testing.T) {
	var gotQuery string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotQuery, _ = body["query"].(string)
		_, _ = w.Write([]byte(`{"text":"Benes is a local Go proxy.","sources":[{"url":"https://example.com/benes","title":"Benes"}]}`))
	}))
	t.Cleanup(upstream.Close)
	h := newAlphaSearchHandler(t, Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		WebSearch:      map[string]websearch.Config{"exa": eligibleSearchConfig(upstream)},
	})
	rr := postAlphaSearch(t, h, alphaSearchBody)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if gotQuery != "benes docs" {
		t.Fatalf("query=%q", gotQuery)
	}
	got := decodeJSONMap(t, rr.Body.Bytes())
	if got["output"] != "Benes is a local Go proxy." {
		t.Fatalf("output=%v", got["output"])
	}
	if got["encrypted_output"] != nil {
		t.Fatalf("encrypted_output=%v", got["encrypted_output"])
	}
	results, _ := got["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("results=%v", got["results"])
	}
	row, _ := results[0].(map[string]any)
	if row["type"] != "text_result" || row["url"] != "https://example.com/benes" || row["title"] != "Benes" {
		t.Fatalf("citation=%v", row)
	}
}

func TestAlphaSearchFailsClosedWithoutBackend(t *testing.T) {
	h := newAlphaSearchHandler(t, Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
	})
	rr := postAlphaSearch(t, h, alphaSearchBody)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if strings.Contains(strings.ToLower(body), "chatgpt") {
		t.Fatalf("must not demand ChatGPT OAuth: %s", body)
	}
	if !strings.Contains(body, "web search") {
		t.Fatalf("body=%s", body)
	}
}

func TestAlphaSearchReportsBackendFailure(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, `{"error":"upstream-search-failed"}`)
	}))
	t.Cleanup(upstream.Close)
	h := newAlphaSearchHandler(t, Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		WebSearch:      map[string]websearch.Config{"exa": eligibleSearchConfig(upstream)},
	})
	rr := postAlphaSearch(t, h, alphaSearchBody)
	if rr.Code != http.StatusBadGateway {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	body := strings.ToLower(rr.Body.String())
	if strings.Contains(body, "chatgpt") && strings.Contains(body, "required") {
		t.Fatalf("must not demand ChatGPT OAuth: %s", rr.Body.String())
	}
	if !strings.Contains(body, "exa") && !strings.Contains(body, "sidecar") {
		t.Fatalf("missing backend diagnostic: %s", rr.Body.String())
	}
}

func TestAlphaSearchKeepsSearchSeparateFromRoutedMainModel(t *testing.T) {
	providerCalled := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"answer":"from exa","sources":[{"url":"https://example.com/a","title":"A"}]}`))
	}))
	t.Cleanup(upstream.Close)
	h := newAlphaSearchHandler(t, Options{
		DataPlaneToken: "local-secret",
		Providers: map[string]Provider{"openai-apikey": providerFunc(func(context.Context, providercontract.DispatchRequest) (EventStream, error) {
			providerCalled = true
			return nil, nil
		})},
		WebSearch: map[string]websearch.Config{"exa": eligibleSearchConfig(upstream)},
	})
	rr := postAlphaSearch(t, h, alphaSearchBody)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if providerCalled {
		t.Fatal("routed main model provider must not serve alpha/search")
	}
	got := decodeJSONMap(t, rr.Body.Bytes())
	if got["output"] != "from exa" {
		t.Fatalf("output=%v", got["output"])
	}
}

func TestAlphaSearchHonorsSidecarEnabledFalse(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte(`{"webSearchSidecar":{"enabled":false,"backend":"dedicated_search"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("disabled sidecar must not call a search backend")
	}))
	t.Cleanup(upstream.Close)
	h := newAlphaSearchHandler(t, Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		ConfigPath:     configPath,
		WebSearch:      map[string]websearch.Config{"exa": eligibleSearchConfig(upstream)},
	})
	rr := postAlphaSearch(t, h, alphaSearchBody)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestAlphaSearchRelaysNativeCodexBackend(t *testing.T) {
	native := &nativeSearchProvider{payload: []byte(`{"encrypted_output":null,"output":"native-relay","results":[]}`)}
	h := newAlphaSearchHandler(t, Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai": native, "openai-apikey": providerFunc(nil)},
		WebSearch: map[string]websearch.Config{
			"openai": {
				ProviderID: "openai",
				Endpoint:   "https://chatgpt.com/backend-api/codex",
				AuthClass:  "forward",
				Wire:       "openai-responses",
				Enabled:    true,
			},
		},
	})
	rr := postAlphaSearch(t, h, alphaSearchBody)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !native.called {
		t.Fatal("native Codex search was not relayed")
	}
	if !strings.Contains(string(native.body), `"q":"benes docs"`) {
		t.Fatalf("relay body=%s", native.body)
	}
	got := decodeJSONMap(t, rr.Body.Bytes())
	if got["output"] != "native-relay" {
		t.Fatalf("output=%v", got["output"])
	}
}

type nativeSearchProvider struct {
	fakeProvider
	payload []byte
	body    []byte
	called  bool
	opened  bool
}

func (p *nativeSearchProvider) NativeCodexForward() bool { return true }

func (p *nativeSearchProvider) Open(_ context.Context, dispatch providercontract.DispatchRequest) (EventStream, error) {
	p.opened = true
	return p.fakeProvider.Open(context.Background(), dispatch)
}

func (p *nativeSearchProvider) Search(_ context.Context, _ providercontract.DispatchRequest, body []byte) (int, http.Header, []byte, error) {
	p.called = true
	p.body = append([]byte(nil), body...)
	header := http.Header{}
	header.Set("Content-Type", "application/json")
	return http.StatusOK, header, append([]byte(nil), p.payload...), nil
}

func eligibleSearchConfig(upstream *httptest.Server) websearch.Config {
	return websearch.Config{
		ProviderID:    "exa",
		Endpoint:      upstream.URL,
		AuthClass:     "key",
		Wire:          "search-json",
		ModelID:       "exa-search",
		AllowedModels: []string{"exa-search"},
		Enabled:       true,
		APIKey:        "sidecar-key",
		HTTPClient:    upstream.Client(),
	}
}

func newAlphaSearchHandler(t *testing.T, options Options) http.Handler {
	t.Helper()
	h, err := NewHandler(options)
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	return h
}

func postAlphaSearch(t *testing.T, h http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/alpha/search", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer local-secret")
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}
