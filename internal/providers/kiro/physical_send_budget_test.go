package kiro

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

func TestKiroRetriesShareTurnPhysicalSendBudget(t *testing.T) {
	hits := 0
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = io.Copy(io.Discard, r.Body)
		_ = r.Body.Close()
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(upstream.Close)

	client, err := NewHardened(context.Background(), Config{
		Account: AccountSnapshot{
			AccessToken: "tok",
			ProfileARN:  "arn:aws:codewhisperer:us-east-1:123456789012:profile/a",
			APIRegion:   "us-east-1",
		},
		HTTPClient: upstream.Client(),
		Endpoint:   "https://runtime.us-east-1.kiro.dev",
	})
	if err != nil {
		t.Fatal(err)
	}
	client.endpoint = upstream.URL

	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxPhysicalSends: 2})
	turn, err := budget.AcquireTurn(context.Background(), "kiro-send-budget")
	if err != nil {
		t.Fatal(err)
	}
	defer turn.Close()

	_, err = client.Open(context.Background(), providers.DispatchRequest{
		Turn: turn,
		Parsed: protocol.ParsedRequest{
			UpstreamModelID: "claude-sonnet-4",
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
