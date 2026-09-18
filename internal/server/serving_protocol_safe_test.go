package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	providercontract "github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/resourcebudget"
)

func TestServingProtocolFromNonComparableProviderDoesNotPanic(t *testing.T) {
	if got := servingProtocolFromProvider(providerFunc(func(context.Context, providercontract.DispatchRequest) (EventStream, error) {
		return &sliceStream{}, nil
	})); got != "" {
		t.Fatalf("protocol=%q", got)
	}
}

func TestResponsesOpensProviderWhenServingProtocolLookupHasNoReporter(t *testing.T) {
	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 1, MaxTurnBytes: 1 << 20})
	var sawTurn bool
	provider := providerFunc(func(_ context.Context, dispatch providercontract.DispatchRequest) (EventStream, error) {
		sawTurn = true
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
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !sawTurn {
		t.Fatal("provider was not opened")
	}
}
