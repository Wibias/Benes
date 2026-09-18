package server

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/harnessboard"
	"github.com/Wibias/Benes/internal/harnessidentity"
	"github.com/Wibias/Benes/internal/harnesspolicy"
	"github.com/Wibias/Benes/internal/protocol"
	providercontract "github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/sidecar/websearch"
)

type harnessSearchProvider struct {
	opens int
}

func (p *harnessSearchProvider) Open(context.Context, providercontract.DispatchRequest) (EventStream, error) {
	p.opens++
	if p.opens == 1 {
		return &harnessSearchStream{events: []protocol.Event{
			{Type: protocol.EventToolCallEnd, ID: "ws_1", Name: "web_search", Arguments: `{"query":"benes"}`},
			{Type: protocol.EventDone},
		}}, nil
	}
	return &harnessSearchStream{events: []protocol.Event{
		{Type: protocol.EventTextDelta, Text: "answer"},
		{Type: protocol.EventDone},
	}}, nil
}

type harnessSearchStream struct{ events []protocol.Event }

func (s *harnessSearchStream) Next() (protocol.Event, error) {
	if len(s.events) == 0 {
		return protocol.Event{}, io.EOF
	}
	event := s.events[0]
	s.events = s.events[1:]
	return event, nil
}
func (s *harnessSearchStream) Close() error { return nil }

type harnessSearchSurface struct {
	name string
	path string
	body string
}

var harnessSearchSurfaces = []harnessSearchSurface{
	{name: "responses", path: "/v1/responses", body: `{"model":"p/m","store":false,"stream":false,"tools":[{"type":"web_search"}]}`},
	{name: "chat", path: "/v1/chat/completions", body: `{"model":"p/m","messages":[{"role":"user","content":"search"}],"tools":[{"type":"web_search"}]}`},
	{name: "anthropic", path: "/v1/messages", body: `{"model":"p/m","max_tokens":64,"messages":[{"role":"user","content":"search"}],"tools":[{"type":"web_search_20250305","name":"web_search"}]}`},
}

func TestHarnessWebSearchEnableOverridesGlobalOffAcrossProtocols(t *testing.T) {
	for _, surface := range harnessSearchSurfaces {
		surface := surface
		t.Run(surface.name, func(t *testing.T) {
			sidecarCalls := 0
			sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				sidecarCalls++
				_, _ = w.Write([]byte(`{"text":"search result","sources":[]}`))
			}))
			t.Cleanup(sidecar.Close)
			provider := &harnessSearchProvider{}
			h := newHarnessSearchPolicyHandler(t, false, harnesspolicy.ActivationEnabled, provider, sidecar)
			req := harnessSearchRequest(surface)
			req.Header.Set(harnessidentity.Header, "opencode")
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
			}
			if sidecarCalls == 0 {
				t.Fatal("Harness ON + global OFF did not execute the web-search sidecar")
			}
			if provider.opens < 2 {
				t.Fatalf("provider opens=%d; sidecar continuation did not run", provider.opens)
			}
		})
	}
}

func TestHarnessWebSearchDisableOverridesGlobalOnAcrossProtocols(t *testing.T) {
	for _, surface := range harnessSearchSurfaces {
		surface := surface
		t.Run(surface.name, func(t *testing.T) {
			sidecarCalls := 0
			sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				sidecarCalls++
				_, _ = w.Write([]byte(`{"text":"search result","sources":[]}`))
			}))
			t.Cleanup(sidecar.Close)
			provider := &harnessSearchProvider{}
			h := newHarnessSearchPolicyHandler(t, true, harnesspolicy.ActivationDisabled, provider, sidecar)
			req := harnessSearchRequest(surface)
			req.Header.Set(harnessidentity.Header, "opencode")
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code != http.StatusNotImplemented {
				t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
			}
			if sidecarCalls != 0 {
				t.Fatalf("disabled Harness policy still executed sidecar %d time(s)", sidecarCalls)
			}
			if provider.opens != 0 {
				t.Fatalf("provider opened %d time(s) after fail-closed sidecar policy", provider.opens)
			}
		})
	}
}

func newHarnessSearchPolicyHandler(t *testing.T, global bool, override harnesspolicy.Activation, provider Provider, sidecar *httptest.Server) http.Handler {
	t.Helper()
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(fmt.Sprintf(`{"hostname":"127.0.0.1","port":18080,"webSearchSidecar":{"enabled":%t}}`, global)), 0o600); err != nil {
		t.Fatal(err)
	}
	row := harnessboard.DefaultSettings()
	row.Sidecars = &harnesspolicy.Overrides{WebSearch: &override}
	if _, err := harnessboard.PutSettings(home, "opencode", row); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "secret",
		Providers:      map[string]Provider{"p": provider},
		ConfigPath:     configPath,
		WebSearch: map[string]websearch.Config{
			"p": {
				ProviderID:    "p",
				Endpoint:      sidecar.URL,
				AuthClass:     "key",
				Wire:          "openai-responses",
				AllowedModels: []string{"m"},
				Enabled:       true,
				APIKey:        "sidecar-key",
				HTTPClient:    sidecar.Client(),
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return attachHandlerClose(t, h)
}

func harnessSearchRequest(surface harnessSearchSurface) *http.Request {
	req := httptest.NewRequest(http.MethodPost, surface.path, strings.NewReader(surface.body))
	req.Header.Set("Authorization", "Bearer secret")
	return req
}
