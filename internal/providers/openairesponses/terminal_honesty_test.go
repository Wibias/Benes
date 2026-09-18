package openairesponses

import (
	"context"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/protocol"
)

func TestStreamFailedTerminalCarriesCurrentTurnUsage(t *testing.T) {
	upstream := sseServer(t, []string{
		`{"type":"response.output_text.delta","delta":"partial"}`,
		`{"type":"response.failed","response":{"status":"failed","error":{"message":"provider failed"},"usage":{"input_tokens":7,"output_tokens":2,"total_tokens":9,"input_tokens_details":{"cached_tokens":3},"output_tokens_details":{"reasoning_tokens":1}}}}`,
	})
	defer upstream.Close()

	client, err := New(Config{Endpoint: upstream.URL, APIKey: "k", HTTPClient: upstream.Client(), MaxStreamBytes: 1 << 20, InactivityTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := client.Open(context.Background(), canonicalRequest(t, `{"model":"openai-apikey/gpt-5.6","store":false}`, "gpt-5.6"))
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()

	first, err := stream.Next()
	if err != nil || first.Type != protocol.EventTextDelta || first.Text != "partial" {
		t.Fatalf("first=%#v err=%v", first, err)
	}
	failed, err := stream.Next()
	if err != nil {
		t.Fatal(err)
	}
	if failed.Type != protocol.EventError || failed.Usage == nil || failed.Usage.InputTokens != 7 || failed.Usage.OutputTokens != 2 || failed.Usage.TotalTokens != 9 || failed.Usage.CachedInputTokens != 3 || failed.Usage.ReasoningOutputTokens != 1 {
		t.Fatalf("failed=%#v", failed)
	}
}
