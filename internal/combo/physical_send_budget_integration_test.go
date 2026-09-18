package combo_test

import (
	"context"
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
		t.Fatalf("physical sends=%d want=8; retry layers multiplied past one request ceiling", got)
	}
	if !strings.Contains(err.Error(), "physical send budget") {
		t.Fatalf("err=%q want physical send budget exhaustion", err)
	}
}
