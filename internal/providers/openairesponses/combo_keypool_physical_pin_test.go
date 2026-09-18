package openairesponses

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/combo"
	"github.com/Wibias/Benes/internal/credentialpool"
	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/resourcebudget"
	"github.com/Wibias/Benes/internal/responses/continuation"
)

type failingPrecommitProvider struct{ opens atomic.Int32 }

func (p *failingPrecommitProvider) Open(_ context.Context, _ providers.DispatchRequest) (providers.EventStream, error) {
	p.opens.Add(1)
	return nil, &providers.OpenError{StatusCode: 503, Message: "member A precommit unavailable HTTP 503"}
}

// TestFabricCombo_OpenAIKeyPoolPhysicalPinThroughWalker proves exact-key ownership
// survives combo.prefixedStream: member B/K1 produces resp_K1; after unpinned Select
// would choose K2, OpenCommitted continuation with PhysicalPin stays on K1 and
// previous_response_id never reaches K2.
func TestFabricCombo_OpenAIKeyPoolPhysicalPinThroughWalker(t *testing.T) {
	var mu sync.Mutex
	var auths []string
	var bodies []string
	var hits atomic.Int32
	var allowCounterfactual atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		auths = append(auths, auth)
		bodies = append(bodies, string(body))
		mu.Unlock()
		n := hits.Add(1)
		hasPrev := strings.Contains(string(body), "resp_K1")
		switch {
		case n == 1:
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_K1\",\"status\":\"completed\",\"output\":[{\"type\":\"function_call\",\"id\":\"fc_1\",\"call_id\":\"c1\",\"name\":\"benes_fabric_delegate\",\"arguments\":\"{}\"}]}}\n\n")
		case !hasPrev && strings.Contains(auth, "sk-k1"):
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"error":{"message":"rate limited"}}`)
		case !hasPrev && strings.Contains(auth, "sk-k2"):
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_poison\",\"status\":\"completed\",\"output\":[]}}\n\n")
		case hasPrev && strings.Contains(auth, "sk-k1"):
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_cont\",\"status\":\"completed\",\"output\":[]}}\n\n")
		case hasPrev && strings.Contains(auth, "sk-k2"):
			if !allowCounterfactual.Load() {
				t.Errorf("K2 received resp_K1 previous_response_id before counterfactual phase")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = io.WriteString(w, `{"error":{"message":"wrong key"}}`)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_cf\",\"status\":\"completed\",\"output\":[]}}\n\n")
		default:
			t.Fatalf("unexpected n=%d auth=%s hasPrev=%v", n, auth, hasPrev)
		}
	}))
	t.Cleanup(upstream.Close)

	store := continuation.NewStore(continuation.StoreLimits{TTL: time.Hour}, time.Now)
	authority, err := continuation.NewAuthority(store, nil, bytes32('c'))
	if err != nil {
		t.Fatal(err)
	}
	a := &failingPrecommitProvider{}
	b, err := New(Config{
		Endpoint:          upstream.URL,
		APIKey:            "sk-k1",
		APIKeyPool:        []APIKeySlot{{ID: "K1", Key: "sk-k1"}, {ID: "K2", Key: "sk-k2"}},
		HTTPClient:        upstream.Client(),
		Continuation:      authority,
		MaxStreamBytes:    1 << 20,
		InactivityTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}

	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 8, MaxTurnBytes: 8 << 20, MaxProcessBytes: 32 << 20,
		ClassBytes: map[resourcebudget.Class]int64{resourcebudget.ClassContinuation: 4 << 20, resourcebudget.ClassTranslator: 4 << 20}})
	turn, err := budget.AcquireTurn(context.Background(), "primary")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = turn.Close() })

	walker := &combo.Walker{Targets: []combo.Target{
		{Member: combo.Member{ID: "A", Protocol: "openai-responses"}, Model: "m-a", Provider: a},
		{Member: combo.Member{ID: "B", Protocol: "openai-responses"}, Model: "m-b", Provider: b},
	}}

	stream, err := walker.Open(context.Background(), providers.DispatchRequest{
		Turn: turn,
		Parsed: protocol.ParsedRequest{
			Source: protocol.RequestSourceResponses, ModelID: "combo/o", UpstreamModelID: "o",
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
	reporter, ok := stream.(interface{ PhysicalOwnership() *providers.PhysicalPin })
	if !ok {
		_ = stream.Close()
		t.Fatalf("combo stream %T hides PhysicalOwnership - Fabric cannot capture K1 pin", stream)
	}
	pin := reporter.PhysicalOwnership()
	_ = stream.Close()
	if pin == nil || pin.CredentialRef != "K1" {
		t.Fatalf("primary pin=%#v want K1", pin)
	}
	if walker.CommittedID() != "B" {
		t.Fatalf("committed=%q want B", walker.CommittedID())
	}

	turnPoison, err := budget.AcquireTurn(context.Background(), "poison")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = turnPoison.Close() })
	poison, err := b.Open(context.Background(), providers.DispatchRequest{
		Turn: turnPoison,
		Parsed: protocol.ParsedRequest{
			Source: protocol.RequestSourceResponses, ModelID: "gpt-test", UpstreamModelID: "gpt-test",
			Context: protocol.Context{Messages: []protocol.Message{{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "poison"}}}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for {
		_, err := poison.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	_ = poison.Close()

	if b.keyPool != nil && b.keyPool.pool != nil {
		ref, _ := b.keyPool.selectKey()
		if ref != "K2" {
			b.keyPool.pool.ReportFailure("K1", credentialpool.Failure{Class: credentialpool.FailureRateLimited})
			ref, _ = b.keyPool.selectKey()
		}
		if ref != "K2" {
			t.Fatalf("after alter, unpinned Select ref=%q want K2", ref)
		}
	}

	turnCont, err := budget.AcquireTurn(context.Background(), "cont")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = turnCont.Close() })
	cont, err := walker.OpenCommitted(context.Background(), providers.DispatchRequest{
		Turn:            turnCont,
		PreferCommitted: true,
		PhysicalPin:     pin,
		Parsed: protocol.ParsedRequest{
			Source:             protocol.RequestSourceResponses,
			ModelID:            "combo/o",
			UpstreamModelID:    "o",
			PreviousResponseID: "resp_K1",
			Context: protocol.Context{Messages: []protocol.Message{{
				Role: protocol.RoleToolResult, ToolCallID: "c1", ToolName: "benes_fabric_delegate",
				Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "child-out"}},
			}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for {
		_, err := cont.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	_ = cont.Close()

	mu.Lock()
	var k2WithPrev bool
	var k1Cont bool
	for i, auth := range auths {
		hasPrev := strings.Contains(bodies[i], "resp_K1")
		if strings.Contains(auth, "sk-k2") && hasPrev {
			k2WithPrev = true
		}
		if strings.Contains(auth, "sk-k1") && hasPrev {
			k1Cont = true
		}
	}
	snap := append([]string(nil), auths...)
	mu.Unlock()
	if k2WithPrev {
		t.Fatalf("K2 received resp_K1 on Fabric continuation: %#v", snap)
	}
	if !k1Cont {
		t.Fatalf("continuation did not use K1 with resp_K1: %#v", snap)
	}

	allowCounterfactual.Store(true)
	turnCF, err := budget.AcquireTurn(context.Background(), "cf")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = turnCF.Close() })
	before := hits.Load()
	cf, err := b.Open(context.Background(), providers.DispatchRequest{
		Turn: turnCF,
		Parsed: protocol.ParsedRequest{
			Source:             protocol.RequestSourceResponses,
			ModelID:            "gpt-test",
			UpstreamModelID:    "gpt-test",
			PreviousResponseID: "resp_K1",
			Context: protocol.Context{Messages: []protocol.Message{{
				Role: protocol.RoleToolResult, ToolCallID: "c1", ToolName: "benes_fabric_delegate",
				Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "child-out"}},
			}}},
		},
	})
	if err != nil {
		// Fail-closed without pin is acceptable; prove Select still prefers K2.
		if b.keyPool != nil {
			ref, _ := b.keyPool.selectKey()
			if ref != "K2" {
				t.Fatalf("counterfactual Select ref=%q want K2 (open err=%v)", ref, err)
			}
		}
		return
	}
	for {
		_, err := cf.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	_ = cf.Close()
	if hits.Load() <= before {
		t.Fatal("counterfactual made no upstream request")
	}
	mu.Lock()
	last := auths[len(auths)-1]
	mu.Unlock()
	if !strings.Contains(last, "sk-k2") {
		t.Fatalf("without PhysicalPin expected K2; last=%s auths=%#v", last, auths)
	}
}
