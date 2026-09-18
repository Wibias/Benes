package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wibias/Benes/internal/resourcebudget"
)

func TestResourceMetricsIsLoopbackOnly(t *testing.T) {
	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 1, MaxTurnBytes: 1 << 20})
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		ResourceBudget: budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	blocked := httptest.NewRequest(http.MethodGet, "/api/resource-metrics", nil)
	blockedRR := httptest.NewRecorder()
	h.ServeHTTP(blockedRR, blocked)
	if blockedRR.Code != http.StatusNotFound {
		t.Fatalf("non-loopback status=%d body=%s", blockedRR.Code, blockedRR.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/api/resource-metrics", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["activeTurns"] != float64(0) || got["processBytes"] != float64(0) || got["activeReaders"] != float64(0) {
		t.Fatalf("metrics=%#v", got)
	}
}
