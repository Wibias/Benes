package google

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/resourcebudget"
)

func TestGoogleRetriesShareTurnPhysicalSendBudget(t *testing.T) {
	hits := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = io.Copy(io.Discard, r.Body)
		_ = r.Body.Close()
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(upstream.Close)

	client, err := New(context.Background(), Config{
		APIKey:     "gk-test",
		HTTPClient: upstream.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	client.testOrigin = upstream.URL

	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxPhysicalSends: 2})
	turn, err := budget.AcquireTurn(context.Background(), "google-send-budget")
	if err != nil {
		t.Fatal(err)
	}
	defer turn.Close()

	_, err = client.Open(context.Background(), providers.DispatchRequest{
		Turn: turn,
		Parsed: protocol.ParsedRequest{
			ModelID: "gemini-3.7-flash",
			Context: protocol.Context{Messages: []protocol.Message{{
				Role: protocol.RoleUser,
				Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hello"}},
			}}},
		},
	})
	if !errors.Is(err, resourcebudget.ErrPhysicalSendBudgetExceeded) {
		t.Fatalf("err=%v", err)
	}
	if hits != 2 {
		t.Fatalf("physical hits=%d want=2", hits)
	}
}
