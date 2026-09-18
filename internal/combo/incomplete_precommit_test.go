package combo

import (
	"context"
	"errors"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
)

func TestWalkerHopsOnlyRetryableZeroOutputIncompleteReasons(t *testing.T) {
	for _, reason := range []string{"adapter_eof", "missing_terminal_event", "upstream_stall_timeout"} {
		t.Run(reason, func(t *testing.T) {
			first := &recordingProvider{events: []protocol.Event{{Type: protocol.EventIncomplete, Reason: reason}}}
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
			if first.closed != 1 {
				t.Fatalf("retryable incomplete stream was not closed before hop: closed=%d", first.closed)
			}
			event, err := stream.Next()
			if err != nil || event.Type != protocol.EventTextDelta || event.Text != "backup" {
				t.Fatalf("event=%#v err=%v", event, err)
			}
			attempts := walker.Attempts()
			if len(attempts) != 2 {
				t.Fatalf("attempts=%#v", attempts)
			}
			if attempts[0].Status != 502 || attempts[0].Code != CodeIncompletePrecommitFailover || attempts[0].Decision != DecisionHop {
				t.Fatalf("first attempt=%#v", attempts[0])
			}
			if attempts[1].Decision != DecisionCommitted {
				t.Fatalf("second attempt=%#v", attempts[1])
			}
		})
	}
}

func TestWalkerDoesNotHopZeroOutputSemanticIncomplete(t *testing.T) {
	for _, reason := range []string{"max_output_tokens", "content_filter", "", "upstream_stall"} {
		t.Run(reason, func(t *testing.T) {
			first := &recordingProvider{events: []protocol.Event{{Type: protocol.EventIncomplete, Reason: reason}}}
			second := &recordingProvider{events: []protocol.Event{{Type: protocol.EventTextDelta, Text: "must-not-run"}}}
			walker := &Walker{Targets: []Target{
				{Member: Member{ID: "a", Protocol: "openai-responses"}, Model: "a", Provider: first},
				{Member: Member{ID: "b", Protocol: "openai-responses"}, Model: "b", Provider: second},
			}}

			stream, err := walker.Open(context.Background(), providers.DispatchRequest{})
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			if first.opens != 1 || second.opens != 0 {
				t.Fatalf("opens first=%d second=%d", first.opens, second.opens)
			}
			event, err := stream.Next()
			if err != nil || event.Type != protocol.EventIncomplete || event.Reason != reason {
				t.Fatalf("event=%#v err=%v", event, err)
			}
			attempts := walker.Attempts()
			if len(attempts) != 1 || attempts[0].Decision != DecisionCommitted {
				t.Fatalf("attempts=%#v", attempts)
			}
		})
	}
}

func TestWalkerDoesNotHopRetryableIncompleteAfterTextCommit(t *testing.T) {
	first := &recordingProvider{events: []protocol.Event{
		{Type: protocol.EventTextDelta, Text: "visible"},
		{Type: protocol.EventIncomplete, Reason: "adapter_eof"},
	}}
	second := &recordingProvider{events: []protocol.Event{{Type: protocol.EventTextDelta, Text: "must-not-run"}}}
	walker := &Walker{Targets: []Target{
		{Member: Member{ID: "a", Protocol: "openai-responses"}, Model: "a", Provider: first},
		{Member: Member{ID: "b", Protocol: "openai-responses"}, Model: "b", Provider: second},
	}}

	stream, err := walker.Open(context.Background(), providers.DispatchRequest{})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if second.opens != 0 {
		t.Fatalf("second opens=%d", second.opens)
	}
	firstEvent, err := stream.Next()
	if err != nil || firstEvent.Type != protocol.EventTextDelta || firstEvent.Text != "visible" {
		t.Fatalf("first event=%#v err=%v", firstEvent, err)
	}
	terminal, err := stream.Next()
	if err != nil || terminal.Type != protocol.EventIncomplete || terminal.Reason != "adapter_eof" {
		t.Fatalf("terminal=%#v err=%v", terminal, err)
	}
}

func TestWalkerDoesNotHopRetryableIncompleteAfterToolCommit(t *testing.T) {
	first := &recordingProvider{events: []protocol.Event{
		{Type: protocol.EventToolCallStart, ID: "call-1", Name: "exec"},
		{Type: protocol.EventIncomplete, Reason: "adapter_eof"},
	}}
	second := &recordingProvider{events: []protocol.Event{{Type: protocol.EventTextDelta, Text: "must-not-run"}}}
	walker := &Walker{Targets: []Target{
		{Member: Member{ID: "a", Protocol: "openai-responses"}, Model: "a", Provider: first},
		{Member: Member{ID: "b", Protocol: "openai-responses"}, Model: "b", Provider: second},
	}}

	stream, err := walker.Open(context.Background(), providers.DispatchRequest{})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if second.opens != 0 {
		t.Fatalf("second opens=%d", second.opens)
	}
	firstEvent, err := stream.Next()
	if err != nil || firstEvent.Type != protocol.EventToolCallStart || firstEvent.ID != "call-1" {
		t.Fatalf("first event=%#v err=%v", firstEvent, err)
	}
	terminal, err := stream.Next()
	if err != nil || terminal.Type != protocol.EventIncomplete || terminal.Reason != "adapter_eof" {
		t.Fatalf("terminal=%#v err=%v", terminal, err)
	}
}

func TestWalkerRetryableIncompleteExhaustionIsFinite(t *testing.T) {
	first := &recordingProvider{events: []protocol.Event{{Type: protocol.EventIncomplete, Reason: "adapter_eof"}}}
	second := &recordingProvider{events: []protocol.Event{{Type: protocol.EventIncomplete, Reason: "missing_terminal_event"}}}
	walker := &Walker{Targets: []Target{
		{Member: Member{ID: "a", Protocol: "openai-responses"}, Model: "a", Provider: first},
		{Member: Member{ID: "b", Protocol: "openai-responses"}, Model: "b", Provider: second},
	}}

	stream, err := walker.Open(context.Background(), providers.DispatchRequest{})
	if err == nil || stream != nil {
		t.Fatalf("stream=%#v err=%v", stream, err)
	}
	if first.opens != 1 || second.opens != 1 {
		t.Fatalf("opens first=%d second=%d", first.opens, second.opens)
	}
	attempts := walker.Attempts()
	if len(attempts) != 2 {
		t.Fatalf("attempts=%#v", attempts)
	}
	for _, attempt := range attempts {
		if attempt.Code != CodeIncompletePrecommitFailover || attempt.Decision != DecisionHop {
			t.Fatalf("attempt=%#v", attempt)
		}
	}
}

func TestWalkerCancellationDuringPrecommitStreamDoesNotFailOver(t *testing.T) {
	first := &recordingProvider{streamErr: context.Canceled}
	second := &recordingProvider{events: []protocol.Event{{Type: protocol.EventTextDelta, Text: "must-not-run"}}}
	walker := &Walker{Targets: []Target{
		{Member: Member{ID: "a", Protocol: "openai-responses"}, Model: "a", Provider: first},
		{Member: Member{ID: "b", Protocol: "openai-responses"}, Model: "b", Provider: second},
	}}

	_, err := walker.Open(context.Background(), providers.DispatchRequest{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	if second.opens != 0 {
		t.Fatalf("second opens=%d", second.opens)
	}
}
