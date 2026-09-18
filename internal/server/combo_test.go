package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/catalog"
	"github.com/Wibias/Benes/internal/combo"
	"github.com/Wibias/Benes/internal/protocol"
	providercontract "github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/timeline"
)

type capturingOpenProvider struct {
	err    error
	events []protocol.Event
	opens  int
	model  string
}

func (p *capturingOpenProvider) Open(_ context.Context, dispatch providercontract.DispatchRequest) (EventStream, error) {
	p.opens++
	p.model = dispatch.Parsed.UpstreamModelID
	if p.err != nil {
		return nil, p.err
	}
	return &sliceStream{events: append([]protocol.Event(nil), p.events...)}, nil
}

func TestResponsesComboWalksMembersOnTheLiveHandler(t *testing.T) {
	first := &capturingOpenProvider{err: fmt.Errorf("Google GenerateContent returned HTTP 503")}
	second := &capturingOpenProvider{events: []protocol.Event{
		{Type: protocol.EventTextDelta, Text: "hello"},
		{Type: protocol.EventDone},
	}}
	store := timeline.NewStore(t.TempDir(), 8)
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"google": first, "openai-apikey": second},
		Combos: []Combo{{
			ID: "fast",
			Targets: []ComboTarget{
				{ProviderID: "google", Model: "gemini-flash", Protocol: "google"},
				{ProviderID: "openai-apikey", Model: "gpt-5.4", Protocol: "openai-chat"},
			},
		}},
		Timeline: store,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"combo/fast","store":false,"stream":true}`))
	req.Header.Set("Authorization", "Bearer local-secret")
	req.Header.Set("X-Request-ID", "req-combo-walk")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if first.opens != 1 || second.opens != 1 {
		t.Fatalf("opens first=%d second=%d", first.opens, second.opens)
	}
	if first.model != "gemini-flash" || second.model != "gpt-5.4" {
		t.Fatalf("models first=%q second=%q", first.model, second.model)
	}
	if !strings.Contains(rr.Body.String(), "response.output_text.delta") {
		t.Fatalf("body=%s", rr.Body.String())
	}
	got, err := store.Load("req-combo-walk")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ev := range got.Events() {
		if ev.Stage == timeline.StageUpstreamWaitHeaders && ev.Milestone == timeline.MilestoneDispatch && ev.OK && ev.Attempt == 2 {
			found = true
		}
	}
	if !found {
		t.Fatalf("events=%#v", got.Events())
	}
}

func TestResponsesPolicyWalksCandidatesOnTheLiveHandler(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	body := `{"routingProfiles":{"fast":{"candidates":[{"provider":"google","model":"gemini-flash"},{"provider":"openai-apikey","model":"gpt-5.4"}]}}}`
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	first := &capturingOpenProvider{err: fmt.Errorf("Google GenerateContent returned HTTP 503")}
	second := &capturingOpenProvider{events: []protocol.Event{
		{Type: protocol.EventTextDelta, Text: "hello"},
		{Type: protocol.EventDone},
	}}
	h := testHandler(t, Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"google": first, "openai-apikey": second},
		ConfigPath:     configPath,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"policy/fast","store":false,"stream":true}`))
	req.Header.Set("Authorization", "Bearer local-secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if first.opens != 1 || second.opens != 1 {
		t.Fatalf("opens first=%d second=%d", first.opens, second.opens)
	}
	if first.model != "gemini-flash" || second.model != "gpt-5.4" {
		t.Fatalf("models first=%q second=%q", first.model, second.model)
	}
}

func TestResponsesPolicySkipsCandidatesThatFailRequireImage(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	body := `{"routingProfiles":{"fast":{"require":{"imageInput":true},"candidates":[{"provider":"google","model":"gemini-flash"},{"provider":"openai-apikey","model":"gpt-5.4"}]}}}`
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	first := &capturingOpenProvider{err: fmt.Errorf("should not open text-only member")}
	second := &capturingOpenProvider{events: []protocol.Event{
		{Type: protocol.EventTextDelta, Text: "hello"},
		{Type: protocol.EventDone},
	}}
	h := testHandler(t, Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"google": first, "openai-apikey": second},
		ConfigPath:     configPath,
		CatalogModels: []catalog.Model{
			{ID: "google/gemini-flash", Vision: catalog.CapabilityFalse},
			{ID: "openai-apikey/gpt-5.4", Vision: catalog.CapabilityTrue},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"policy/fast","store":false,"stream":true}`))
	req.Header.Set("Authorization", "Bearer local-secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if first.opens != 0 || second.opens != 1 {
		t.Fatalf("opens first=%d second=%d", first.opens, second.opens)
	}
	if second.model != "gpt-5.4" {
		t.Fatalf("model=%q", second.model)
	}
}

func TestResponsesUnknownPolicyFailsClosed(t *testing.T) {
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": &fakeProvider{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"policy/missing","store":false}`))
	req.Header.Set("Authorization", "Bearer local-secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestResponsesPolicyRanksBySeededLatency(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	body := `{"routingProfiles":{"fast":{"optimize":{"latency":1,"health":0,"cost":0,"quota":0},"candidates":[{"provider":"google","model":"gemini-flash"},{"provider":"openai-apikey","model":"gpt-5.4"}]}}}`
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	first := &capturingOpenProvider{events: []protocol.Event{{Type: protocol.EventTextDelta, Text: "hello"}, {Type: protocol.EventDone}}}
	second := &capturingOpenProvider{events: []protocol.Event{{Type: protocol.EventTextDelta, Text: "hello"}, {Type: protocol.EventDone}}}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"google": first, "openai-apikey": second},
		ConfigPath:     configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	inner := h.(*handler)
	inner.policyRuntime.mu.Lock()
	inner.policyRuntime.latency = map[string]float64{"google/gemini-flash": 800, "openai-apikey/gpt-5.4": 80}
	inner.policyRuntime.mu.Unlock()
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"policy/fast","store":false,"stream":true}`))
	req.Header.Set("Authorization", "Bearer local-secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if first.opens != 0 || second.opens != 1 {
		t.Fatalf("opens first=%d second=%d", first.opens, second.opens)
	}
}

func TestResponsesPolicyStickyPinsCommittedMember(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	body := `{"routingProfiles":{"fast":{"optimize":{"latency":1,"health":0,"cost":0,"quota":0},"candidates":[{"provider":"google","model":"gemini-flash"},{"provider":"openai-apikey","model":"gpt-5.4"}]}}}`
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	first := &capturingOpenProvider{events: []protocol.Event{{Type: protocol.EventTextDelta, Text: "hello"}, {Type: protocol.EventDone}}}
	second := &capturingOpenProvider{events: []protocol.Event{{Type: protocol.EventTextDelta, Text: "hello"}, {Type: protocol.EventDone}}}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"google": first, "openai-apikey": second},
		ConfigPath:     configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	inner := h.(*handler)
	inner.policyRuntime.mu.Lock()
	inner.policyRuntime.latency = map[string]float64{"google/gemini-flash": 80, "openai-apikey/gpt-5.4": 800}
	inner.policyRuntime.mu.Unlock()
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"policy/fast","store":false,"stream":true}`))
	req.Header.Set("Authorization", "Bearer local-secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if first.opens != 1 || second.opens != 0 {
		t.Fatalf("first opens first=%d second=%d", first.opens, second.opens)
	}
	respID := policyResponseID(rr.Body.String())
	if respID == "" {
		t.Fatalf("missing response id body=%s", rr.Body.String())
	}
	inner.policyRuntime.mu.Lock()
	inner.policyRuntime.latency = map[string]float64{"google/gemini-flash": 800, "openai-apikey/gpt-5.4": 80}
	inner.policyRuntime.mu.Unlock()
	first.opens = 0
	second.opens = 0
	follow := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"policy/fast","store":false,"stream":true,"previous_response_id":"`+respID+`"}`))
	follow.Header.Set("Authorization", "Bearer local-secret")
	followRR := httptest.NewRecorder()
	h.ServeHTTP(followRR, follow)
	if followRR.Code != http.StatusOK {
		t.Fatalf("follow status=%d body=%s", followRR.Code, followRR.Body.String())
	}
	if first.opens != 1 || second.opens != 0 {
		t.Fatalf("sticky opens first=%d second=%d", first.opens, second.opens)
	}
}

func policyResponseID(body string) string {
	const prefix = `"id":"`
	idx := strings.Index(body, prefix+"resp_")
	if idx < 0 {
		return ""
	}
	rest := body[idx+len(prefix):]
	end := strings.Index(rest, `"`)
	if end < 0 {
		return ""
	}
	return rest[:end]
}

func TestResponsesComboStopsOnInvalidRequestWithoutOpeningLaterMembers(t *testing.T) {
	first := &capturingOpenProvider{err: fmt.Errorf("OpenAI Responses upstream returned HTTP 400")}
	second := &capturingOpenProvider{events: []protocol.Event{{Type: protocol.EventDone}}}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"a": first, "b": second},
		Combos: []Combo{{
			ID: "strict",
			Targets: []ComboTarget{
				{ProviderID: "a", Model: "one", Protocol: "openai-chat"},
				{ProviderID: "b", Model: "two", Protocol: "openai-chat"},
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"combo/strict","store":false}`))
	req.Header.Set("Authorization", "Bearer local-secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadGateway {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if first.opens != 1 || second.opens != 0 {
		t.Fatalf("opens first=%d second=%d", first.opens, second.opens)
	}
}

func TestResponsesUnknownComboFailsClosed(t *testing.T) {
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": &fakeProvider{}},
		Combos:         []Combo{{ID: "fast", Targets: []ComboTarget{{ProviderID: "openai-apikey", Model: "gpt-5.4"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"combo/missing","store":false}`))
	req.Header.Set("Authorization", "Bearer local-secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestResponsesPhysicalComboProviderRemainsWhenNoCombosConfigured(t *testing.T) {
	provider := &fakeProvider{events: []protocol.Event{
		{Type: protocol.EventTextDelta, Text: "ok"},
		{Type: protocol.EventDone},
	}}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"combo": provider},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"combo/physical","store":false,"stream":true}`))
	req.Header.Set("Authorization", "Bearer local-secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || provider.model != "physical" {
		t.Fatalf("status=%d model=%q body=%s", rr.Code, provider.model, rr.Body.String())
	}
}

func TestClassifyOpenErrorReadsProviderHTTPStatus(t *testing.T) {
	got := combo.ClassifyOpenError(fmt.Errorf("Cursor upstream returned HTTP 429"))
	if got.Status != 429 {
		t.Fatalf("got=%#v", got)
	}
}
