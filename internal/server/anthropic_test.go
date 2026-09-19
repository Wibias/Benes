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

	"github.com/Wibias/Benes/internal/capability"
	"github.com/Wibias/Benes/internal/protocol"
	providercontract "github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/resourcebudget"
	"github.com/Wibias/Benes/internal/sidecar/websearch"
	"github.com/Wibias/Benes/internal/timeline"
)

type providerFuncAnthropic func(context.Context, providercontract.DispatchRequest) (EventStream, error)

func (f providerFuncAnthropic) Open(c context.Context, r providercontract.DispatchRequest) (EventStream, error) {
	return f(c, r)
}

type trackedAnthropicStream struct {
	events []protocol.Event
	closed int
}

func (s *trackedAnthropicStream) Next() (protocol.Event, error) {
	if len(s.events) == 0 {
		return protocol.Event{}, io.EOF
	}
	e := s.events[0]
	s.events = s.events[1:]
	return e, nil
}
func (s *trackedAnthropicStream) Close() error { s.closed++; return nil }

func TestAnthropicMessagesFailsClosedWhenOutputBudgetExceeded(t *testing.T) {
	budget := resourcebudget.NewManager(resourcebudget.Limits{
		ClassBytes: map[resourcebudget.Class]int64{resourcebudget.ClassOutput: 4},
	})
	h, err := NewHandler(Options{
		DataPlaneToken: "secret",
		Providers: map[string]Provider{"p": providerFuncAnthropic(func(context.Context, providercontract.DispatchRequest) (EventStream, error) {
			return &trackedAnthropicStream{events: []protocol.Event{
				{Type: protocol.EventTextDelta, Text: "hello world"},
				{Type: protocol.EventDone, StopReason: "end_turn"},
			}}, nil
		})},
		ResourceBudget: budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"p/m","max_tokens":64,"messages":[{"role":"user","content":"hello"}]}`))
	req.Header.Set("Authorization", "Bearer secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "resource_exhausted") {
		t.Fatalf("body=%s", rr.Body.String())
	}
}

func TestAnthropicStreamDispatchPreservesClientModelAndClosesProvider(t *testing.T) {
	var got providercontract.DispatchRequest
	stream := &trackedAnthropicStream{events: []protocol.Event{{Type: protocol.EventTextDelta, Text: "hi"}, {Type: protocol.EventDone, StopReason: "stop"}}}
	h, _ := NewHandler(Options{DataPlaneToken: "secret", Providers: map[string]Provider{"p": providerFuncAnthropic(func(_ context.Context, r providercontract.DispatchRequest) (EventStream, error) {
		got = r
		return stream, nil
	})}})
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"p/public-alias","max_tokens":64,"stream":true,"messages":[{"role":"user","content":"hello"}]}`))
	req.Header.Set("Authorization", "Bearer secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if got.Parsed.Source != protocol.RequestSourceAnthropicMessages || got.Parsed.ModelID != "p/public-alias" || got.Parsed.UpstreamModelID != "public-alias" {
		t.Fatalf("request=%#v", got)
	}
	body := rr.Body.String()
	if !strings.Contains(body, `"model":"p/public-alias"`) || !strings.Contains(body, "event: message_stop\n") || strings.Contains(body, "[DONE]") {
		t.Fatalf("body=%s", body)
	}
	if stream.closed != 1 {
		t.Fatalf("closes=%d", stream.closed)
	}
}

func TestAnthropicStreamPersistsFirstDownstreamAndTTFT(t *testing.T) {
	store := timeline.NewStore(t.TempDir(), 8)
	stream := &trackedAnthropicStream{events: []protocol.Event{{Type: protocol.EventTextDelta, Text: "hi"}, {Type: protocol.EventDone}}}
	h, err := NewHandler(Options{
		DataPlaneToken: "secret",
		Providers:      map[string]Provider{"p": providerFuncAnthropic(func(context.Context, providercontract.DispatchRequest) (EventStream, error) { return stream, nil })},
		Timeline:       store,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"p/m","max_tokens":64,"stream":true,"messages":[{"role":"user","content":"hello"}]}`))
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("X-Request-ID", "req-anthropic-stream")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	got, err := store.Load("req-anthropic-stream")
	if err != nil {
		t.Fatal(err)
	}
	var sawDown, sawTTFT bool
	for _, ev := range got.Events() {
		if ev.Milestone == timeline.MilestoneFirstDownstream && ev.OK {
			sawDown = true
		}
		if ev.Milestone == timeline.MilestoneTTFT && ev.OK {
			sawTTFT = true
		}
	}
	if !sawDown || !sawTTFT {
		t.Fatalf("events=%#v", got.Events())
	}
}

func TestAnthropicNonStreamAndTypedProviderFailure(t *testing.T) {
	for _, tc := range []struct {
		name   string
		events []protocol.Event
		status int
		want   string
	}{
		{"success", []protocol.Event{{Type: protocol.EventTextDelta, Text: "hello"}, {Type: protocol.EventDone}}, 200, `"type":"message"`},
		{"rate-limit", []protocol.Event{{Type: protocol.EventError, Message: "slow down", HTTPStatus: 429, Code: "rate_limit_exceeded"}}, 429, `"type":"rate_limit_error"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stream := &trackedAnthropicStream{events: tc.events}
			h, _ := NewHandler(Options{DataPlaneToken: "secret", Providers: map[string]Provider{"p": providerFuncAnthropic(func(context.Context, providercontract.DispatchRequest) (EventStream, error) { return stream, nil })}})
			h = attachHandlerClose(t, h)
			req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"p/m","max_tokens":64,"messages":[{"role":"user","content":"x"}]}`))
			req.Header.Set("Authorization", "Bearer secret")
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code != tc.status || !strings.Contains(rr.Body.String(), tc.want) {
				t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
			}
			if stream.closed != 1 {
				t.Fatalf("closes=%d", stream.closed)
			}
		})
	}
}

func TestAnthropicHostedWebSearchFailsBeforeProvider(t *testing.T) {
	called := false
	h, _ := NewHandler(Options{DataPlaneToken: "secret", Providers: map[string]Provider{"p": providerFuncAnthropic(func(context.Context, providercontract.DispatchRequest) (EventStream, error) {
		called = true
		return nil, nil
	})}})
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"p/m","max_tokens":64,"messages":[{"role":"user","content":"search"}],"tools":[{"type":"web_search_20250305","name":"web_search"}]}`))
	req.Header.Set("Authorization", "Bearer secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 501 || !strings.Contains(rr.Body.String(), "web_search_unavailable") {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if called {
		t.Fatal("provider called")
	}
}

func TestAnthropicSelectedWebSearchWrapsProviderStream(t *testing.T) {
	sidecarCalled := false
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sidecarCalled = true
		if got := r.Header.Get("Authorization"); got != "Bearer sidecar-key" {
			t.Errorf("sidecar auth=%q", got)
		}
		_, _ = w.Write([]byte(`{"sources":[{"url":"https://example.com","title":"Example"}]}`))
	}))
	t.Cleanup(sidecar.Close)

	opens := 0
	h, err := NewHandler(Options{
		DataPlaneToken: "secret",
		Providers: map[string]Provider{"p": providerFuncAnthropic(func(context.Context, providercontract.DispatchRequest) (EventStream, error) {
			opens++
			if opens == 1 {
				return &trackedAnthropicStream{events: []protocol.Event{
					{Type: protocol.EventToolCallEnd, ID: "ws_1", Name: "web_search", Arguments: `{"query":"benes"}`},
					{Type: protocol.EventDone},
				}}, nil
			}
			return &trackedAnthropicStream{events: []protocol.Event{
				{Type: protocol.EventTextDelta, Text: "answer"},
				{Type: protocol.EventDone},
			}}, nil
		})},
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
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"p/m","max_tokens":64,"messages":[{"role":"user","content":"search"}],"tools":[{"type":"web_search_20250305","name":"web_search"}]}`))
	req.Header.Set("Authorization", "Bearer secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if opens != 2 {
		t.Fatalf("provider opens=%d", opens)
	}
	if !sidecarCalled {
		t.Fatal("web-search sidecar was not executed")
	}
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	content := payload["content"].([]any)
	if content[0].(map[string]any)["type"] != "server_tool_use" {
		t.Fatalf("content=%#v", content)
	}
	result := content[1].(map[string]any)
	if result["type"] != "web_search_tool_result" || result["tool_use_id"] != "ws_1" {
		t.Fatalf("result=%#v", result)
	}
	hits := result["content"].([]any)
	if len(hits) != 1 || hits[0].(map[string]any)["url"] != "https://example.com" {
		t.Fatalf("hits=%#v", hits)
	}
	usage := payload["usage"].(map[string]any)
	if usage["server_tool_use"].(map[string]any)["web_search_requests"] != float64(1) && usage["server_tool_use"].(map[string]any)["web_search_requests"] != 1 {
		t.Fatalf("usage=%#v", usage)
	}
}

func TestAnthropicSelectedWebSearchFailsClosedWithoutCredential(t *testing.T) {
	called := false
	h, _ := NewHandler(Options{
		DataPlaneToken: "secret",
		Providers: map[string]Provider{"p": providerFuncAnthropic(func(context.Context, providercontract.DispatchRequest) (EventStream, error) {
			called = true
			return nil, nil
		})},
		WebSearch: map[string]websearch.Config{
			"p": {
				ProviderID:    "p",
				Endpoint:      "https://api.openai.com/v1/responses",
				AuthClass:     "key",
				Wire:          "openai-responses",
				AllowedModels: []string{"m"},
				Enabled:       true,
			},
		},
	})
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"p/m","max_tokens":64,"messages":[{"role":"user","content":"search"}],"tools":[{"type":"web_search_20250305","name":"web_search"}]}`))
	req.Header.Set("Authorization", "Bearer secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 501 || !strings.Contains(rr.Body.String(), "web_search_unavailable") {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if called {
		t.Fatal("provider called without a resolvable sidecar credential")
	}
}

func TestAnthropicRouteRejectsImplicitUnknownOversizedAndRedactsOpenError(t *testing.T) {
	secret := "SECRET-UPSTREAM"
	provider := providerFuncAnthropic(func(context.Context, providercontract.DispatchRequest) (EventStream, error) {
		return nil, errors.New(secret)
	})
	h, _ := NewHandler(Options{DataPlaneToken: "secret", Providers: map[string]Provider{"p": provider}, MaxRequestBytes: 96})
	h = attachHandlerClose(t, h)
	cases := []struct {
		name, body string
		want       int
		contains   string
	}{
		{"implicit", `{"model":"claude","max_tokens":64,"messages":[{"role":"user","content":"x"}]}`, 501, "migration_not_ready"},
		{"unknown", `{"model":"q/m","max_tokens":64,"messages":[{"role":"user","content":"x"}]}`, 501, "migration_not_ready"},
		{"oversized", `{"model":"p/m","max_tokens":64,"messages":[{"role":"user","content":"` + strings.Repeat("x", 200) + `"}]}`, 413, "request_too_large"},
		{"open-error", `{"model":"p/m","max_tokens":64,"messages":[{"role":"user","content":"x"}]}`, 502, "provider request could not be opened"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(tc.body))
			req.Header.Set("Authorization", "Bearer secret")
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code != tc.want || !strings.Contains(rr.Body.String(), tc.contains) {
				t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
			}
			if strings.Contains(rr.Body.String(), secret) {
				t.Fatalf("secret leaked: %s", rr.Body.String())
			}
		})
	}
}

func TestAnthropicPreflightActivatesMessagesButUnknownRouteStays404(t *testing.T) {
	h, _ := NewHandler(Options{DataPlaneToken: "secret", Providers: map[string]Provider{"p": providerFuncAnthropic(func(context.Context, providercontract.DispatchRequest) (EventStream, error) { return nil, nil })}})
	h = attachHandlerClose(t, h)
	for path, want := range map[string]int{"/v1/messages": 204, "/v1/live": 404} {
		req := httptest.NewRequest(http.MethodOptions, path, nil)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != want {
			t.Fatalf("path=%s status=%d", path, rr.Code)
		}
	}
}

func assertAnthropicError(t *testing.T, rr *httptest.ResponseRecorder, typ, code string) {
	t.Helper()
	var body map[string]any
	if json.Unmarshal(rr.Body.Bytes(), &body) != nil {
		t.Fatalf("body=%s", rr.Body.String())
	}
	if body["type"] != "error" {
		t.Fatalf("body=%#v", body)
	}
	errBody := body["error"].(map[string]any)
	if errBody["type"] != typ {
		t.Fatalf("error=%#v", errBody)
	}
	if code == "" {
		if _, exists := errBody["code"]; exists {
			t.Fatalf("unexpected code in error=%#v", errBody)
		}
	} else if errBody["code"] != code {
		t.Fatalf("error=%#v", errBody)
	}
}

func TestAnthropicInvalidRequestUsesAnthropicErrorEnvelope(t *testing.T) {
	h, _ := NewHandler(Options{DataPlaneToken: "secret", Providers: map[string]Provider{"p": providerFuncAnthropic(func(context.Context, providercontract.DispatchRequest) (EventStream, error) { return nil, nil })}})
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"p/m","messages":[]}`))
	req.Header.Set("Authorization", "Bearer secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	assertAnthropicError(t, rr, "invalid_request_error", "")
	if rr.Header().Get("Content-Type") != "application/json" || rr.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("headers=%v", rr.Header())
	}
}

func TestAnthropicDispatchCarriesForwardAuthorityWithoutLeakingDataPlaneBearer(t *testing.T) {
	var got providercontract.DispatchRequest
	stream := &trackedAnthropicStream{events: []protocol.Event{{Type: protocol.EventDone}}}
	h, _ := NewHandler(Options{DataPlaneToken: "local-secret", Providers: map[string]Provider{
		"p": providerFuncAnthropic(func(_ context.Context, dispatch providercontract.DispatchRequest) (EventStream, error) {
			got = dispatch
			return stream, nil
		}),
	}})
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"p/m","max_tokens":64,"messages":[{"role":"user","content":"x"}]}`))
	req.Header.Set("Authorization", "Bearer local-secret")
	req.Header.Set("X-OAI-Attestation", "attestation-token")
	req.Header.Set("ChatGPT-Account-Id", "acct_123")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if got.Parsed.Source != protocol.RequestSourceAnthropicMessages || got.Parsed.UpstreamModelID != "m" {
		t.Fatalf("dispatch=%#v", got)
	}
	if got.ForwardHeaders.Get("x-oai-attestation") != "attestation-token" || got.ForwardHeaders.Get("chatgpt-account-id") != "acct_123" {
		t.Fatalf("forward headers=%#v", got.ForwardHeaders)
	}
	if got.ForwardHeaders.Get("authorization") != "" || !got.ForwardHeaders.BlockedAuthorization() {
		t.Fatalf("data-plane bearer leaked into forward authority: %#v", got.ForwardHeaders)
	}
}

func TestAnthropicStructuredOutputCapabilityRefusalIsLocalBadRequest(t *testing.T) {
	h, err := NewHandler(Options{
		DataPlaneToken: "secret",
		Providers: map[string]Provider{
			"p": providerFuncAnthropic(func(context.Context, providercontract.DispatchRequest) (EventStream, error) {
				return nil, capability.ErrStructuredOutputUnsupported
			}),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"p/m","max_tokens":64,"messages":[{"role":"user","content":"x"}]}`))
	req.Header.Set("Authorization", "Bearer secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	assertAnthropicError(t, rr, "invalid_request_error", "unsupported_structured_output")
}

