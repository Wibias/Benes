package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	providercontract "github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/resourcebudget"
)

func TestConcurrentResponsesReturnBudgetToIdle(t *testing.T) {
	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 8, MaxTurnBytes: 1 << 20, MaxProcessBytes: 8 << 20})
	provider := providerFunc(func(_ context.Context, _ providercontract.DispatchRequest) (EventStream, error) {
		return &sliceStream{events: []protocol.Event{
			{Type: protocol.EventTextDelta, Text: "ok"},
			{Type: protocol.EventDone},
		}}, nil
	})
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": provider},
		ResourceBudget: budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","store":false,"stream":false,"input":"hi"}`))
			req.Header.Set("Authorization", "Bearer local-secret")
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code != http.StatusOK {
				t.Errorf("status=%d body=%s", rr.Code, rr.Body.String())
			}
		}()
	}
	wg.Wait()
	if budget.Metrics().ActiveTurns != 0 || budget.Metrics().ProcessBytes != 0 {
		t.Fatalf("metrics=%#v", budget.Metrics())
	}
}

func TestConcurrentChatReturnBudgetToIdle(t *testing.T) {
	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 8, MaxTurnBytes: 1 << 20, MaxProcessBytes: 8 << 20})
	provider := providerFunc(func(_ context.Context, _ providercontract.DispatchRequest) (EventStream, error) {
		return &sliceStream{events: []protocol.Event{
			{Type: protocol.EventTextDelta, Text: "ok"},
			{Type: protocol.EventDone},
		}}, nil
	})
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": provider},
		ResourceBudget: budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","messages":[{"role":"user","content":"hi"}],"stream":false}`))
			req.Header.Set("Authorization", "Bearer local-secret")
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code != http.StatusOK {
				t.Errorf("status=%d body=%s", rr.Code, rr.Body.String())
			}
		}()
	}
	wg.Wait()
	if budget.Metrics().ActiveTurns != 0 || budget.Metrics().ProcessBytes != 0 {
		t.Fatalf("metrics=%#v", budget.Metrics())
	}
}

func TestConcurrentAnthropicReturnBudgetToIdle(t *testing.T) {
	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 8, MaxTurnBytes: 1 << 20, MaxProcessBytes: 8 << 20})
	provider := providerFunc(func(_ context.Context, _ providercontract.DispatchRequest) (EventStream, error) {
		return &sliceStream{events: []protocol.Event{
			{Type: protocol.EventTextDelta, Text: "ok"},
			{Type: protocol.EventDone, StopReason: "end_turn"},
		}}, nil
	})
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": provider},
		ResourceBudget: budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","max_tokens":64,"stream":false,"messages":[{"role":"user","content":"hi"}]}`))
			req.Header.Set("Authorization", "Bearer local-secret")
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code != http.StatusOK {
				t.Errorf("status=%d body=%s", rr.Code, rr.Body.String())
			}
		}()
	}
	wg.Wait()
	if budget.Metrics().ActiveTurns != 0 || budget.Metrics().ProcessBytes != 0 {
		t.Fatalf("metrics=%#v", budget.Metrics())
	}
}

func TestConcurrentChatOverflowFailsClosedAndReturnsToIdle(t *testing.T) {
	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 8, MaxTurnBytes: 16, MaxProcessBytes: 8 << 20})
	provider := providerFunc(func(_ context.Context, _ providercontract.DispatchRequest) (EventStream, error) {
		t.Fatal("provider opened after budget overflow")
		return nil, nil
	})
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": provider},
		ResourceBudget: budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","messages":[{"role":"user","content":"hi"}],"stream":false}`))
			req.Header.Set("Authorization", "Bearer local-secret")
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code != http.StatusServiceUnavailable || !strings.Contains(rr.Body.String(), "resource_exhausted") {
				t.Errorf("status=%d body=%s", rr.Code, rr.Body.String())
			}
		}()
	}
	wg.Wait()
	if budget.Metrics().ActiveTurns != 0 || budget.Metrics().ProcessBytes != 0 {
		t.Fatalf("metrics=%#v", budget.Metrics())
	}
}

func TestConcurrentAnthropicOverflowFailsClosedAndReturnsToIdle(t *testing.T) {
	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 8, MaxTurnBytes: 16, MaxProcessBytes: 8 << 20})
	provider := providerFunc(func(_ context.Context, _ providercontract.DispatchRequest) (EventStream, error) {
		t.Fatal("provider opened after budget overflow")
		return nil, nil
	})
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": provider},
		ResourceBudget: budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","max_tokens":64,"stream":false,"messages":[{"role":"user","content":"hi"}]}`))
			req.Header.Set("Authorization", "Bearer local-secret")
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code != http.StatusServiceUnavailable || !strings.Contains(rr.Body.String(), "resource_exhausted") {
				t.Errorf("status=%d body=%s", rr.Code, rr.Body.String())
			}
		}()
	}
	wg.Wait()
	if budget.Metrics().ActiveTurns != 0 || budget.Metrics().ProcessBytes != 0 {
		t.Fatalf("metrics=%#v", budget.Metrics())
	}
}

func TestConcurrentResponsesOverflowFailsClosedAndReturnsToIdle(t *testing.T) {
	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 8, MaxTurnBytes: 16, MaxProcessBytes: 8 << 20})
	provider := providerFunc(func(_ context.Context, _ providercontract.DispatchRequest) (EventStream, error) {
		t.Fatal("provider opened after budget overflow")
		return nil, nil
	})
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": provider},
		ResourceBudget: budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","store":false,"stream":false,"input":"hi"}`))
			req.Header.Set("Authorization", "Bearer local-secret")
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code != http.StatusServiceUnavailable || !strings.Contains(rr.Body.String(), "resource_exhausted") {
				t.Errorf("status=%d body=%s", rr.Code, rr.Body.String())
			}
		}()
	}
	wg.Wait()
	if budget.Metrics().ActiveTurns != 0 || budget.Metrics().ProcessBytes != 0 {
		t.Fatalf("metrics=%#v", budget.Metrics())
	}
}
