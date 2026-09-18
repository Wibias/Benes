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

type fakeStream struct {
	events []protocol.Event
	err    error
	index  int
	closed bool
}

func (s *fakeStream) Next() (protocol.Event, error) {
	if s.index < len(s.events) {
		event := s.events[s.index]
		s.index++
		return event, nil
	}
	if s.err != nil {
		err := s.err
		s.err = nil
		return protocol.Event{}, err
	}
	return protocol.Event{}, io.EOF
}
func (s *fakeStream) Close() error { s.closed = true; return nil }

func TestWriteSSEFramesCanonicalEventsAndLeavesStreamOwnershipToCaller(t *testing.T) {
	stream := &fakeStream{events: []protocol.Event{
		{Type: protocol.EventTextDelta, Text: "hi"},
		{Type: protocol.EventDone, Usage: &protocol.Usage{InputTokens: 2, OutputTokens: 1}},
	}}
	rr := httptest.NewRecorder()
	err := WriteSSE(context.Background(), rr, stream, "client/model", Options{IDGenerator: func() string { return "chatcmpl-fixed" }, CreatedUnix: 123})
	if err != nil {
		t.Fatal(err)
	}
	if stream.closed {
		t.Fatal("WriteSSE closed caller-owned stream")
	}
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("Content-Type"); got != "text/event-stream; charset=utf-8" {
		t.Fatalf("Content-Type=%q", got)
	}
	if rr.Header().Get("Cache-Control") != "no-store" || rr.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("headers=%v", rr.Header())
	}
	body := rr.Body.String()
	if !strings.Contains(body, `"role":"assistant"`) || !strings.Contains(body, `"content":"hi"`) || !strings.Contains(body, `"finish_reason":"stop"`) {
		t.Fatalf("body=%s", body)
	}
	if !strings.HasSuffix(body, "data: [DONE]\n\n") {
		t.Fatalf("missing DONE: %q", body)
	}
}

func TestWriteSSEMarksFirstDownstreamAndTTFT(t *testing.T) {
	stream := &fakeStream{events: []protocol.Event{
		{Type: protocol.EventTextDelta, Text: "hi"},
		{Type: protocol.EventDone},
	}}
	tr := timeline.New("req-chat", 8)
	rr := httptest.NewRecorder()
	if err := WriteSSE(context.Background(), rr, stream, "m", Options{
		IDGenerator: func() string { return "id" }, CreatedUnix: 1, Trace: tr,
	}); err != nil {
		t.Fatal(err)
	}
	var sawDown, sawTTFT bool
	for _, ev := range tr.Events() {
		if ev.Milestone == timeline.MilestoneFirstDownstream {
			sawDown = true
		}
		if ev.Milestone == timeline.MilestoneTTFT {
			sawTTFT = true
		}
	}
	if !sawDown || !sawTTFT {
		t.Fatalf("events=%#v", tr.Events())
	}
}

func TestWriteSSEMarksRelayTransformOnMalformedEvent(t *testing.T) {
	stream := &fakeStream{events: []protocol.Event{{Type: protocol.EventType("not-a-canonical-event")}}}
	tr := timeline.New("req-relay", 8)
	if err := WriteSSE(context.Background(), httptest.NewRecorder(), stream, "m", Options{
		IDGenerator: func() string { return "id" }, CreatedUnix: 1, Trace: tr,
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
	stream := &fakeStream{events: []protocol.Event{
		{Type: protocol.EventTextDelta, Text: "hi"},
		{Type: protocol.EventDone},
	}}
	tr := timeline.New("req-chat-end", 8)
	if err := WriteSSE(context.Background(), httptest.NewRecorder(), stream, "m", Options{
		IDGenerator: func() string { return "id" }, CreatedUnix: 1, Trace: tr,
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

func TestWriteSSECanonicalErrorAndPrematureEOFDoNotEmitDone(t *testing.T) {
	for _, stream := range []*fakeStream{
		{events: []protocol.Event{{Type: protocol.EventError, Message: "provider failed", HTTPStatus: 429, ErrorType: "rate_limit_error", Code: "rate_limit_exceeded"}}},
		{events: []protocol.Event{{Type: protocol.EventTextDelta, Text: "partial"}}},
	} {
		rr := httptest.NewRecorder()
		if err := WriteSSE(context.Background(), rr, stream, "m", Options{IDGenerator: func() string { return "id" }, CreatedUnix: 1}); err != nil {
			t.Fatal(err)
		}
		body := rr.Body.String()
		if !strings.Contains(body, `"error"`) || strings.Contains(body, "[DONE]") {
			t.Fatalf("body=%s", body)
		}
	}
}

func TestWriteSSERedactsRawStreamFailure(t *testing.T) {
	sentinel := errors.New("TOP-SECRET provider socket error")
	stream := &fakeStream{err: sentinel}
	rr := httptest.NewRecorder()
	if err := WriteSSE(context.Background(), rr, stream, "m", Options{IDGenerator: func() string { return "id" }, CreatedUnix: 1}); err != nil {
		t.Fatal(err)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "provider stream failed") || strings.Contains(body, "TOP-SECRET") || strings.Contains(body, "[DONE]") {
		t.Fatalf("body=%s", body)
	}
}

func TestCollectReturnsFinalChatCompletionWithoutClosingStream(t *testing.T) {
	stream := &fakeStream{events: []protocol.Event{
		{Type: protocol.EventReasoningRawDelta, Text: "think"},
		{Type: protocol.EventTextDelta, Text: "answer"},
		{Type: protocol.EventDone, Usage: &protocol.Usage{InputTokens: 3, OutputTokens: 4}},
	}}
	completion, err := Collect(stream, "client/model", Options{IDGenerator: func() string { return "chatcmpl-fixed" }, CreatedUnix: 123})
	if err != nil {
		t.Fatal(err)
	}
	if stream.closed {
		t.Fatal("Collect closed caller-owned stream")
	}
	choice := completion["choices"].([]any)[0].(map[string]any)
	message := choice["message"].(map[string]any)
	if message["content"] != "answer" || message["reasoning_content"] != "think" || choice["finish_reason"] != "stop" {
		t.Fatalf("completion=%#v", completion)
	}
}

func TestCollectTurnsEOFAndReadFailureIntoTypedTerminalErrors(t *testing.T) {
	tests := []struct {
		name   string
		stream *fakeStream
		code   string
	}{
		{"eof", &fakeStream{events: []protocol.Event{{Type: protocol.EventTextDelta, Text: "partial"}}}, "upstream_incomplete"},
		{"read error", &fakeStream{err: errors.New("SECRET transport")}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			completion, err := Collect(tt.stream, "m", Options{IDGenerator: func() string { return "id" }, CreatedUnix: 1})
			if completion != nil || err == nil {
				t.Fatalf("completion=%#v err=%v", completion, err)
			}
			var terminal *TerminalError
			if !errors.As(err, &terminal) {
				t.Fatalf("err=%T %v", err, err)
			}
			if tt.code != "" && terminal.Code != tt.code {
				t.Fatalf("terminal=%#v", terminal)
			}
			if strings.Contains(terminal.Message, "SECRET") {
				t.Fatalf("raw stream error leaked: %v", terminal)
			}
		})
	}
}

func TestWriteSSERejectsCancelledContextBeforeWritingHeaders(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rr := httptest.NewRecorder()
	err := WriteSSE(ctx, rr, &fakeStream{}, "m", Options{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	if rr.Code != http.StatusOK || rr.Body.Len() != 0 || rr.Header().Get("Content-Type") != "" {
		t.Fatalf("response mutated: status=%d headers=%v body=%q", rr.Code, rr.Header(), rr.Body.String())
	}
}

func TestWriteSSEReturnsWriterFailure(t *testing.T) {
	sentinel := errors.New("write failed")
	w := &failingResponseWriter{err: sentinel}
	stream := &fakeStream{events: []protocol.Event{{Type: protocol.EventTextDelta, Text: "x"}}}
	err := WriteSSE(context.Background(), w, stream, "m", Options{IDGenerator: func() string { return "id" }, CreatedUnix: 1})
	if !errors.Is(err, sentinel) {
		t.Fatalf("err=%v", err)
	}
}

type failingResponseWriter struct {
	header http.Header
	err    error
}

func (w *failingResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}
func (w *failingResponseWriter) WriteHeader(int)           {}
func (w *failingResponseWriter) Write([]byte) (int, error) { return 0, w.err }

func TestHTTPHelpersRejectNilDependencies(t *testing.T) {
	if err := WriteSSE(nil, httptest.NewRecorder(), &fakeStream{}, "m", Options{}); err == nil {
		t.Fatal("nil context accepted")
	}
	if err := WriteSSE(context.Background(), nil, &fakeStream{}, "m", Options{}); err == nil {
		t.Fatal("nil writer accepted")
	}
	if err := WriteSSE(context.Background(), httptest.NewRecorder(), nil, "m", Options{}); err == nil {
		t.Fatal("nil stream accepted")
	}
	if _, err := Collect(nil, "m", Options{}); err == nil {
		t.Fatal("nil stream accepted by Collect")
	}
}
