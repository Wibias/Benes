package outbound

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/timeline"
)

type eventStream struct {
	events []protocol.Event
	err    error
	index  int
}

func (s *eventStream) Next() (protocol.Event, error) {
	if s.index < len(s.events) {
		e := s.events[s.index]
		s.index++
		return e, nil
	}
	if s.err != nil {
		err := s.err
		s.err = nil
		return protocol.Event{}, err
	}
	return protocol.Event{}, io.EOF
}
func (s *eventStream) Close() error { return nil }

func TestWriteSSEFramesNamedAnthropicEventsWithoutDoneSentinel(t *testing.T) {
	stream := &eventStream{events: []protocol.Event{
		{Type: protocol.EventTextDelta, Text: "hello"},
		{Type: protocol.EventDone, Usage: &protocol.Usage{InputTokens: 4, OutputTokens: 2}},
	}}
	rr := httptest.NewRecorder()
	if err := WriteSSE(context.Background(), rr, stream, "model", Options{IDGenerator: func() string { return "msg" }}); err != nil {
		t.Fatal(err)
	}
	if rr.Code != http.StatusOK || !strings.HasPrefix(rr.Header().Get("Content-Type"), "text/event-stream") {
		t.Fatalf("status=%d headers=%v", rr.Code, rr.Header())
	}
	body := rr.Body.String()
	for _, want := range []string{"event: message_start\n", "event: content_block_start\n", "event: content_block_delta\n", "event: content_block_stop\n", "event: message_delta\n", "event: message_stop\n"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "[DONE]") {
		t.Fatalf("Anthropic stream emitted OpenAI sentinel: %s", body)
	}
}

func TestWriteSSEInitialFailureDoesNotManufactureMessageStart(t *testing.T) {
	stream := &eventStream{events: []protocol.Event{{Type: protocol.EventError, Message: "busy", HTTPStatus: 529}}}
	rr := httptest.NewRecorder()
	if err := WriteSSE(context.Background(), rr, stream, "model", Options{IDGenerator: func() string { return "msg" }}); err != nil {
		t.Fatal(err)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "event: error\n") || strings.Contains(body, "event: message_start\n") {
		t.Fatalf("body=%s", body)
	}
	if !strings.Contains(body, `"type":"overloaded_error"`) {
		t.Fatalf("body=%s", body)
	}
}

func TestWriteSSEProviderEOFBecomesTerminalAnthropicError(t *testing.T) {
	rr := httptest.NewRecorder()
	if err := WriteSSE(context.Background(), rr, &eventStream{}, "model", Options{IDGenerator: func() string { return "msg" }}); err != nil {
		t.Fatal(err)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "event: error\n") || !strings.Contains(body, `"code":"upstream_incomplete"`) {
		t.Fatalf("body=%s", body)
	}
}

func TestCollectBuildsFinalAnthropicMessage(t *testing.T) {
	stream := &eventStream{events: []protocol.Event{
		{Type: protocol.EventTextDelta, Text: "hello"},
		{Type: protocol.EventDone, Usage: &protocol.Usage{InputTokens: 3, OutputTokens: 1}},
	}}
	message, err := Collect(stream, "model", Options{IDGenerator: func() string { return "msg" }})
	if err != nil {
		t.Fatal(err)
	}
	if message["id"] != "msg" || message["model"] != "model" || message["stop_reason"] != "end_turn" {
		t.Fatalf("message=%#v", message)
	}
	content := message["content"].([]any)
	if len(content) != 1 || content[0].(map[string]any)["text"] != "hello" {
		t.Fatalf("content=%#v", content)
	}
}

func TestCollectPreservesTerminalProviderFailure(t *testing.T) {
	stream := &eventStream{events: []protocol.Event{{Type: protocol.EventError, Message: "slow down", HTTPStatus: 429, Code: "rate_limit_exceeded"}}}
	_, err := Collect(stream, "model", Options{IDGenerator: func() string { return "msg" }})
	var terminal *TerminalError
	if !errors.As(err, &terminal) || terminal.Status != 429 || terminal.Type != "rate_limit_error" || terminal.Code != "rate_limit_exceeded" {
		t.Fatalf("err=%#v", err)
	}
}

func TestWriteSSERejectsCanceledContextBeforeReadingProvider(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	stream := &countingStream{}
	err := WriteSSE(ctx, httptest.NewRecorder(), stream, "model", Options{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	if stream.nextCalls != 0 {
		t.Fatalf("Next called %d times", stream.nextCalls)
	}
}

type countingStream struct{ nextCalls int }

func (s *countingStream) Next() (protocol.Event, error) {
	s.nextCalls++
	return protocol.Event{}, io.EOF
}
func (s *countingStream) Close() error { return nil }

func TestHTTPHelpersRejectNilDependencies(t *testing.T) {
	if err := WriteSSE(nil, httptest.NewRecorder(), &eventStream{}, "m", Options{}); err == nil {
		t.Fatal("nil context accepted")
	}
	if err := WriteSSE(context.Background(), nil, &eventStream{}, "m", Options{}); err == nil {
		t.Fatal("nil writer accepted")
	}
	if err := WriteSSE(context.Background(), httptest.NewRecorder(), nil, "m", Options{}); err == nil {
		t.Fatal("nil stream accepted")
	}
	if _, err := Collect(nil, "m", Options{}); err == nil {
		t.Fatal("nil collector stream accepted")
	}
}

func TestWriteSSERedactsProviderReaderFailureAndDoesNotCloseStream(t *testing.T) {
	stream := &ownedStream{err: errors.New("SECRET provider socket failure")}
	rr := httptest.NewRecorder()
	if err := WriteSSE(context.Background(), rr, stream, "model", Options{IDGenerator: func() string { return "msg" }}); err != nil {
		t.Fatal(err)
	}
	body := rr.Body.String()
	if strings.Contains(body, "SECRET") || !strings.Contains(body, "provider stream failed") {
		t.Fatalf("body=%s", body)
	}
	if stream.closeCalls != 0 {
		t.Fatalf("wrapper stole stream ownership: closes=%d", stream.closeCalls)
	}
}

type ownedStream struct {
	err        error
	closeCalls int
}

func (s *ownedStream) Next() (protocol.Event, error) { return protocol.Event{}, s.err }
func (s *ownedStream) Close() error                  { s.closeCalls++; return nil }

type failingWriter struct {
	header http.Header
	err    error
}

func (w *failingWriter) Header() http.Header       { return w.header }
func (w *failingWriter) WriteHeader(int)           {}
func (w *failingWriter) Write([]byte) (int, error) { return 0, w.err }

func TestWriteSSEPropagatesWriterFailure(t *testing.T) {
	sentinel := errors.New("write failed")
	writer := &failingWriter{header: make(http.Header), err: sentinel}
	stream := &eventStream{events: []protocol.Event{{Type: protocol.EventTextDelta, Text: "hello"}}}
	err := WriteSSE(context.Background(), writer, stream, "model", Options{IDGenerator: func() string { return "msg" }})
	if !errors.Is(err, sentinel) {
		t.Fatalf("err=%v", err)
	}
}

func TestWriteSSEMarksFirstDownstreamAndTTFT(t *testing.T) {
	stream := &eventStream{events: []protocol.Event{
		{Type: protocol.EventTextDelta, Text: "hello"},
		{Type: protocol.EventDone, Usage: &protocol.Usage{InputTokens: 4, OutputTokens: 2}},
	}}
	tr := timeline.New("req-anthropic", 8)
	rr := httptest.NewRecorder()
	if err := WriteSSE(context.Background(), rr, stream, "model", Options{
		IDGenerator: func() string { return "msg" },
		Trace:       tr,
	}); err != nil {
		t.Fatal(err)
	}
	var sawDown, sawTTFT bool
	for _, ev := range tr.Events() {
		if ev.Milestone == timeline.MilestoneFirstDownstream && ev.OK {
			sawDown = true
		}
		if ev.Milestone == timeline.MilestoneTTFT && ev.OK {
			sawTTFT = true
		}
	}
	if !sawDown || !sawTTFT {
		t.Fatalf("events=%#v", tr.Events())
	}
}

func TestWriteSSEMarksRelayTransformOnMalformedEvent(t *testing.T) {
	stream := &eventStream{events: []protocol.Event{{Type: protocol.EventType("not-a-canonical-event")}}}
	tr := timeline.New("req-relay", 8)
	if err := WriteSSE(context.Background(), httptest.NewRecorder(), stream, "model", Options{
		IDGenerator: func() string { return "msg" },
		Trace:       tr,
	}); err != nil {
		t.Fatal(err)
	}
	var saw bool
	for _, ev := range tr.Events() {
		if ev.Stage == timeline.StageRelayTransform && !ev.OK && ev.Cause == "malformed_frame" {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("events=%#v", tr.Events())
	}
}

func TestWriteSSEMarksDownstreamEndOnTerminal(t *testing.T) {
	stream := &eventStream{events: []protocol.Event{
		{Type: protocol.EventTextDelta, Text: "hello"},
		{Type: protocol.EventDone, Usage: &protocol.Usage{InputTokens: 4, OutputTokens: 2}},
	}}
	tr := timeline.New("req-anthropic-end", 8)
	if err := WriteSSE(context.Background(), httptest.NewRecorder(), stream, "model", Options{
		IDGenerator: func() string { return "msg" },
		Trace:       tr,
	}); err != nil {
		t.Fatal(err)
	}
	var saw bool
	for _, ev := range tr.Events() {
		if ev.Milestone == timeline.MilestoneDownstreamEnd && ev.OK {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("events=%#v", tr.Events())
	}
}

func TestWriteSSEMarksDownstreamWriteFailure(t *testing.T) {
	sentinel := errors.New("write failed")
	tr := timeline.New("req-write", 8)
	writer := &failingWriter{header: make(http.Header), err: sentinel}
	stream := &eventStream{events: []protocol.Event{{Type: protocol.EventTextDelta, Text: "hello"}}}
	err := WriteSSE(context.Background(), writer, stream, "model", Options{
		IDGenerator: func() string { return "msg" },
		Trace:       tr,
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("err=%v", err)
	}
	var saw bool
	for _, ev := range tr.Events() {
		if ev.Stage == timeline.StageDownstreamWrite && !ev.OK && ev.Cause == "downstream_write" {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("events=%#v", tr.Events())
	}
}

func TestWriteSSEMarksClientCancelAfterFirstFrame(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tr := timeline.New("req-cancel", 8)
	writer := &cancelAfterWrite{header: make(http.Header), cancel: cancel}
	stream := &eventStream{events: []protocol.Event{
		{Type: protocol.EventTextDelta, Text: "hello"},
		{Type: protocol.EventDone},
	}}
	err := WriteSSE(ctx, writer, stream, "model", Options{
		IDGenerator: func() string { return "msg" },
		Trace:       tr,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	var saw bool
	for _, ev := range tr.Events() {
		if ev.Stage == timeline.StageClientCancel && !ev.OK && ev.Cause == "client_cancel" {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("events=%#v", tr.Events())
	}
}

type cancelAfterWrite struct {
	header http.Header
	body   []byte
	cancel context.CancelFunc
}

func (w *cancelAfterWrite) Header() http.Header { return w.header }
func (w *cancelAfterWrite) WriteHeader(int)     {}
func (w *cancelAfterWrite) Write(p []byte) (int, error) {
	w.body = append(w.body, p...)
	if w.cancel != nil {
		w.cancel()
	}
	return len(p), nil
}
