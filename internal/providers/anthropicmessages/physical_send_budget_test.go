package anthropicmessages

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/resourcebudget"
	"github.com/Wibias/Benes/internal/transport"
)

func TestAnthropicTransientRetriesShareTurnPhysicalSendBudget(t *testing.T) {
	hits := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = io.Copy(io.Discard, r.Body)
		_ = r.Body.Close()
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(upstream.Close)

	client, err := New(Config{
		Endpoint:   upstream.URL + "/v1/messages",
		APIKey:     "key",
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
	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxPhysicalSends: 2})
	turn, err := budget.AcquireTurn(context.Background(), "anthropic-send-budget")
	if err != nil {
		t.Fatal(err)
	}
	defer turn.Close()

	_, err = client.Open(context.Background(), providers.DispatchRequest{
		Turn:   turn,
		Parsed: minimalRequest("claude-test"),
	})
	if !errors.Is(err, resourcebudget.ErrPhysicalSendBudgetExceeded) {
		t.Fatalf("err=%v", err)
	}
	if hits != 2 {
		t.Fatalf("physical hits=%d want=2", hits)
	}
}
