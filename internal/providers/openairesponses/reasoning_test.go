package openairesponses

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/protocol"
)

func TestValidateMigratedRequestAllowsStatelessReasoningConfig(t *testing.T) {
	request := canonicalRequest(t, `{"model":"openai-apikey/gpt-5","store":false,"reasoning":{"effort":"high","summary":"auto"}}`, "gpt-5")
	if err := ValidateMigratedRequest(request.Parsed); err != nil {
		t.Fatalf("ValidateMigratedRequest(): %v", err)
	}
}

func TestValidateMigratedRequestRejectsEncryptedReasoningInclude(t *testing.T) {
	request := canonicalRequest(t, `{"model":"openai-apikey/gpt-5","store":false,"reasoning":{"summary":"auto"},"include":["reasoning.encrypted_content"]}`, "gpt-5")
	if err := ValidateMigratedRequest(request.Parsed); !errors.Is(err, ErrUnsupportedRequestShape) {
		t.Fatalf("err=%v", err)
	}
}

func TestStreamMapsReasoningSummaryAndRawReasoning(t *testing.T) {
	upstream := sseServer(t, []string{
		`{"type":"response.output_item.added","item":{"type":"reasoning","id":"rs_1","summary":[]}}`,
		`{"type":"response.reasoning_summary_part.added","item_id":"rs_1","output_index":0,"summary_index":0,"part":{"type":"summary_text","text":""}}`,
		`{"type":"response.reasoning_summary_text.delta","item_id":"rs_1","output_index":0,"summary_index":0,"delta":"plan "}`,
		`{"type":"response.reasoning_summary_text.delta","item_id":"rs_1","output_index":0,"summary_index":0,"delta":"carefully"}`,
		`{"type":"response.reasoning_summary_text.done","item_id":"rs_1","output_index":0,"summary_index":0,"text":"plan carefully"}`,
		`{"type":"response.reasoning_summary_part.done","item_id":"rs_1","output_index":0,"summary_index":0,"part":{"type":"summary_text","text":"plan carefully"}}`,
		`{"type":"response.reasoning_text.delta","item_id":"rs_1","output_index":0,"content_index":0,"delta":"raw"}`,
		`{"type":"response.reasoning_text.done","item_id":"rs_1","output_index":0,"content_index":0,"text":"raw"}`,
		`{"type":"response.output_item.done","output_index":0,"item":{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"plan carefully"}],"content":[{"type":"reasoning_text","text":"raw"}]}}`,
		`{"type":"response.completed","response":{"usage":{"input_tokens":2,"output_tokens":3,"output_tokens_details":{"reasoning_tokens":2},"total_tokens":5}}}`,
	})
	defer upstream.Close()
	client, err := New(Config{Endpoint: upstream.URL, APIKey: "k", HTTPClient: upstream.Client(), MaxStreamBytes: 1 << 20, InactivityTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := client.Open(context.Background(), canonicalRequest(t, `{"model":"openai-apikey/gpt-5","store":false,"reasoning":{"summary":"auto"}}`, "gpt-5"))
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()

	var got []protocol.Event
	for {
		event, err := stream.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Next(): %v", err)
		}
		got = append(got, event)
	}
	want := []protocol.EventType{
		protocol.EventThinkingDelta,
		protocol.EventThinkingDelta,
		protocol.EventReasoningRawDelta,
		protocol.EventDone,
	}
	if len(got) != len(want) {
		t.Fatalf("events=%#v", got)
	}
	for index := range want {
		if got[index].Type != want[index] {
			t.Fatalf("event[%d]=%s want=%s", index, got[index].Type, want[index])
		}
	}
	if got[0].Thinking != "plan " || got[1].Thinking != "carefully" || got[2].Text != "raw" {
		t.Fatalf("events=%#v", got)
	}
	if got[3].Usage == nil || got[3].Usage.ReasoningOutputTokens != 2 {
		t.Fatalf("usage=%#v", got[3].Usage)
	}
}

func TestStreamFailsClosedOnEncryptedReasoningOutput(t *testing.T) {
	upstream := sseServer(t, []string{
		`{"type":"response.output_item.added","item":{"type":"reasoning","id":"rs_1","summary":[],"encrypted_content":"opaque"}}`,
	})
	defer upstream.Close()
	client, err := New(Config{Endpoint: upstream.URL, APIKey: "k", HTTPClient: upstream.Client(), MaxStreamBytes: 1 << 20, InactivityTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := client.Open(context.Background(), canonicalRequest(t, `{"model":"openai-apikey/gpt-5","store":false}`, "gpt-5"))
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if _, err := stream.Next(); !errors.Is(err, ErrUnsupportedUpstreamEvent) {
		t.Fatalf("err=%v", err)
	}
}
