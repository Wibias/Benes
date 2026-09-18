package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	providercontract "github.com/Wibias/Benes/internal/providers"
)

type canonicalCaptureProvider struct {
	dispatch providercontract.DispatchRequest
	called   bool
}

func (p *canonicalCaptureProvider) Open(_ context.Context, dispatch providercontract.DispatchRequest) (EventStream, error) {
	p.called = true
	p.dispatch = dispatch
	return &sliceStream{events: []protocol.Event{{Type: protocol.EventDone}}}, nil
}

func TestResponsesDispatchesCanonicalParsedRequest(t *testing.T) {
	provider := &canonicalCaptureProvider{}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": provider},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	body := `{"model":"openai-apikey/gpt-5.6","input":"hello","stream":false,"temperature":0,"tool_choice":{"type":"function","name":"lookup"},"tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}],"future_field":{"x":1}}`
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer local-secret")
	req.Header.Set("X-Codex-Window-ID", "window-1")
	req.Header.Set("X-Caller-Secret", "unknown-marker")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !provider.called {
		t.Fatal("provider was not called")
	}
	got := provider.dispatch.Parsed
	if got.ModelID != "openai-apikey/gpt-5.6" || got.UpstreamModelID != "gpt-5.6" {
		t.Fatalf("models client=%q upstream=%q", got.ModelID, got.UpstreamModelID)
	}
	if got.Stream {
		t.Fatal("stream=true")
	}
	if len(got.Context.Messages) != 1 || got.Context.Messages[0].Content[0].Text != "hello" {
		t.Fatalf("messages=%+v", got.Context.Messages)
	}
	if len(got.Context.Tools) != 1 || got.Context.Tools[0].Name != "lookup" {
		t.Fatalf("tools=%+v", got.Context.Tools)
	}
	if got.Options.Temperature == nil || *got.Options.Temperature != 0 {
		t.Fatalf("temperature=%v", got.Options.Temperature)
	}
	if got.Options.ToolChoice == nil || got.Options.ToolChoice.Name != "lookup" {
		t.Fatalf("tool choice=%+v", got.Options.ToolChoice)
	}
	if !strings.Contains(string(got.Raw), `"future_field":{"x":1}`) {
		t.Fatalf("raw=%s", got.Raw)
	}
	if got := provider.dispatch.ForwardHeaders.Get("x-codex-window-id"); got != "window-1" {
		t.Fatalf("forward window=%q", got)
	}
	if got := provider.dispatch.ForwardHeaders.Get("x-caller-secret"); got != "" {
		t.Fatalf("unknown header leaked=%q", got)
	}
}
