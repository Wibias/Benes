package openairesponses

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/resourcebudget"
	"github.com/Wibias/Benes/internal/responses/continuation"
)

func TestFabricCredentialPin_OpenAI_NoHopAfterCommitOn429(t *testing.T) {
	var auths []string
	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		auths = append(auths, auth)
		n := hits.Add(1)
		body, _ := io.ReadAll(r.Body)
		hasPrev := strings.Contains(string(body), `"previous_response_id"`) || strings.Contains(string(body), `"previous_response_id":`)
		if n == 1 {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_K1\",\"status\":\"completed\",\"output\":[]}}\n\n")
			return
		}
		if hasPrev && strings.Contains(auth, "sk-k1") {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"error":{"message":"rate limited"}}`)
			return
		}
		t.Fatalf("unexpected hop auth=%s n=%d body=%s", auth, n, body)
	}))
	t.Cleanup(upstream.Close)

	store := continuation.NewStore(continuation.StoreLimits{TTL: time.Hour}, time.Now)
	authority, err := continuation.NewAuthority(store, nil, bytes32('o'))
	if err != nil {
		t.Fatal(err)
	}
	client, err := New(Config{
		Endpoint:     upstream.URL,
		APIKey:       "sk-k1",
		APIKeyPool:   []APIKeySlot{{ID: "K1", Key: "sk-k1"}, {ID: "K2", Key: "sk-k2"}},
		HTTPClient:   upstream.Client(),
		Continuation: authority,
		MaxStreamBytes: 1 << 20,
		InactivityTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}

	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 4, MaxTurnBytes: 8 << 20, MaxProcessBytes: 32 << 20,
		ClassBytes: map[resourcebudget.Class]int64{resourcebudget.ClassContinuation: 4 << 20, resourcebudget.ClassTranslator: 4 << 20}})
	turn, err := budget.AcquireTurn(context.Background(), "t1")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = turn.Close() })

	stream, err := client.Open(context.Background(), providers.DispatchRequest{
		Turn: turn,
		Parsed: protocol.ParsedRequest{
			Source:  protocol.RequestSourceResponses,
			ModelID: "gpt-test", UpstreamModelID: "gpt-test",
			Context: protocol.Context{Messages: []protocol.Message{{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}}}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var native string
	for {
		ev, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if ev.Type == protocol.EventDone || ev.Type == protocol.EventIncomplete {
			if raw, ok := ev.ProviderState["openai_responses_previous"]; ok {
				var body struct {
					ID string `json:"id"`
				}
				_ = json.Unmarshal(raw, &body)
				native = body.ID
			}
		}
	}
	_ = stream.Close()
	pin := stream.(interface{ PhysicalOwnership() *providers.PhysicalPin }).PhysicalOwnership()
	if pin == nil || pin.CredentialRef == "" {
		t.Fatalf("missing physical pin: %#v", pin)
	}
	if native == "" {
		// Some streams use completed path; force remember via Authority Peek after persistence.
		native = "resp_K1"
	}

	turn2, err := budget.AcquireTurn(context.Background(), "t2")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = turn2.Close() })
	_, err = client.Open(context.Background(), providers.DispatchRequest{
		Turn:            turn2,
		PreferCommitted: true,
		PhysicalPin:     pin,
		Parsed: protocol.ParsedRequest{
			Source:             protocol.RequestSourceResponses,
			ModelID:            "gpt-test", UpstreamModelID: "gpt-test",
			PreviousResponseID: "resp_K1",
			Context: protocol.Context{Messages: []protocol.Message{{
				Role: protocol.RoleToolResult, ToolCallID: "c1", ToolName: "x",
				Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "out"}},
			}}},
		},
	})
	if err == nil {
		t.Fatal("expected fail-closed on K1 429 without hopping to K2")
	}
	for _, auth := range auths {
		if strings.Contains(auth, "sk-k2") {
			t.Fatalf("K2 attempted with K1-owned state: %#v", auths)
		}
	}
}

func TestFabricCredentialPin_OpenAI_PrimaryOnK2BindsAuthorityToK2(t *testing.T) {
	var auths []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auths = append(auths, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_K2\",\"status\":\"completed\",\"output\":[]}}\n\n")
	}))
	t.Cleanup(upstream.Close)

	store := continuation.NewStore(continuation.StoreLimits{TTL: time.Hour}, time.Now)
	authority, err := continuation.NewAuthority(store, nil, bytes32('p'))
	if err != nil {
		t.Fatal(err)
	}
	client, err := New(Config{
		Endpoint:   upstream.URL,
		APIKey:     "sk-default",
		APIKeyPool: []APIKeySlot{{ID: "K2", Key: "sk-k2"}, {ID: "K1", Key: "sk-k1"}},
		HTTPClient: upstream.Client(),
		Continuation: authority,
		MaxStreamBytes: 1 << 20,
		InactivityTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 4, MaxTurnBytes: 8 << 20, MaxProcessBytes: 32 << 20,
		ClassBytes: map[resourcebudget.Class]int64{resourcebudget.ClassContinuation: 4 << 20, resourcebudget.ClassTranslator: 4 << 20}})
	turn, err := budget.AcquireTurn(context.Background(), "t1")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = turn.Close() })
	stream, err := client.Open(context.Background(), providers.DispatchRequest{
		Turn: turn,
		Parsed: protocol.ParsedRequest{
			Source:  protocol.RequestSourceResponses,
			ModelID: "gpt-test", UpstreamModelID: "gpt-test",
			Context: protocol.Context{Messages: []protocol.Message{{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}}}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for {
		_, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	pin := stream.(interface{ PhysicalOwnership() *providers.PhysicalPin }).PhysicalOwnership()
	_ = stream.Close()
	if pin == nil || pin.CredentialRef != "K2" {
		t.Fatalf("pin=%#v want K2 (pool primary)", pin)
	}
	if len(auths) == 0 || !strings.Contains(auths[0], "sk-k2") {
		t.Fatalf("primary auth=%#v", auths)
	}

	turn2, err := budget.AcquireTurn(context.Background(), "t2")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = turn2.Close() })
	stream2, err := client.Open(context.Background(), providers.DispatchRequest{
		Turn:            turn2,
		PreferCommitted: true,
		PhysicalPin:     pin,
		Parsed: protocol.ParsedRequest{
			Source:             protocol.RequestSourceResponses,
			ModelID:            "gpt-test", UpstreamModelID: "gpt-test",
			PreviousResponseID: "resp_K2",
			Context:            protocol.Context{Messages: []protocol.Message{{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "next"}}}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = stream2.Close()
	if len(auths) < 2 || !strings.Contains(auths[1], "sk-k2") {
		t.Fatalf("continuation must stay on K2: %#v", auths)
	}
}
