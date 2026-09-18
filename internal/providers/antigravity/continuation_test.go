package antigravity

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/resourcebudget"
	"github.com/Wibias/Benes/internal/responses/continuation"
	"github.com/Wibias/Benes/internal/transport"
)

func antigravityTestAuthority(t *testing.T) *continuation.Authority {
	t.Helper()
	store := continuation.NewStore(continuation.StoreLimits{
		MaxEntryBytes: 1 << 20, MaxTotalBytes: 4 << 20, MaxEntries: 32, TTL: time.Hour,
	}, time.Now)
	salt := bytes32('a')
	authority, err := continuation.NewAuthority(store, nil, salt)
	if err != nil {
		t.Fatal(err)
	}
	return authority
}

func bytes32(fill byte) []byte {
	salt := make([]byte, 32)
	for i := range salt {
		salt[i] = fill
	}
	return salt
}

func antigravityTestTurn(t *testing.T) *resourcebudget.Turn {
	t.Helper()
	budget := resourcebudget.NewManager(resourcebudget.Limits{
		MaxActiveTurns: 4, MaxProcessBytes: 4 << 20, MaxTurnBytes: 4 << 20,
		ClassBytes: map[resourcebudget.Class]int64{resourcebudget.ClassContinuation: 4 << 20},
	})
	turn, err := budget.AcquireTurn(context.Background(), "thread")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = turn.Close() })
	return turn
}

func TestThoughtSignatureDoesNotReplayAcrossAntigravityAccounts(t *testing.T) {
	authority := antigravityTestAuthority(t)
	turn := antigravityTestTurn(t)
	var bodies []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(raw))
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, ccaOK)
	}))
	t.Cleanup(upstream.Close)

	owner, err := NewHardened(context.Background(), Config{
		Endpoint:          DailyAPI,
		Accounts:          []Account{{ID: "acct-a", Token: "tok-a", ProjectID: "proj-a"}},
		CatalogModels:     []string{"gemini-3.7-flash"},
		HTTPClient:        &http.Client{Transport: rewriteHost{base: upstream.URL, next: http.DefaultTransport}},
		DestinationPolicy: transport.DestinationPolicy{AllowPrivateNetwork: true},
		Continuation:      authority,
	})
	if err != nil {
		t.Fatal(err)
	}
	bound, err := owner.bindContinuation(providers.DispatchRequest{
		Parsed: protocol.ParsedRequest{PreviousResponseID: "thread-1", UpstreamModelID: "gemini-3.7-flash"},
		Turn:   turn,
	}, Account{ID: "acct-a", Token: "tok-a"}, "gemini-3.7-flash", DailyAPI)
	if err != nil {
		t.Fatal(err)
	}
	if err := authority.RememberSignature(bound.Owner, bound.Durable, "thread-1", "c1", "sig-a"); err != nil {
		t.Fatal(err)
	}
	bound.Release()

	req := providers.DispatchRequest{
		Turn: turn,
		Parsed: protocol.ParsedRequest{
			UpstreamModelID:    "gemini-3.7-flash",
			PreviousResponseID: "thread-1",
			Context: protocol.Context{Messages: []protocol.Message{
				{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}}},
				{Role: protocol.RoleAssistant, Content: []protocol.ContentPart{{
					Type: protocol.ContentToolCall, ToolCallID: "c1", ToolName: "lookup", Arguments: map[string]any{"q": "x"},
					ThoughtSignature: "client-forged",
				}}},
				{Role: protocol.RoleToolResult, ToolCallID: "c1", ToolName: "lookup", Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "out"}}},
			}},
		},
	}
	stream, err := owner.Open(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	stream.Close()
	if len(bodies) != 1 || !strings.Contains(bodies[0], `"thoughtSignature":"sig-a"`) {
		t.Fatalf("owned signature missing: %v", bodies)
	}

	foreign, err := NewHardened(context.Background(), Config{
		Endpoint:          DailyAPI,
		Accounts:          []Account{{ID: "acct-b", Token: "tok-b", ProjectID: "proj-b"}},
		CatalogModels:     []string{"gemini-3.7-flash"},
		HTTPClient:        &http.Client{Transport: rewriteHost{base: upstream.URL, next: http.DefaultTransport}},
		DestinationPolicy: transport.DestinationPolicy{AllowPrivateNetwork: true},
		Continuation:      authority,
	})
	if err != nil {
		t.Fatal(err)
	}
	stream, err = foreign.Open(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	stream.Close()
	if len(bodies) != 2 || strings.Contains(bodies[1], "sig-a") || strings.Contains(bodies[1], "client-forged") {
		t.Fatalf("foreign account replayed signature: %v", bodies)
	}
}

func TestAntigravityPersistsThoughtSignatureFromFunctionCall(t *testing.T) {
	authority := antigravityTestAuthority(t)
	turn := antigravityTestTurn(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"functionCall\":{\"id\":\"c9\",\"name\":\"lookup\",\"args\":{\"q\":\"x\"}},\"thoughtSignature\":\"sig-live\"}]}}]}\n\n")
	}))
	t.Cleanup(upstream.Close)
	client, err := NewHardened(context.Background(), Config{
		Endpoint:          DailyAPI,
		Accounts:          []Account{{ID: "acct-a", Token: "tok-a", ProjectID: "proj-a"}},
		CatalogModels:     []string{"gemini-3.7-flash"},
		HTTPClient:        &http.Client{Transport: rewriteHost{base: upstream.URL, next: http.DefaultTransport}},
		DestinationPolicy: transport.DestinationPolicy{AllowPrivateNetwork: true},
		Continuation:      authority,
	})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := client.Open(context.Background(), providers.DispatchRequest{
		Turn: turn,
		Parsed: protocol.ParsedRequest{
			UpstreamModelID:    "gemini-3.7-flash",
			PreviousResponseID: "thread-live",
			Context: protocol.Context{Messages: []protocol.Message{{
				Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}},
			}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ev, err := stream.Next()
	if err != nil || ev.Type != protocol.EventToolCallStart || ev.ID != "c9" {
		t.Fatalf("event=%#v err=%v", ev, err)
	}
	stream.Close()
	bound, err := client.bindContinuation(providers.DispatchRequest{
		Parsed: protocol.ParsedRequest{PreviousResponseID: "thread-live", UpstreamModelID: "gemini-3.7-flash"},
		Turn:   turn,
	}, Account{ID: "acct-a", Token: "tok-rotated"}, "gemini-3.7-flash", DailyAPI)
	if err != nil {
		t.Fatal(err)
	}
	defer bound.Release()
	got, ok := authority.LookupSignature(bound.Owner, "thread-live", "c9", turn)
	if !ok || got != "sig-live" {
		t.Fatalf("persisted=%q ok=%v", got, ok)
	}
}
