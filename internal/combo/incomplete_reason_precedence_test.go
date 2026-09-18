package combo

import (
	"context"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
)

func TestWalkerUsesStopReasonForIncompleteFailoverClassification(t *testing.T) {
	first := &recordingProvider{events: []protocol.Event{{
		Type:       protocol.EventIncomplete,
		StopReason: "missing_terminal_event",
		Reason:     "content_filter",
	}}}
	second := &recordingProvider{events: []protocol.Event{{Type: protocol.EventTextDelta, Text: "backup"}}}
	walker := &Walker{Targets: []Target{
		{Member: Member{ID: "a", Protocol: "openai-responses"}, Model: "a", Provider: first},
		{Member: Member{ID: "b", Protocol: "openai-responses"}, Model: "b", Provider: second},
	}}

	stream, err := walker.Open(context.Background(), providers.DispatchRequest{})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if first.opens != 1 || second.opens != 1 {
		t.Fatalf("opens first=%d second=%d", first.opens, second.opens)
	}
	attempts := walker.Attempts()
	if len(attempts) != 2 || attempts[0].Code != CodeIncompletePrecommitFailover {
		t.Fatalf("attempts=%#v", attempts)
	}
}
