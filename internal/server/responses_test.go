package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	providercontract "github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/providers/openairesponses"
	"github.com/Wibias/Benes/internal/sidecar/websearch"
	"github.com/Wibias/Benes/internal/timeline"
	"github.com/Wibias/Benes/internal/transport"
)

type fakeProvider struct {
	events []protocol.Event
	raw    json.RawMessage
	model  string
}

func (p *fakeProvider) Open(_ context.Context, dispatch providercontract.DispatchRequest) (EventStream, error) {
	request := dispatch.Parsed
	p.raw = append([]byte(nil), request.Raw...)
	p.model = request.UpstreamModelID
	return &sliceStream{events: append([]protocol.Event(nil), p.events...)}, nil
}

type sliceStream struct{ events []protocol.Event }

func (s *sliceStream) Next() (protocol.Event, error) {
	if len(s.events) == 0 {
		return protocol.Event{}, io.EOF
	}
	e := s.events[0]
	s.events = s.events[1:]
	return e, nil
}
func (s *sliceStream) Close() error { return nil }

func TestResponsesRequiresDataPlaneBearer(t *testing.T) {
	provider := &fakeProvider{}
	h, err := NewHandler(Options{DataPlaneToken: "local-secret", Providers: map[string]Provider{"openai-apikey": provider}})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	for _, auth := range []string{"", "Bearer wrong", "Basic local-secret"} {
		req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","store":false}`))
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("auth=%q status=%d", auth, rr.Code)
		}
	}
}

func TestResponsesRoutesExplicitProviderAndStreamsBridgeFrames(t *testing.T) {
	provider := &fakeProvider{events: []protocol.Event{
		{Type: protocol.EventTextDelta, Text: "hello"},
		{Type: protocol.EventDone, Usage: &protocol.Usage{InputTokens: 1, OutputTokens: 1}},
	}}
	h, _ := NewHandler(Options{DataPlaneToken: "local-secret", Providers: map[string]Provider{"openai-apikey": provider}})
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","store":false,"stream":true,"future_field":1}`))
	req.Header.Set("Authorization", "Bearer local-secret")
	req.Header.Set("X-Caller-Secret", "do-not-forward")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Header().Get("X-Benes-Request-Id") == "" {
		t.Fatal("missing request id header")
	}
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content-type=%q", ct)
	}
	if provider.model != "gpt-5.6" {
		t.Fatalf("model=%q", provider.model)
	}
	if !strings.Contains(string(provider.raw), `"future_field":1`) {
		t.Fatalf("raw=%s", provider.raw)
	}
	body := rr.Body.String()
	for _, eventType := range []string{"response.created", "response.output_text.delta", "response.completed"} {
		if !strings.Contains(body, eventType) {
			t.Fatalf("body missing %s: %s", eventType, body)
		}
	}
}

func TestResponsesPersistsTimelineWithoutFailingDelivery(t *testing.T) {
	dir := t.TempDir()
	store := timeline.NewStore(dir, 8)
	provider := &fakeProvider{events: []protocol.Event{
		{Type: protocol.EventTextDelta, Text: "hello"},
		{Type: protocol.EventDone},
	}}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": provider},
		Timeline:       store,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","store":false,"stream":true}`))
	req.Header.Set("Authorization", "Bearer local-secret")
	req.Header.Set("X-Request-ID", "req-persist")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d", rr.Code)
	}
	got, err := store.Load("req-persist")
	if err != nil || len(got.Events()) == 0 {
		t.Fatalf("persisted=%#v err=%v", got, err)
	}
}

func TestResponsesStreamMarksFirstDownstreamAndTTFT(t *testing.T) {
	store := timeline.NewStore(t.TempDir(), 8)
	provider := &fakeProvider{events: []protocol.Event{
		{Type: protocol.EventTextDelta, Text: "hello"},
		{Type: protocol.EventDone},
	}}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": provider},
		Timeline:       store,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","store":false,"stream":true}`))
	req.Header.Set("Authorization", "Bearer local-secret")
	req.Header.Set("X-Request-ID", "req-responses-stream")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	got, err := store.Load("req-responses-stream")
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

func TestResponsesStreamMarksUpstreamAndDownstreamEnd(t *testing.T) {
	store := timeline.NewStore(t.TempDir(), 8)
	provider := &fakeProvider{events: []protocol.Event{
		{Type: protocol.EventTextDelta, Text: "hello"},
		{Type: protocol.EventDone},
	}}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": provider},
		Timeline:       store,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","store":false,"stream":true}`))
	req.Header.Set("Authorization", "Bearer local-secret")
	req.Header.Set("X-Request-ID", "req-responses-end")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d", rr.Code)
	}
	got, err := store.Load("req-responses-end")
	if err != nil {
		t.Fatal(err)
	}
	var sawUp, sawDown bool
	for _, ev := range got.Events() {
		if ev.Milestone == timeline.MilestoneUpstreamEnd && ev.OK {
			sawUp = true
		}
		if ev.Milestone == timeline.MilestoneDownstreamEnd && ev.OK {
			sawDown = true
		}
	}
	if !sawUp || !sawDown {
		t.Fatalf("events=%#v", got.Events())
	}
}

func TestResponsesStreamMarksRelayTransformOnMalformedEvent(t *testing.T) {
	store := timeline.NewStore(t.TempDir(), 8)
	provider := &fakeProvider{events: []protocol.Event{{Type: protocol.EventType("not-a-canonical-event")}}}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": provider},
		Timeline:       store,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","store":false,"stream":true}`))
	req.Header.Set("Authorization", "Bearer local-secret")
	req.Header.Set("X-Request-ID", "req-responses-relay")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	got, err := store.Load("req-responses-relay")
	if err != nil {
		t.Fatal(err)
	}
	var saw bool
	for _, ev := range got.Events() {
		if ev.Stage == timeline.StageRelayTransform && !ev.OK && ev.Cause == "malformed_frame" {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("events=%#v", got.Events())
	}
}

func TestResponsesStreamMarksDownstreamWriteFailure(t *testing.T) {
	store := timeline.NewStore(t.TempDir(), 8)
	provider := &fakeProvider{events: []protocol.Event{
		{Type: protocol.EventTextDelta, Text: "hello"},
		{Type: protocol.EventDone},
	}}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": provider},
		Timeline:       store,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","store":false,"stream":true}`))
	req.Header.Set("Authorization", "Bearer local-secret")
	req.Header.Set("X-Request-ID", "req-responses-write")
	writer := &failingResponseWriter{header: make(http.Header), err: errors.New("write failed")}
	h.ServeHTTP(writer, req)
	got, err := store.Load("req-responses-write")
	if err != nil {
		t.Fatal(err)
	}
	var saw bool
	for _, ev := range got.Events() {
		if ev.Stage == timeline.StageDownstreamWrite && !ev.OK && ev.Cause == "downstream_write" {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("events=%#v", got.Events())
	}
}

func TestResponsesStreamMarksClientCancelAfterFirstFrame(t *testing.T) {
	store := timeline.NewStore(t.TempDir(), 8)
	provider := &fakeProvider{events: []protocol.Event{
		{Type: protocol.EventTextDelta, Text: "hello"},
		{Type: protocol.EventDone},
	}}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": provider},
		Timeline:       store,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","store":false,"stream":true}`))
	req = req.WithContext(ctx)
	req.Header.Set("Authorization", "Bearer local-secret")
	req.Header.Set("X-Request-ID", "req-responses-cancel")
	writer := &cancelAfterWrite{header: make(http.Header), cancel: cancel}
	h.ServeHTTP(writer, req)
	got, err := store.Load("req-responses-cancel")
	if err != nil {
		t.Fatal(err)
	}
	var saw bool
	for _, ev := range got.Events() {
		if ev.Stage == timeline.StageClientCancel && !ev.OK && ev.Cause == "client_cancel" {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("events=%#v", got.Events())
	}
}

type failingResponseWriter struct {
	header http.Header
	err    error
}

func (w *failingResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}
func (w *failingResponseWriter) WriteHeader(int)           {}
func (w *failingResponseWriter) Write([]byte) (int, error) { return 0, w.err }

type cancelAfterWrite struct {
	header http.Header
	cancel context.CancelFunc
}

func (w *cancelAfterWrite) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}
func (w *cancelAfterWrite) WriteHeader(int) {}
func (w *cancelAfterWrite) Write(p []byte) (int, error) {
	if w.cancel != nil {
		w.cancel()
	}
	return len(p), nil
}

func TestResponsesNonStreamingReturnsFinalResponseObject(t *testing.T) {
	provider := &fakeProvider{events: []protocol.Event{{Type: protocol.EventTextDelta, Text: "hello"}, {Type: protocol.EventDone}}}
	h, _ := NewHandler(Options{DataPlaneToken: "local-secret", Providers: map[string]Provider{"openai-apikey": provider}})
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","store":false,"stream":false}`))
	req.Header.Set("Authorization", "Bearer local-secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var response map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response["status"] != "completed" {
		t.Fatalf("response=%#v", response)
	}
}

func TestResponsesSelectedWebSearchWrapsProviderStream(t *testing.T) {
	sidecarCalled := false
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sidecarCalled = true
		_, _ = w.Write([]byte(`{"sources":[{"url":"https://example.com","title":"Example"}]}`))
	}))
	t.Cleanup(sidecar.Close)
	provider := &fakeProvider{events: []protocol.Event{
		{Type: protocol.EventToolCallEnd, ID: "ws_1", Name: "web_search", Arguments: `{"query":"benes"}`},
		{Type: protocol.EventDone},
	}}
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
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","store":false,"stream":false,"tools":[{"type":"web_search"}]}`))
	req.Header.Set("Authorization", "Bearer local-secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !sidecarCalled {
		t.Fatal("web-search sidecar was not executed")
	}
	var response map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	output := response["output"].([]any)
	found := false
	for _, item := range output {
		obj := item.(map[string]any)
		if obj["type"] != "web_search_call" {
			continue
		}
		found = true
		if obj["status"] != "completed" {
			t.Fatalf("item=%#v", obj)
		}
		sources := obj["sources"].([]any)
		if len(sources) != 1 || sources[0].(map[string]any)["url"] != "https://example.com" {
			t.Fatalf("sources=%#v", sources)
		}
	}
	if !found {
		t.Fatalf("output=%#v", output)
	}
}

func TestResponsesSelectedWebSearchFailsClosedWithoutCredential(t *testing.T) {
	called := false
	provider := providerFunc(func(context.Context, providercontract.DispatchRequest) (EventStream, error) {
		called = true
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
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","store":false,"tools":[{"type":"web_search"}]}`))
	req.Header.Set("Authorization", "Bearer local-secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotImplemented {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if called {
		t.Fatal("provider called without a resolvable sidecar credential")
	}
	if !strings.Contains(rr.Body.String(), "web_search_unavailable") {
		t.Fatalf("body=%s", rr.Body.String())
	}
}

func TestResponsesRejectsImplicitOrUnregisteredRoute(t *testing.T) {
	h, _ := NewHandler(Options{DataPlaneToken: "local-secret", Providers: map[string]Provider{"openai-apikey": &fakeProvider{}}})
	h = attachHandlerClose(t, h)
	for _, model := range []string{"gpt-5.6", "unknown/gpt-5.6"} {
		req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(fmt.Sprintf(`{"model":%q,"store":false}`, model)))
		req.Header.Set("Authorization", "Bearer local-secret")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusNotImplemented {
			t.Fatalf("model=%q status=%d body=%s", model, rr.Code, rr.Body.String())
		}
	}
}

func TestResponsesConvertsProviderStreamFailureToTerminalFailedEvent(t *testing.T) {
	provider := providerFunc(func(context.Context, providercontract.DispatchRequest) (EventStream, error) {
		return &errorStream{err: errors.New("parser failed")}, nil
	})
	h, _ := NewHandler(Options{DataPlaneToken: "local-secret", Providers: map[string]Provider{"openai-apikey": provider}})
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","store":false,"stream":true}`))
	req.Header.Set("Authorization", "Bearer local-secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if !strings.Contains(rr.Body.String(), "response.failed") {
		t.Fatalf("body=%s", rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "parser failed") {
		t.Fatalf("internal parser detail leaked: %s", rr.Body.String())
	}
}

type providerFunc func(context.Context, providercontract.DispatchRequest) (EventStream, error)

func (f providerFunc) Open(ctx context.Context, request providercontract.DispatchRequest) (EventStream, error) {
	return f(ctx, request)
}

type errorStream struct{ err error }

func (s *errorStream) Next() (protocol.Event, error) { return protocol.Event{}, s.err }
func (s *errorStream) Close() error                  { return nil }

func TestResponsesEndToEndWithHardenedOpenAIProvider(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer upstream-key" {
			t.Errorf("upstream Authorization=%q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\n")
		fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n")
	}))
	defer upstream.Close()

	provider, err := openairesponses.NewHardened(context.Background(), openairesponses.Config{
		Endpoint:          upstream.URL,
		APIKey:            "upstream-key",
		DestinationPolicy: transport.DestinationPolicy{AllowPrivateNetwork: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{DataPlaneToken: "local-secret", Providers: map[string]Provider{"openai-apikey": provider}})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai-apikey/gpt-4o","store":false,"stream":true,"input":"hi"}`))
	req.Header.Set("Authorization", "Bearer local-secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "response.completed") || !strings.Contains(rr.Body.String(), `"delta":"hello"`) {
		t.Fatalf("body=%s", rr.Body.String())
	}
}
