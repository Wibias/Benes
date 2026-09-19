package openaichat

import (
	"context"
	"net/http"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
)

func TestChatClientUsesCallerUserAgentCompatibilityFallback(t *testing.T) {
	trip := &captureRoundTrip{}
	client, err := New(Config{
		Endpoint:   "https://compat.example/v1/chat/completions",
		APIKey:     "key",
		HTTPClient: &http.Client{Transport: trip},
	})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := client.Open(context.Background(), providers.DispatchRequest{
		Parsed: protocol.ParsedRequest{
			UpstreamModelID: "model",
			Context: protocol.Context{Messages: []protocol.Message{{
				Role: protocol.RoleUser,
				Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}},
			}}},
		},
		ForwardHeaders: providers.NewForwardHeaders(map[string]string{
			"user-agent": "codex-cli/9.9 compatibility-marker",
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = stream.Close()
	if len(trip.requests) != 1 {
		t.Fatalf("requests=%d", len(trip.requests))
	}
	if got := trip.requests[0].Header.Get("User-Agent"); got != "codex-cli/9.9 compatibility-marker" {
		t.Fatalf("user-agent=%q", got)
	}
}
