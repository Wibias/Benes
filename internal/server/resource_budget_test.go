package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	providercontract "github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/resourcebudget"
)

func TestResponsesAcquiresAndReleasesResourceBudgetTurn(t *testing.T) {
	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 1, MaxTurnBytes: 1 << 20})
	var sawTurn bool
	provider := providerFunc(func(_ context.Context, dispatch providercontract.DispatchRequest) (EventStream, error) {
		if dispatch.Turn == nil {
			t.Fatal("provider opened without a resource-budget turn")
		}
		sawTurn = true
		if budget.Metrics().ActiveTurns != 1 {
			t.Fatalf("active turns during Open = %d", budget.Metrics().ActiveTurns)
		}
		return &sliceStream{}, nil
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
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","input":"hi"}`))
	req.Header.Set("Authorization", "Bearer local-secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if !sawTurn {
		t.Fatal("provider was not opened")
	}
	if budget.Metrics().ActiveTurns != 0 || budget.Metrics().ProcessBytes != 0 {
		t.Fatalf("budget leaked after request: %#v", budget.Metrics())
	}
}

func TestResponsesFailsClosedWhenOutputBudgetExceeded(t *testing.T) {
	budget := resourcebudget.NewManager(resourcebudget.Limits{
		ClassBytes: map[resourcebudget.Class]int64{resourcebudget.ClassOutput: 4},
	})
	provider := providerFunc(func(context.Context, providercontract.DispatchRequest) (EventStream, error) {
		return &sliceStream{events: []protocol.Event{
			{Type: protocol.EventTextDelta, Text: "hello world"},
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
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","input":"hi","stream":false}`))
	req.Header.Set("Authorization", "Bearer local-secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "resource_exhausted") {
		t.Fatalf("body=%s", rr.Body.String())
	}
}

func TestHealthzDoesNotConsumeResourceBudget(t *testing.T) {
	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 0})
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai": &fakeProvider{}},
		ResourceBudget: budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if budget.Metrics().ActiveTurns != 0 {
		t.Fatalf("active turns=%d", budget.Metrics().ActiveTurns)
	}
}
