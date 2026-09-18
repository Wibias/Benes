package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/compaction"
	"github.com/Wibias/Benes/internal/protocol"
	providercontract "github.com/Wibias/Benes/internal/providers"
)

type capturingProvider struct {
	fakeProvider
	last providercontract.DispatchRequest
}

func (p *capturingProvider) Open(ctx context.Context, dispatch providercontract.DispatchRequest) (EventStream, error) {
	p.last = dispatch
	return p.fakeProvider.Open(ctx, dispatch)
}

type nativeCompactProvider struct {
	fakeProvider
	body   []byte
	called bool
	opened bool
}

func (p *nativeCompactProvider) SupportsNativeCompact() bool { return true }

func (p *nativeCompactProvider) Open(_ context.Context, dispatch providercontract.DispatchRequest) (EventStream, error) {
	p.opened = true
	return p.fakeProvider.Open(context.Background(), dispatch)
}

func (p *nativeCompactProvider) Compact(_ context.Context, _ providercontract.DispatchRequest, body []byte) (int, http.Header, []byte, error) {
	p.called = true
	p.body = append([]byte(nil), body...)
	header := http.Header{}
	header.Set("Content-Type", "application/json")
	header.Set("Retry-After", "12")
	return http.StatusOK, header, []byte(`{"id":"cmp_native"}`), nil
}

type nativeForwardProvider struct {
	capturingProvider
}

func (p *nativeForwardProvider) NativeCodexForward() bool { return true }

func compactRequest(t *testing.T, h http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, compactPath, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer local-secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func TestCompactRequiresModelAndObjectBody(t *testing.T) {
	h, err := NewHandler(Options{DataPlaneToken: "local-secret", Providers: map[string]Provider{"openai-apikey": &fakeProvider{}}})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	for _, body := range []string{`[]`, `{"input":[]}`, `{"model":""}`} {
		rr := compactRequest(t, h, body)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("body=%s status=%d", body, rr.Code)
		}
	}
}

func TestCompactUnroutableModelIsNotFound(t *testing.T) {
	h, err := NewHandler(Options{DataPlaneToken: "local-secret", Providers: map[string]Provider{"openai-apikey": &fakeProvider{}}})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	rr := compactRequest(t, h, `{"model":"gpt-5.5","input":[]}`)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "invalid_request_error") {
		t.Fatalf("body=%s", rr.Body.String())
	}
}

func TestCompactRoutedTurnInstallsReplacementHistory(t *testing.T) {
	provider := &capturingProvider{fakeProvider: fakeProvider{events: []protocol.Event{
		{Type: protocol.EventTextDelta, Text: "handoff summary"},
		{Type: protocol.EventDone},
	}}}
	h, err := NewHandler(Options{DataPlaneToken: "local-secret", Providers: map[string]Provider{"openai-apikey": provider}})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	rr := compactRequest(t, h, `{"model":"openai-apikey/gpt-5.5","reasoning":{"effort":"high"},"tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}],"input":[{"role":"user","content":"keep going"}]}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var got struct {
		Output []map[string]any `json:"output"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Output) != 2 {
		t.Fatalf("output=%#v", got.Output)
	}
	if got.Output[0]["role"] != "user" {
		t.Fatalf("first=%#v", got.Output[0])
	}
	content, _ := got.Output[1]["content"].([]any)
	if len(content) == 0 {
		t.Fatalf("summary item=%#v", got.Output[1])
	}
	block, _ := content[0].(map[string]any)
	text, _ := block["text"].(string)
	if !strings.Contains(text, "handoff summary") || !strings.Contains(text, compaction.SummaryPrefix) {
		t.Fatalf("summary text=%q", text)
	}
	parsed := provider.last.Parsed
	if !parsed.CompactionRequest {
		t.Fatal("expected compaction request")
	}
	if len(parsed.Context.Tools) != 0 {
		t.Fatalf("tools=%#v", parsed.Context.Tools)
	}
	last := parsed.Context.Messages[len(parsed.Context.Messages)-1]
	if last.Role != protocol.RoleUser || last.Content[0].Text != compaction.CompactPrompt {
		t.Fatalf("last message=%#v", last)
	}
}

func TestCompactIncompleteTurnDoesNotInstallReplacementHistory(t *testing.T) {
	provider := &fakeProvider{events: []protocol.Event{
		{Type: protocol.EventTextDelta, Text: "partial"},
		{Type: protocol.EventDone, StopReason: "pause_turn"},
	}}
	h, err := NewHandler(Options{DataPlaneToken: "local-secret", Providers: map[string]Provider{"openai-apikey": provider}})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	rr := compactRequest(t, h, `{"model":"openai-apikey/gpt-5.5","input":[{"role":"user","content":"keep"}]}`)
	if rr.Code != http.StatusBadGateway {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), `"output"`) && strings.Contains(rr.Body.String(), "Another language model") {
		t.Fatalf("installed replacement history: %s", rr.Body.String())
	}
}

func TestCompactNativeProviderForwardsBody(t *testing.T) {
	provider := &nativeCompactProvider{}
	h, err := NewHandler(Options{DataPlaneToken: "local-secret", Providers: map[string]Provider{"openai": provider}})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	rr := compactRequest(t, h, `{"model":"openai/gpt-5.5","reasoning":{"effort":"high"},"input":[{"role":"user","content":"x"}]}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !provider.called {
		t.Fatal("native compact not called")
	}
	if provider.opened {
		t.Fatal("native compact used routed Open")
	}
	if !strings.Contains(string(provider.body), `"model":"openai/gpt-5.5"`) {
		t.Fatalf("body=%s", provider.body)
	}
	if rr.Header().Get("Retry-After") != "12" {
		t.Fatalf("headers=%v", rr.Header())
	}
	if rr.Body.String() != `{"id":"cmp_native"}` {
		t.Fatalf("body=%s", rr.Body.String())
	}
}

func TestResponsesV2TriggerUsesRoutedSummarizer(t *testing.T) {
	provider := &capturingProvider{fakeProvider: fakeProvider{events: []protocol.Event{
		{Type: protocol.EventTextDelta, Text: "summary-v2"},
		{Type: protocol.EventDone},
	}}}
	h, err := NewHandler(Options{DataPlaneToken: "local-secret", Providers: map[string]Provider{"openai-apikey": provider}})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai-apikey/gpt-5.5","store":false,"stream":false,"tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}],"input":[{"role":"user","content":"keep"},{"type":"compaction_trigger"}]}`))
	req.Header.Set("Authorization", "Bearer local-secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	output, _ := got["output"].([]any)
	if len(output) != 1 {
		t.Fatalf("output=%#v", output)
	}
	item, _ := output[0].(map[string]any)
	if item["type"] != "compaction" {
		t.Fatalf("item=%#v", item)
	}
	encrypted, _ := item["encrypted_content"].(string)
	decoded, ok := compaction.DecodeSummary(encrypted)
	if !ok || decoded != "summary-v2" {
		t.Fatalf("encrypted=%q decoded=%q ok=%v", encrypted, decoded, ok)
	}
	if len(provider.last.Parsed.Context.Tools) != 0 {
		t.Fatalf("tools survived: %#v", provider.last.Parsed.Context.Tools)
	}
}

func TestResponsesV2TriggerOnNativeChatGPTDoesNotRoutedSummarize(t *testing.T) {
	provider := &nativeForwardProvider{capturingProvider: capturingProvider{fakeProvider: fakeProvider{events: []protocol.Event{
		{Type: protocol.EventCompaction, ID: "cmp_native", Data: "native-blob"},
		{Type: protocol.EventDone},
	}}}}
	h, err := NewHandler(Options{DataPlaneToken: "local-secret", Providers: map[string]Provider{"openai": provider}})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai/gpt-5.5","store":false,"stream":false,"input":[{"role":"user","content":"keep"},{"type":"compaction_trigger"}]}`))
	req.Header.Set("Authorization", "Bearer local-secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	last := provider.last.Parsed.Context.Messages
	if len(last) == 0 {
		t.Fatal("missing messages")
	}
	if last[len(last)-1].Content[0].Text == compaction.CompactPrompt {
		t.Fatal("native ChatGPT used routed summarizer prompt")
	}
	var got map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	output, _ := got["output"].([]any)
	item, _ := output[0].(map[string]any)
	if item["type"] != "compaction" || item["encrypted_content"] != "native-blob" {
		t.Fatalf("item=%#v", item)
	}
}
