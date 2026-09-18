package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Wibias/Benes/internal/providers/openairesponses"
	"github.com/Wibias/Benes/internal/transport"
)

func TestResponsesGlobalRequestLimitCannotBeRaisedByLargerProviderLimit(t *testing.T) {
	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{}}\n\n"))
	}))
	t.Cleanup(upstream.Close)

	provider, err := openairesponses.NewHardened(context.Background(), openairesponses.Config{
		Endpoint:          upstream.URL + "/v1/responses",
		APIKey:            "upstream-key",
		DestinationPolicy: transport.DestinationPolicy{AllowPrivateNetwork: true},
		TransportOptions: transport.ClientOptions{
			MaxRequestBodyBytes:  1 << 20,
			RequestBodyLimitPath: "/v1/responses",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken:  "local-secret",
		Providers:       map[string]Provider{"target": provider},
		MaxRequestBytes: 64,
	})
	if err != nil {
		t.Fatal(err)
	}
	body := `{"model":"target/gpt-5.6","store":false,"input":"this inbound request is intentionally longer than the global sixty four byte request ceiling"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer local-secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if calls := upstreamCalls.Load(); calls != 0 {
		t.Fatalf("upstream calls=%d, want 0", calls)
	}
}
