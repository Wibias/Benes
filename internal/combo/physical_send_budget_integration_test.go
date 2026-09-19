package combo_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/combo"
	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/providers/openairesponses"
	"github.com/Wibias/Benes/internal/resourcebudget"
	"github.com/Wibias/Benes/internal/transport"
)

func TestComboTransientRetriesShareOnePhysicalSendCeiling(t *testing.T) {
	var sends atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		_ = r.Body.Close()
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, `{"error":"temporary"}`)
	}))
	t.Cleanup(upstream.Close)

	newMember := func(id string) combo.Target {
		client, err := openairesponses.New(openairesponses.Config{
			Endpoint:       upstream.URL,
			APIKey:         "key-" + id,
			ProviderID:     "provider-" + id,
			HTTPClient:     upstream.Client(),
			MaxStreamBytes: 1 << 20,
			Transient5xx: transport.Transient5xxPolicy{
				Enabled:  true,
				Attempts: 4,
				Sleep:    func(context.Context, time.Duration) error { return nil },
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		return combo.Target{
			Member:   combo.Member{ID: id, Protocol: "openai-responses"},
			Model:    "gpt-test",
			Provider: client,
		}
	}

	budget := resourcebudget.NewManager(resourcebudget.Limits{})
	turn, err := budget.AcquireTurn(context.Background(), "send-budget")
	if err != nil {
		t.Fatal(err)
	}
	defer turn.Close()

	walker := &combo.Walker{Targets: []combo.Target{
		newMember("a"),
		newMember("b"),
		newMember("c"),
	}}

	_, err = walker.Open(context.Background(), providers.DispatchRequest{
		Turn: turn,
		Parsed: protocol.ParsedRequest{
			Source:          protocol.RequestSourceResponses,
			ModelID:         "combo/send-budget",
			UpstreamModelID: "gpt-test",
			Raw:             []byte(`{"model":"gpt-test","input":"hello","stream":true}`),
			Context: protocol.Context{Messages: []protocol.Message{{
				Role: protocol.RoleUser,
				Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hello"}},
			}}},
			Stream: true,
		},
	})
	if err == nil {
		t.Fatal("expected exhausted request to fail")
	}
	if got := sends.Load(); got != 8 {
		t.Fatalf("physical sends=%d want=8; retry layers multiplied past one request ceiling (err=%v)", got, err)
	}
	if !strings.Contains(err.Error(), "physical send budget") {
		t.Fatalf("err=%q want physical send budget exhaustion", err)
	}
}

func TestComboCredentialAndTransientRetriesShareOnePhysicalSendCeiling(t *testing.T) {
	var sends atomic.Int32
	perAuth := map[string]int{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		auth := r.Header.Get("Authorization")
		perAuth[auth]++
		_, _ = io.Copy(io.Discard, r.Body)
		_ = r.Body.Close()

		switch auth {
		case "Bearer key-a":
			if perAuth[auth] <= 2 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			w.WriteHeader(http.StatusTooManyRequests)
		case "Bearer key-b":
			w.WriteHeader(http.StatusServiceUnavailable)
		case "Bearer key-c":
			w.WriteHeader(http.StatusServiceUnavailable)
		default:
			t.Fatalf("unexpected authorization %q", auth)
		}
	}))
	t.Cleanup(upstream.Close)

	memberA, err := openairesponses.New(openairesponses.Config{
		Endpoint: upstream.URL,
		APIKeyPool: []openairesponses.APIKeySlot{
			{ID: "a", Key: "key-a"},
			{ID: "b", Key: "key-b"},
		},
		ProviderID: "provider-a",
		HTTPClient: upstream.Client(),
		Transient5xx: transport.Transient5xxPolicy{
			Enabled:  true,
			Attempts: 4,
			Sleep:    func(context.Context, time.Duration) error { return nil },
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	memberB, err := openairesponses.New(openairesponses.Config{
		Endpoint:   upstream.URL,
		APIKey:     "key-c",
		ProviderID: "provider-b",
		HTTPClient: upstream.Client(),
		Transient5xx: transport.Transient5xxPolicy{
			Enabled:  true,
			Attempts: 4,
			Sleep:    func(context.Context, time.Duration) error { return nil },
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxPhysicalSends: 8})
	turn, err := budget.AcquireTurn(context.Background(), "nested-send-budget")
	if err != nil {
		t.Fatal(err)
	}
	defer turn.Close()

	walker := &combo.Walker{Targets: []combo.Target{
		{Member: combo.Member{ID: "a", Protocol: "openai-responses"}, Model: "gpt-test", Provider: memberA},
		{Member: combo.Member{ID: "b", Protocol: "openai-responses"}, Model: "gpt-test", Provider: memberB},
	}}
	_, err = walker.Open(context.Background(), providers.DispatchRequest{
		Turn: turn,
		Parsed: protocol.ParsedRequest{
			Source:          protocol.RequestSourceResponses,
			ModelID:         "combo/nested",
			UpstreamModelID: "gpt-test",
			Raw:             []byte(`{"model":"gpt-test","input":"hello","stream":true}`),
			Context: protocol.Context{Messages: []protocol.Message{{
				Role: protocol.RoleUser,
				Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hello"}},
			}}},
			Stream: true,
		},
	})
	if !errors.Is(err, resourcebudget.ErrPhysicalSendBudgetExceeded) {
		t.Fatalf("err=%v", err)
	}
	if got := sends.Load(); got != 8 {
		t.Fatalf("physical sends=%d want=8", got)
	}
	if perAuth["Bearer key-a"] != 3 || perAuth["Bearer key-b"] != 4 || perAuth["Bearer key-c"] != 1 {
		t.Fatalf("per-auth sends=%#v", perAuth)
	}
	records := turn.PhysicalSends()
	if len(records) != 8 {
		t.Fatalf("records=%#v", records)
	}
	for i, record := range records {
		if record.Ordinal != i+1 {
			t.Fatalf("record[%d]=%#v", i, record)
		}
	}
}

