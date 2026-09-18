package combo

import (
	"context"
	"io"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
)

// owningStream is a distinguishable EventStream that reports PhysicalOwnership.
type owningStream struct {
	events []protocol.Event
	pin    *providers.PhysicalPin
	closed bool
}

func (s *owningStream) Next() (protocol.Event, error) {
	if len(s.events) == 0 {
		return protocol.Event{}, io.EOF
	}
	ev := s.events[0]
	s.events = s.events[1:]
	return ev, nil
}

func (s *owningStream) Close() error {
	s.closed = true
	return nil
}

func (s *owningStream) PhysicalOwnership() *providers.PhysicalPin {
	if s == nil || s.pin == nil {
		return nil
	}
	pin := *s.pin
	return &pin
}

type owningProvider struct {
	pin    *providers.PhysicalPin
	events []protocol.Event
	opens  int
}

func (p *owningProvider) Open(_ context.Context, _ providers.DispatchRequest) (providers.EventStream, error) {
	p.opens++
	return &owningStream{
		events: append([]protocol.Event(nil), p.events...),
		pin:    p.pin,
	}, nil
}

// TestWalkerOpen_ForwardsPhysicalOwnershipThroughPrefixedStream is the low-level
// RED for combo.prefixedStream: after Walker.Open commits on model-visible output,
// the returned stream must expose the same PhysicalPin the inner member reported.
// Current bug: prefixedStream only implements Next/Close, so runModelTurn captures nil.
func TestWalkerOpen_ForwardsPhysicalOwnershipThroughPrefixedStream(t *testing.T) {
	want := &providers.PhysicalPin{
		Destination:   "https://example.invalid/v1",
		CredentialRef: "cred-K1-unmistakable",
		AuthClass:     "api-key",
	}
	member := &owningProvider{
		pin: want,
		events: []protocol.Event{
			{Type: protocol.EventTextDelta, Text: "visible"},
			{Type: protocol.EventDone},
		},
	}
	walker := &Walker{Targets: []Target{
		{Member: Member{ID: "m1", Protocol: "openai-responses"}, Model: "m", Provider: member},
	}}
	stream, err := walker.Open(context.Background(), providers.DispatchRequest{
		Parsed: protocol.ParsedRequest{ModelID: "combo/x", UpstreamModelID: "x"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()

	reporter, ok := stream.(interface{ PhysicalOwnership() *providers.PhysicalPin })
	if !ok {
		t.Fatalf("Walker.Open stream %T does not expose PhysicalOwnership (prefixedStream hide)", stream)
	}
	got := reporter.PhysicalOwnership()
	if got == nil {
		t.Fatal("PhysicalOwnership returned nil")
	}
	if got.CredentialRef != want.CredentialRef || got.Destination != want.Destination || got.AuthClass != want.AuthClass {
		t.Fatalf("pin=%#v want %#v", got, want)
	}
	// Defensive copy: mutating the returned pin must not alias inner storage.
	got.CredentialRef = "mutated"
	again := reporter.PhysicalOwnership()
	if again == nil || again.CredentialRef != want.CredentialRef {
		t.Fatalf("defensive copy broken: again=%#v", again)
	}
}

func TestWalkerOpen_ForwardsPhysicalOwnershipAfterIncompleteVisiblePrefix(t *testing.T) {
	want := &providers.PhysicalPin{Destination: "dest-B", CredentialRef: "acct-7", AuthClass: "oauth"}
	member := &owningProvider{
		pin: want,
		events: []protocol.Event{
			{Type: protocol.EventIncomplete, StopReason: "max_tokens"},
		},
	}
	walker := &Walker{Targets: []Target{
		{Member: Member{ID: "b", Protocol: "google"}, Model: "g", Provider: member},
	}}
	stream, err := walker.Open(context.Background(), providers.DispatchRequest{})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	reporter, ok := stream.(interface{ PhysicalOwnership() *providers.PhysicalPin })
	if !ok {
		t.Fatalf("stream %T hides PhysicalOwnership", stream)
	}
	got := reporter.PhysicalOwnership()
	if got == nil || got.CredentialRef != want.CredentialRef || got.Destination != want.Destination {
		t.Fatalf("pin=%#v want %#v", got, want)
	}
}
