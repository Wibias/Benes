package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/credentials"
	"github.com/Wibias/Benes/internal/protocol"
	providercontract "github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/sidecar/websearch"
)

func TestChatCompletionsHostedWebSearchFailsClosedAsMigrationNotReady(t *testing.T) {
	providerCalled := false
	provider := providerFunc(func(context.Context, providercontract.DispatchRequest) (EventStream, error) {
		providerCalled = true
		return nil, nil
	})
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": provider},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)

	req := httptest.NewRequest(http.MethodPost, chatCompletionsPath, strings.NewReader(`{"model":"openai-apikey/gpt-5.6","messages":[{"role":"user","content":"search"}],"tools":[{"type":"web_search"}]}`))
	req.Header.Set("Authorization", "Bearer local-secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotImplemented {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if providerCalled {
		t.Fatal("provider was called for an unmigrated hosted Chat tool")
	}
	assertChatError(t, rr, "invalid_request_error", "web_search_unavailable")
}

func TestChatCompletionsSelectedWebSearchInjectsSyntheticTool(t *testing.T) {
	var dispatched []protocol.Tool
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"sources":[]}`))
	}))
	t.Cleanup(sidecar.Close)
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers: map[string]Provider{"openai-apikey": providerFunc(func(_ context.Context, dispatch providercontract.DispatchRequest) (EventStream, error) {
			dispatched = append([]protocol.Tool(nil), dispatch.Parsed.Context.Tools...)
			return &chatRouteStream{events: []protocol.Event{{Type: protocol.EventDone}}}, nil
		})},
		WebSearch: map[string]websearch.Config{
			"openai-apikey": {
				ProviderID:    "openai-apikey",
				Endpoint:      sidecar.URL,
				AuthClass:     "key",
				Wire:          "openai-responses",
				AllowedModels: []string{"gpt-5.6"},
				Enabled:       true,
				APIKey:        "sidecar-key",
				HTTPClient:    sidecar.Client(),
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodPost, chatCompletionsPath, strings.NewReader(`{"model":"openai-apikey/gpt-5.6","messages":[{"role":"user","content":"search"}],"tools":[{"type":"web_search_preview"},{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}]}`))
	req.Header.Set("Authorization", "Bearer local-secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if len(dispatched) != 2 || dispatched[0].Name != "lookup" || dispatched[1].Name != "web_search" || dispatched[1].Description == "" || dispatched[1].HostedWebSearch {
		t.Fatalf("tools=%#v", dispatched)
	}
}

func TestChatCompletionsSelectedWebSearchWrapsProviderStream(t *testing.T) {
	sidecarCalled := false
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sidecarCalled = true
		if got := r.Header.Get("Authorization"); got != "Bearer sidecar-key" {
			t.Errorf("sidecar auth=%q", got)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["query"] != "benes" {
			t.Errorf("sidecar query=%#v", body["query"])
		}
		_, _ = w.Write([]byte(`{"sources":[{"url":"https://example.com","title":"Example"}]}`))
	}))
	t.Cleanup(sidecar.Close)

	providerCalled := false
	provider := providerFunc(func(context.Context, providercontract.DispatchRequest) (EventStream, error) {
		providerCalled = true
		return &chatRouteStream{events: []protocol.Event{
			{Type: protocol.EventToolCallEnd, ID: "c1", Name: "web_search", Arguments: `{"query":"benes"}`},
			{Type: protocol.EventDone},
		}}, nil
	})
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": provider},
		WebSearch: map[string]websearch.Config{
			"openai-apikey": {
				ProviderID:    "openai-apikey",
				Endpoint:      sidecar.URL,
				AuthClass:     "key",
				Wire:          "openai-responses",
				AllowedModels: []string{"gpt-5.6"},
				Enabled:       true,
				APIKey:        "sidecar-key",
				HTTPClient:    sidecar.Client(),
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodPost, chatCompletionsPath, strings.NewReader(`{"model":"openai-apikey/gpt-5.6","messages":[{"role":"user","content":"search"}],"tools":[{"type":"web_search"}]}`))
	req.Header.Set("Authorization", "Bearer local-secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !providerCalled {
		t.Fatal("provider was not opened for a selected web-search turn")
	}
	if !sidecarCalled {
		t.Fatal("web-search sidecar was not executed")
	}
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	msg := payload["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)
	annotations := msg["annotations"].([]any)
	if len(annotations) != 1 {
		t.Fatalf("annotations=%#v", msg["annotations"])
	}
	citation := annotations[0].(map[string]any)
	if citation["type"] != "url_citation" || citation["url"] != "https://example.com" || citation["title"] != "Example" {
		t.Fatalf("citation=%#v", citation)
	}
}

func TestChatCompletionsSelectedWebSearchResolvesCredentialRef(t *testing.T) {
	store, err := credentials.NewFileStore(t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := store.Put("openai-apikey", []byte("sidecar-key"))
	if err != nil {
		t.Fatal(err)
	}
	sidecarCalled := false
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sidecarCalled = true
		if got := r.Header.Get("Authorization"); got != "Bearer sidecar-key" {
			t.Errorf("sidecar auth=%q", got)
		}
		_, _ = w.Write([]byte(`{"sources":[{"url":"https://example.com","title":"Example"}]}`))
	}))
	t.Cleanup(sidecar.Close)
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers: map[string]Provider{"openai-apikey": providerFunc(func(context.Context, providercontract.DispatchRequest) (EventStream, error) {
			return &chatRouteStream{events: []protocol.Event{
				{Type: protocol.EventToolCallEnd, ID: "c1", Name: "web_search", Arguments: `{"query":"benes"}`},
				{Type: protocol.EventDone},
			}}, nil
		})},
		Credentials: store,
		WebSearch: map[string]websearch.Config{
			"openai-apikey": {
				ProviderID:    "openai-apikey",
				Endpoint:      sidecar.URL,
				AuthClass:     "key",
				Wire:          "openai-responses",
				AllowedModels: []string{"gpt-5.6"},
				Enabled:       true,
				CredentialRef: ref,
				HTTPClient:    sidecar.Client(),
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodPost, chatCompletionsPath, strings.NewReader(`{"model":"openai-apikey/gpt-5.6","messages":[{"role":"user","content":"search"}],"tools":[{"type":"web_search"}]}`))
	req.Header.Set("Authorization", "Bearer local-secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !sidecarCalled {
		t.Fatal("web-search sidecar was not executed")
	}
}

func TestChatCompletionsSelectedWebSearchFailsClosedWithoutCredential(t *testing.T) {
	providerCalled := false
	provider := providerFunc(func(context.Context, providercontract.DispatchRequest) (EventStream, error) {
		providerCalled = true
		return nil, nil
	})
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": provider},
		WebSearch: map[string]websearch.Config{
			"openai-apikey": {
				ProviderID:    "openai-apikey",
				Endpoint:      "https://api.openai.com/v1/responses",
				AuthClass:     "key",
				Wire:          "openai-responses",
				AllowedModels: []string{"gpt-5.6"},
				Enabled:       true,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodPost, chatCompletionsPath, strings.NewReader(`{"model":"openai-apikey/gpt-5.6","messages":[{"role":"user","content":"search"}],"tools":[{"type":"web_search"}]}`))
	req.Header.Set("Authorization", "Bearer local-secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotImplemented {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if providerCalled {
		t.Fatal("provider was called without a resolvable sidecar credential")
	}
	assertChatError(t, rr, "invalid_request_error", "web_search_unavailable")
}
