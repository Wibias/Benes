package google

import (
	"io"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
)

type stubStream struct {
	events []protocol.Event
	closed bool
}

func (s *stubStream) Next() (protocol.Event, error) {
	if len(s.events) == 0 {
		return protocol.Event{}, io.EOF
	}
	ev := s.events[0]
	s.events = s.events[1:]
	return ev, nil
}

func (s *stubStream) Close() error {
	s.closed = true
	return nil
}

func TestCollectBoundsAndCloses(t *testing.T) {
	stream := &stubStream{events: []protocol.Event{
		{Type: protocol.EventTextDelta, Text: "ab"},
		{Type: protocol.EventTextDelta, Text: strings.Repeat("x", 8)},
	}}
	if _, err := Collect(stream, 4); err == nil || !strings.Contains(err.Error(), "byte cap") {
		t.Fatalf("err=%v", err)
	}
	if !stream.closed {
		t.Fatal("must close")
	}
}

func TestCollectReturnsEventsOnEOF(t *testing.T) {
	stream := &stubStream{events: []protocol.Event{
		{Type: protocol.EventTextDelta, Text: "hi"},
		{Type: protocol.EventDone},
	}}
	got, err := Collect(stream, 0)
	if err != nil || len(got) != 2 || !stream.closed {
		t.Fatalf("got=%#v err=%v closed=%v", got, err, stream.closed)
	}
}
