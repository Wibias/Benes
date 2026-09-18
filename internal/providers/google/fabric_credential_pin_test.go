package google

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
)

func TestFabricCredentialPin_Google_NoHopAfterCommitOn429(t *testing.T) {
	var keys []string
	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("x-goog-api-key")
		keys = append(keys, key)
		n := hits.Add(1)
		body, _ := io.ReadAll(r.Body)
		hasSig := strings.Contains(string(body), "thoughtSignature") || strings.Contains(string(body), "sig-K1")
		if n == 1 {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"functionCall\":{\"id\":\"c1\",\"name\":\"lookup\",\"args\":{\"q\":\"x\"}},\"thoughtSignature\":\"sig-K1\"}]},\"finishReason\":\"STOP\"}]}\n\n")
			return
		}
		if hasSig && key == "gk-k1" {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"error":{"code":429,"message":"rate limit"}}`)
			return
		}
		t.Fatalf("unexpected hop key=%s n=%d", key, n)
	}))
	t.Cleanup(upstream.Close)

	client, err := New(context.Background(), Config{
		APIKey:     "gk-k1",
		Keys:       []KeySlot{{ID: "K1", Key: "gk-k1"}, {ID: "K2", Key: "gk-k2"}},
		HTTPClient: upstream.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	client.testOrigin = upstream.URL

	stream, err := client.Open(context.Background(), providers.DispatchRequest{
		Parsed: protocol.ParsedRequest{
			Source: protocol.RequestSourceResponses, ModelID: "gemini", UpstreamModelID: "gemini-2.0-flash",
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
	if pin == nil || pin.CredentialRef == "" {
		t.Fatalf("missing pin %#v", pin)
	}

	_, err = client.Open(context.Background(), providers.DispatchRequest{
		PreferCommitted: true,
		PhysicalPin:     pin,
		Parsed: protocol.ParsedRequest{
			Source: protocol.RequestSourceResponses, ModelID: "gemini", UpstreamModelID: "gemini-2.0-flash",
			Context: protocol.Context{Messages: []protocol.Message{
				{Role: protocol.RoleAssistant, Content: []protocol.ContentPart{{
					Type: protocol.ContentToolCall, ToolCallID: "c1", ToolName: "lookup",
					ThoughtSignature: "sig-K1",
					ProviderMetadata: &protocol.ProviderOpaqueMetadata{Google: &protocol.GoogleOpaqueMetadata{ThoughtSignature: "sig-K1"}},
				}}},
				{Role: protocol.RoleToolResult, ToolCallID: "c1", ToolName: "lookup", Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "out"}}},
			}},
		},
	})
	if err == nil {
		t.Fatal("expected fail-closed on K1 429")
	}
	for _, key := range keys {
		if key == "gk-k2" {
			t.Fatalf("K2 attempted with K1-owned ThoughtSignature: %#v", keys)
		}
	}
}

func TestFabricCredentialPin_Google_PrimaryOnK2StaysOnK2(t *testing.T) {
	var keys []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		keys = append(keys, r.Header.Get("x-goog-api-key"))
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"ok\"}]},\"finishReason\":\"STOP\"}]}\n\n")
	}))
	t.Cleanup(upstream.Close)

	client, err := New(context.Background(), Config{
		APIKey:     "gk-default",
		Keys:       []KeySlot{{ID: "K2", Key: "gk-k2"}, {ID: "K1", Key: "gk-k1"}},
		HTTPClient: upstream.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	client.testOrigin = upstream.URL
	stream, err := client.Open(context.Background(), providers.DispatchRequest{
		Parsed: protocol.ParsedRequest{
			Source: protocol.RequestSourceResponses, ModelID: "gemini", UpstreamModelID: "gemini-2.0-flash",
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
		t.Fatalf("pin=%#v", pin)
	}
	_, err = client.Open(context.Background(), providers.DispatchRequest{
		PreferCommitted: true,
		PhysicalPin:     pin,
		Parsed: protocol.ParsedRequest{
			Source: protocol.RequestSourceResponses, ModelID: "gemini", UpstreamModelID: "gemini-2.0-flash",
			Context: protocol.Context{Messages: []protocol.Message{{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "next"}}}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) < 2 || keys[0] != "gk-k2" || keys[1] != "gk-k2" {
		t.Fatalf("keys=%#v", keys)
	}
}
