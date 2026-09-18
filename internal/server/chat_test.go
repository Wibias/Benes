package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	providercontract "github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/resourcebudget"
	"github.com/Wibias/Benes/internal/timeline"
)

type providerFuncChat func(context.Context, providercontract.DispatchRequest) (EventStream, error)

func (f providerFuncChat) Open(ctx context.Context, r providercontract.DispatchRequest) (EventStream, error) {
	return f(ctx, r)
}

type chatRouteProvider struct {
	events         []protocol.Event
	openErr        error
	request        protocol.ParsedRequest
	forwardHeaders providercontract.ForwardHeaders
	stream         *chatRouteStream
}

func (p *chatRouteProvider) Open(_ context.Context, dispatch providercontract.DispatchRequest) (EventStream, error) {
	p.request = dispatch.Parsed
	p.forwardHeaders = dispatch.ForwardHeaders
	if p.openErr != nil {
		return nil, p.openErr
	}
	p.stream = &chatRouteStream{events: append([]protocol.Event(nil), p.events...)}
	return p.stream, nil
}

type chatRouteStream struct {
	events []protocol.Event
	err    error
	closed bool
}

func (s *chatRouteStream) Next() (protocol.Event, error) {
	if len(s.events) > 0 {
		event := s.events[0]
		s.events = s.events[1:]
		return event, nil
	}
	if s.err != nil {
		err := s.err
		s.err = nil
		return protocol.Event{}, err
	}
	return protocol.Event{}, io.EOF
}

func (s *chatRouteStream) Close() error {
	s.closed = true
	return nil
}

func newChatRouteHandler(t *testing.T, provider Provider, maxBytes int64) http.Handler {
	t.Helper()
	h, err := NewHandler(Options{
		DataPlaneToken:  "local-secret",
		Providers:       map[string]Provider{"openai-apikey": provider},
		MaxRequestBytes: maxBytes,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	return h
}

func chatRequest(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer local-secret")
	return req
}

func TestChatCompletionsAttachesTraceToProviderContext(t *testing.T) {
	var got *timeline.Trace
	provider := providerFuncChat(func(ctx context.Context, _ providercontract.DispatchRequest) (EventStream, error) {
		got = timeline.FromContext(ctx)
		return &chatRouteStream{events: []protocol.Event{{Type: protocol.EventDone}}}, nil
	})
	h := newChatRouteHandler(t, provider, 0)
	req := chatRequest(`{"model":"openai-apikey/gpt-5.6","messages":[{"role":"user","content":"hi"}]}`)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if got == nil || got.ID() == "" {
		t.Fatalf("provider context missing trace: %#v", got)
	}
}

func TestChatCompletionsUsesSharedAdmissionWithChatErrorShape(t *testing.T) {
	h := newChatRouteHandler(t, &chatRouteProvider{}, 0)
	for _, auth := range []string{"", "Bearer wrong", "Basic local-secret"} {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","messages":[{"role":"user","content":"hi"}]}`))
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("auth=%q status=%d body=%s", auth, rr.Code, rr.Body.String())
		}
		if rr.Header().Get("WWW-Authenticate") != "Bearer" {
			t.Fatalf("auth=%q WWW-Authenticate=%q", auth, rr.Header().Get("WWW-Authenticate"))
		}
		assertChatError(t, rr, "invalid_request_error", "invalid_api_key")
	}
}

func TestChatCompletionsRoutesCanonicalRequestAndStreamsChatWire(t *testing.T) {
	provider := &chatRouteProvider{events: []protocol.Event{
		{Type: protocol.EventTextDelta, Text: "hello"},
		{Type: protocol.EventDone, Usage: &protocol.Usage{InputTokens: 2, OutputTokens: 1}},
	}}
	h := newChatRouteHandler(t, provider, 0)
	req := chatRequest(`{"model":"openai-apikey/gpt-5.6","messages":[{"role":"user","content":"hi"}],"stream":true,"future_field":1}`)
	req.Header.Set("X-Codex-Window-ID", "window-1")
	req.Header.Set("X-Caller-Secret", "unknown-marker")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content-type=%q", ct)
	}
	if provider.request.Source != protocol.RequestSourceChatCompletions {
		t.Fatalf("source=%q", provider.request.Source)
	}
	if provider.request.ModelID != "openai-apikey/gpt-5.6" || provider.request.UpstreamModelID != "gpt-5.6" {
		t.Fatalf("model=%q upstream=%q", provider.request.ModelID, provider.request.UpstreamModelID)
	}
	if !strings.Contains(string(provider.request.Raw), `"future_field":1`) {
		t.Fatalf("raw=%s", provider.request.Raw)
	}
	if got := provider.forwardHeaders.Get("x-codex-window-id"); got != "window-1" {
		t.Fatalf("forward window=%q", got)
	}
	if got := provider.forwardHeaders.Get("x-caller-secret"); got != "" {
		t.Fatalf("unknown header leaked=%q", got)
	}
	if provider.stream == nil || !provider.stream.closed {
		t.Fatal("provider stream was not closed by server")
	}
	body := rr.Body.String()
	for _, want := range []string{`"object":"chat.completion.chunk"`, `"model":"openai-apikey/gpt-5.6"`, `"content":"hello"`, `"finish_reason":"stop"`, "data: [DONE]"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q: %s", want, body)
		}
	}
}

func TestChatCompletionsNonStreamingReturnsChatCompletion(t *testing.T) {
	provider := &chatRouteProvider{events: []protocol.Event{
		{Type: protocol.EventReasoningRawDelta, Text: "think"},
		{Type: protocol.EventTextDelta, Text: "answer"},
		{Type: protocol.EventDone, Usage: &protocol.Usage{InputTokens: 3, OutputTokens: 4}},
	}}
	h := newChatRouteHandler(t, provider, 0)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, chatRequest(`{"model":"openai-apikey/gpt-5.6","messages":[{"role":"user","content":"hi"}],"stream":false}`))

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var completion map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &completion); err != nil {
		t.Fatal(err)
	}
	if completion["object"] != "chat.completion" || completion["model"] != "openai-apikey/gpt-5.6" {
		t.Fatalf("completion=%#v", completion)
	}
	choice := completion["choices"].([]any)[0].(map[string]any)
	message := choice["message"].(map[string]any)
	if message["content"] != "answer" || message["reasoning_content"] != "think" || choice["finish_reason"] != "stop" {
		t.Fatalf("choice=%#v", choice)
	}
	if provider.stream == nil || !provider.stream.closed {
		t.Fatal("provider stream was not closed by server")
	}
}

func TestChatCompletionsFailsClosedWhenOutputBudgetExceeded(t *testing.T) {
	budget := resourcebudget.NewManager(resourcebudget.Limits{
		ClassBytes: map[resourcebudget.Class]int64{resourcebudget.ClassOutput: 4},
	})
	provider := &chatRouteProvider{events: []protocol.Event{
		{Type: protocol.EventTextDelta, Text: "hello world"},
		{Type: protocol.EventDone},
	}}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": provider},
		ResourceBudget: budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, chatRequest(`{"model":"openai-apikey/gpt-5.6","messages":[{"role":"user","content":"hi"}],"stream":false}`))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "resource_exhausted") {
		t.Fatalf("body=%s", rr.Body.String())
	}
}

func TestChatCompletionsNonStreamingPreservesTypedProviderFailure(t *testing.T) {
	provider := &chatRouteProvider{events: []protocol.Event{{
		Type: protocol.EventError, Message: "slow down", HTTPStatus: http.StatusTooManyRequests,
		ErrorType: "rate_limit_error", Code: "rate_limit_exceeded",
	}}}
	h := newChatRouteHandler(t, provider, 0)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, chatRequest(`{"model":"openai-apikey/gpt-5.6","messages":[{"role":"user","content":"hi"}]}`))
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	assertChatError(t, rr, "rate_limit_error", "rate_limit_exceeded")
}

func TestChatCompletionsRejectsImplicitUnregisteredAndOversizedRequests(t *testing.T) {
	provider := &chatRouteProvider{}
	h := newChatRouteHandler(t, provider, 0)
	for _, model := range []string{"gpt-5.6", "unknown/gpt-5.6"} {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, chatRequest(`{"model":"`+model+`","messages":[{"role":"user","content":"hi"}]}`))
		if rr.Code != http.StatusNotImplemented {
			t.Fatalf("model=%q status=%d body=%s", model, rr.Code, rr.Body.String())
		}
		assertChatError(t, rr, "invalid_request_error", "migration_not_ready")
	}

	small := newChatRouteHandler(t, provider, 32)
	rr := httptest.NewRecorder()
	small.ServeHTTP(rr, chatRequest(`{"model":"openai-apikey/gpt-5.6","messages":[{"role":"user","content":"this body is deliberately too large"}]}`))
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	assertChatError(t, rr, "invalid_request_error", "request_too_large")
}

func TestChatCompletionsProviderOpenFailureIsRedacted(t *testing.T) {
	provider := &chatRouteProvider{openErr: errors.New("SECRET upstream credential detail")}
	h := newChatRouteHandler(t, provider, 0)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, chatRequest(`{"model":"openai-apikey/gpt-5.6","messages":[{"role":"user","content":"hi"}]}`))
	if rr.Code != http.StatusBadGateway {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "SECRET") {
		t.Fatalf("provider detail leaked: %s", rr.Body.String())
	}
	assertChatError(t, rr, "upstream_error", "")
}

func TestChatCompletionsCORSPreflightUsesSupportedRoute(t *testing.T) {
	h := newChatRouteHandler(t, &chatRouteProvider{}, 0)
	req := httptest.NewRequest(http.MethodOptions, "/v1/chat/completions", nil)
	req.Header.Set("Origin", "http://localhost:23100")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if rr.Header().Get("Access-Control-Allow-Origin") != "http://localhost:23100" {
		t.Fatalf("headers=%v", rr.Header())
	}
}

func assertChatError(t *testing.T, rr *httptest.ResponseRecorder, wantType, wantCode string) {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	errorBody, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatalf("body=%#v", body)
	}
	if errorBody["type"] != wantType || errorBody["param"] != nil {
		t.Fatalf("error=%#v", errorBody)
	}
	if wantCode == "" {
		if errorBody["code"] != nil {
			t.Fatalf("error=%#v", errorBody)
		}
	} else if errorBody["code"] != wantCode {
		t.Fatalf("error=%#v", errorBody)
	}
	if message, _ := errorBody["message"].(string); message == "" {
		t.Fatalf("error=%#v", errorBody)
	}
}
