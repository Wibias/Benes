package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Wibias/Benes/internal/providers/openairesponses"
	"github.com/Wibias/Benes/internal/transport"
)

const testContextOverflowSecret = "should-not-reach-client"

func TestResponsesStreamingProvider413BecomesTerminalContextOverflow(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		_, _ = w.Write([]byte(`{"detail":"request too large; ` + testContextOverflowSecret + `"}`))
	}))
	t.Cleanup(upstream.Close)

	provider, err := openairesponses.NewHardened(context.Background(), openairesponses.Config{
		Endpoint:          upstream.URL,
		APIKey:            "upstream-key",
		DestinationPolicy: transport.DestinationPolicy{AllowPrivateNetwork: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"target": provider},
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"target/gpt-5.6","store":false,"stream":true,"input":"oversized turn"}`))
	req.Header.Set("Authorization", "Bearer local-secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if contentType := rr.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/event-stream") {
		t.Fatalf("content-type=%q", contentType)
	}
	body := rr.Body.String()
	for _, want := range []string{`"type":"response.failed"`, `"type":"invalid_request_error"`, `"code":"context_length_exceeded"`, `"retryable":false`} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %s: %s", want, body)
		}
	}
	if strings.Contains(body, testContextOverflowSecret) {
		t.Fatalf("provider-controlled 413 body leaked: %s", body)
	}
}

func TestResponsesNonStreamingProvider413Keeps413WithCanonicalCode(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		_, _ = w.Write([]byte(`{"detail":"request too large; ` + testContextOverflowSecret + `"}`))
	}))
	t.Cleanup(upstream.Close)

	provider, err := openairesponses.NewHardened(context.Background(), openairesponses.Config{
		Endpoint:          upstream.URL,
		APIKey:            "upstream-key",
		DestinationPolicy: transport.DestinationPolicy{AllowPrivateNetwork: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"target": provider},
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"target/gpt-5.6","store":false,"stream":false,"input":"oversized turn"}`))
	req.Header.Set("Authorization", "Bearer local-secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var payload struct {
		Error struct {
			Type    string `json:"type"`
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Error.Type != "invalid_request_error" || payload.Error.Code != "context_length_exceeded" || strings.TrimSpace(payload.Error.Message) == "" {
		t.Fatalf("payload=%#v", payload)
	}
	if strings.Contains(rr.Body.String(), testContextOverflowSecret) {
		t.Fatalf("provider-controlled 413 body leaked: %s", rr.Body.String())
	}
}

func TestResponsesLocalProviderBodyLimitFailsBeforeNetworkWithCanonicalCode(t *testing.T) {
	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{}}\n\n"))
	}))
	t.Cleanup(upstream.Close)

	provider, err := openairesponses.NewHardened(context.Background(), openairesponses.Config{
		Endpoint:          upstream.URL,
		APIKey:            "upstream-key",
		DestinationPolicy: transport.DestinationPolicy{AllowPrivateNetwork: true},
		TransportOptions:  transport.ClientOptions{MaxRequestBodyBytes: 64},
	})
	if err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{DataPlaneToken: "local-secret", Providers: map[string]Provider{"target": provider}})
	if err != nil {
		t.Fatal(err)
	}

	for _, stream := range []bool{true, false} {
		t.Run(map[bool]string{true: "stream", false: "non_stream"}[stream], func(t *testing.T) {
			body := `{"model":"target/gpt-5.6","store":false,"stream":` + map[bool]string{true: "true", false: "false"}[stream] + `,"input":"this input is deliberately long enough to push the exact compiled upstream body beyond sixty four bytes"}`
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
			req.Header.Set("Authorization", "Bearer local-secret")
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)

			if stream {
				if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"code":"context_length_exceeded"`) || !strings.Contains(rr.Body.String(), `"type":"response.failed"`) {
					t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
				}
			} else {
				if rr.Code != http.StatusRequestEntityTooLarge || !strings.Contains(rr.Body.String(), `"code":"context_length_exceeded"`) {
					t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
				}
			}
		})
	}
	if calls := upstreamCalls.Load(); calls != 0 {
		t.Fatalf("upstream calls=%d, want 0", calls)
	}
}

func TestResponsesProviderNon413OpenFailuresKeepExistingOuterClassification(t *testing.T) {
	for _, status := range []int{
		http.StatusBadRequest,
		http.StatusUnauthorized,
		http.StatusTooManyRequests,
		http.StatusInternalServerError,
	} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"error":{"message":"ordinary upstream failure"}}`))
			}))
			t.Cleanup(upstream.Close)

			provider, err := openairesponses.NewHardened(context.Background(), openairesponses.Config{
				Endpoint:          upstream.URL,
				APIKey:            "upstream-key",
				DestinationPolicy: transport.DestinationPolicy{AllowPrivateNetwork: true},
			})
			if err != nil {
				t.Fatal(err)
			}
			h, err := NewHandler(Options{DataPlaneToken: "local-secret", Providers: map[string]Provider{"target": provider}})
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"target/gpt-5.6","store":false,"stream":true}`))
			req.Header.Set("Authorization", "Bearer local-secret")
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code != http.StatusBadGateway {
				t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
			}
			var payload struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if payload.Error.Code != "upstream_error" {
				t.Fatalf("payload=%#v", payload)
			}
		})
	}
}
