package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestStopAPIIsLoopbackOnlyAndInvokesStop(t *testing.T) {
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	inner, ok := h.(*handler)
	if !ok {
		t.Fatal("handler type")
	}
	stopped := make(chan struct{})
	inner.SetStop(func() { close(stopped) })

	blocked := httptest.NewRequest(http.MethodPost, "/api/stop", nil)
	blocked.Header.Set("Authorization", "Bearer local-secret")
	blockedRR := httptest.NewRecorder()
	h.ServeHTTP(blockedRR, blocked)
	if blockedRR.Code != http.StatusNotFound {
		t.Fatalf("data-plane status=%d body=%s", blockedRR.Code, blockedRR.Body.String())
	}

	req := httptest.NewRequest(http.MethodPost, "/api/stop", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("stop was not invoked")
	}
}
