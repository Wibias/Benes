package openairesponses

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/resourcebudget"
)

func TestAPIKeyPoolSelectsAndFailsOverThroughCredentialPool(t *testing.T) {
	var keys []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		keys = append(keys, r.Header.Get("Authorization"))
		if len(keys) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n")
	}))
	defer upstream.Close()

	client, err := New(Config{
		Endpoint: upstream.URL,
		APIKeyPool: []APIKeySlot{
			{ID: "a", Key: "key-a"},
			{ID: "b", Key: "key-b"},
		},
		HTTPClient:        upstream.Client(),
		MaxStreamBytes:    1 << 20,
		InactivityTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := client.Open(context.Background(), canonicalRequest(t, `{"model":"openai-apikey/gpt-5.6","input":"hi"}`, "gpt-5.6"))
	if err != nil {
		t.Fatal(err)
	}
	drainStream(t, stream)
	if len(keys) != 2 || keys[0] == keys[1] {
		t.Fatalf("keys=%v", keys)
	}
}

func TestOpenReservesTranslatorAndStreamBudgetClasses(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n")
	}))
	defer upstream.Close()
	client, err := New(Config{Endpoint: upstream.URL, APIKey: "k", HTTPClient: upstream.Client(), MaxStreamBytes: 1 << 20, InactivityTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 1, MaxTurnBytes: 1 << 20, MaxProcessBytes: 1 << 20})
	turn, err := budget.AcquireTurn(context.Background(), "t")
	if err != nil {
		t.Fatal(err)
	}
	defer turn.Close()
	dispatch := canonicalRequest(t, `{"model":"openai-apikey/gpt-5.6","input":"hi"}`, "gpt-5.6")
	dispatch.Turn = turn
	stream, err := client.Open(context.Background(), dispatch)
	if err != nil {
		t.Fatal(err)
	}
	drainStream(t, stream)
	metrics := budget.Metrics()
	if metrics.Bytes[resourcebudget.ClassTranslator] == 0 || metrics.Bytes[resourcebudget.ClassStreamPending] == 0 {
		t.Fatalf("missing class reservations: %#v", metrics.Bytes)
	}
}
