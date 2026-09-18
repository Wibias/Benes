package antigravity

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

func TestAntigravityPeerFailoverSharesTurnPhysicalSendBudget(t *testing.T) {
	hits := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = io.Copy(io.Discard, r.Body)
		_ = r.Body.Close()
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(upstream.Close)

	client := newTestClient(t, upstream, []Account{{ID: "acct", Token: "tok", ProjectID: "proj"}}, false)
	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxPhysicalSends: 1})
	turn, err := budget.AcquireTurn(context.Background(), "antigravity-send-budget")
	if err != nil {
		t.Fatal(err)
	}
	defer turn.Close()

	_, err = client.Open(context.Background(), providers.DispatchRequest{
		Turn: turn,
		Parsed: protocol.ParsedRequest{UpstreamModelID: "gemini-3.7-flash"},
	})
	if !errors.Is(err, resourcebudget.ErrPhysicalSendBudgetExceeded) {
		t.Fatalf("err=%v", err)
	}
	if hits != 1 {
		t.Fatalf("physical hits=%d want=1", hits)
	}
}
