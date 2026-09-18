package google_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Wibias/Benes/internal/combo"
	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/providers/google"
)

type failingPrecommitProvider struct{ opens atomic.Int32 }

func (p *failingPrecommitProvider) Open(_ context.Context, _ providers.DispatchRequest) (providers.EventStream, error) {
	p.opens.Add(1)
	return nil, &providers.OpenError{StatusCode: 503, Message: "member A precommit unavailable HTTP 503"}
}

// TestFabricCombo_GoogleKeyPoolPhysicalPinThroughWalker proves exact-key ownership
// survives combo.prefixedStream into OpenCommitted (Fabric continuation path):
// combo commits member B; K1 serves primary + ThoughtSignature; after pool poison so
// unpinned Select returns K2, pinned continuation still uses K1; K2 never sees
// K1-owned signature on the Fabric continuation request.
func TestFabricCombo_GoogleKeyPoolPhysicalPinThroughWalker(t *testing.T) {
	var mu sync.Mutex
	var keys []string
	var bodies []string
	var hits atomic.Int32
	var allowCounterfactual atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("x-goog-api-key")
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		keys = append(keys, key)
		bodies = append(bodies, string(body))
		mu.Unlock()
		n := hits.Add(1)
		hasSig := strings.Contains(string(body), "sig-K1")
		switch {
		case n == 1:
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"functionCall\":{\"id\":\"c1\",\"name\":\"benes_fabric_delegate\",\"args\":{\"instruction\":\"x\"}},\"thoughtSignature\":\"sig-K1\"}]},\"finishReason\":\"STOP\"}]}\n\n")
		case !hasSig && key == "gk-k1":
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"error":{"code":429,"message":"rate limit","status":"RESOURCE_EXHAUSTED"}}`)
		case !hasSig && key == "gk-k2":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"poison-ok\"}]},\"finishReason\":\"STOP\"}]}\n\n")
		case hasSig && key == "gk-k1":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"cont-ok\"}]},\"finishReason\":\"STOP\"}]}\n\n")
		case hasSig && key == "gk-k2":
			if !allowCounterfactual.Load() {
				t.Errorf("K2 received K1-owned ThoughtSignature before counterfactual phase")
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"counterfactual-k2\"}]},\"finishReason\":\"STOP\"}]}\n\n")
		default:
			t.Fatalf("unexpected request n=%d key=%s hasSig=%v", n, key, hasSig)
		}
	}))
	t.Cleanup(upstream.Close)

	a := &failingPrecommitProvider{}
	b, err := google.New(context.Background(), google.Config{
		APIKey:     "gk-k1",
		Keys:       []google.KeySlot{{ID: "K1", Key: "gk-k1"}, {ID: "K2", Key: "gk-k2"}},
		HTTPClient: upstream.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	google.SetTestOrigin(b, upstream.URL)

	walker := &combo.Walker{Targets: []combo.Target{
		{Member: combo.Member{ID: "A", Protocol: "google"}, Model: "gem-a", Provider: a},
		{Member: combo.Member{ID: "B", Protocol: "google"}, Model: "gem-b", Provider: b},
	}}

	stream, err := walker.Open(context.Background(), providers.DispatchRequest{
		Parsed: protocol.ParsedRequest{
			Source: protocol.RequestSourceResponses, ModelID: "combo/g", UpstreamModelID: "g",
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
	// Same capture shape as runModelTurn / authoritative_turn.go.
	reporter, ok := stream.(interface{ PhysicalOwnership() *providers.PhysicalPin })
	if !ok {
		_ = stream.Close()
		t.Fatalf("combo stream %T hides PhysicalOwnership - Fabric cannot capture K1 pin", stream)
	}
	pin := reporter.PhysicalOwnership()
	_ = stream.Close()
	if pin == nil || pin.CredentialRef != "K1" {
		t.Fatalf("primary pin=%#v want K1 (runModelTurn would attach nil/wrong PhysicalPin)", pin)
	}
	if a.opens.Load() != 1 {
		t.Fatalf("member A opens=%d want 1", a.opens.Load())
	}
	if walker.CommittedID() != "B" {
		t.Fatalf("committed=%q want B", walker.CommittedID())
	}

	// Poison unpinned selection toward K2 (fresh Select would not stay on K1).
	poison, err := b.Open(context.Background(), providers.DispatchRequest{
		Parsed: protocol.ParsedRequest{
			Source: protocol.RequestSourceResponses, ModelID: "gemini", UpstreamModelID: "gemini-2.0-flash",
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

	ref, _ := google.TestingSelectKey(b)
	if ref != "K2" {
		google.TestingReportKeyRateLimited(b, "K1")
		ref, _ = google.TestingSelectKey(b)
	}
	if ref != "K2" {
		t.Fatalf("after alter, unpinned Select ref=%q want K2 (counterfactual)", ref)
	}

	cont, err := walker.OpenCommitted(context.Background(), providers.DispatchRequest{
		PreferCommitted: true,
		PhysicalPin:     pin,
		Parsed: protocol.ParsedRequest{
			Source: protocol.RequestSourceResponses, ModelID: "combo/g", UpstreamModelID: "g",
			Context: protocol.Context{Messages: []protocol.Message{
				{Role: protocol.RoleAssistant, Content: []protocol.ContentPart{{
					Type: protocol.ContentToolCall, ToolCallID: "c1", ToolName: "benes_fabric_delegate",
					ThoughtSignature: "sig-K1",
					ProviderMetadata: &protocol.ProviderOpaqueMetadata{Google: &protocol.GoogleOpaqueMetadata{ThoughtSignature: "sig-K1"}},
				}}},
				{Role: protocol.RoleToolResult, ToolCallID: "c1", ToolName: "benes_fabric_delegate", Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "child-out"}}},
			}},
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
	var k2WithSigBeforeCF bool
	var k1Cont bool
	for i, key := range keys {
		hasSig := strings.Contains(bodies[i], "sig-K1")
		if key == "gk-k2" && hasSig {
			k2WithSigBeforeCF = true
		}
		if key == "gk-k1" && hasSig && i > 0 {
			k1Cont = true
		}
	}
	snap := append([]string(nil), keys...)
	mu.Unlock()
	if k2WithSigBeforeCF {
		t.Fatalf("K2 received K1-owned ThoughtSignature on Fabric continuation: keys=%#v", snap)
	}
	if !k1Cont {
		t.Fatalf("continuation did not hit K1 with signature: keys=%#v", snap)
	}

	// Counterfactual: without PhysicalPin, unpinned select chooses K2 for the same signature payload.
	allowCounterfactual.Store(true)
	before := hits.Load()
	lost, err := b.Open(context.Background(), providers.DispatchRequest{
		Parsed: protocol.ParsedRequest{
			Source: protocol.RequestSourceResponses, ModelID: "gemini", UpstreamModelID: "gemini-2.0-flash",
			Context: protocol.Context{Messages: []protocol.Message{
				{Role: protocol.RoleAssistant, Content: []protocol.ContentPart{{
					Type: protocol.ContentToolCall, ToolCallID: "c1", ToolName: "benes_fabric_delegate",
					ThoughtSignature: "sig-K1",
					ProviderMetadata: &protocol.ProviderOpaqueMetadata{Google: &protocol.GoogleOpaqueMetadata{ThoughtSignature: "sig-K1"}},
				}}},
				{Role: protocol.RoleToolResult, ToolCallID: "c1", ToolName: "benes_fabric_delegate", Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "child-out"}}},
			}},
		},
	})
	if err != nil {
		t.Fatalf("counterfactual open: %v", err)
	}
	for {
		_, err := lost.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	_ = lost.Close()
	if hits.Load() <= before {
		t.Fatal("counterfactual made no upstream request")
	}
	mu.Lock()
	last := keys[len(keys)-1]
	mu.Unlock()
	if last != "gk-k2" {
		t.Fatalf("without PhysicalPin expected K2; last=%s keys=%#v", last, keys)
	}
}
