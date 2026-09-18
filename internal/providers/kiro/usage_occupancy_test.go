package kiro

import (
	"bytes"
	"io"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
)

func TestOccupancyTokensFromPercentage(t *testing.T) {
	if got := OccupancyTokens(50, 200_000); got != 100_000 {
		t.Fatalf("occupancy=%d", got)
	}
	if got := OccupancyTokens(33.33, 200_000); got != 66_660 {
		t.Fatalf("rounded occupancy=%d", got)
	}
	if got := OccupancyTokens(0, 200_000); got != 0 {
		t.Fatalf("zero percent=%d", got)
	}
	if got := OccupancyTokens(40, 0); got != 0 {
		t.Fatalf("missing window=%d", got)
	}
	if got := OccupancyTokens(150, 100); got != 100 {
		t.Fatalf("capped percent=%d", got)
	}
}

func TestApplyKiroOccupancyPreservesTurnUsageAndDoesNotDoubleCountCache(t *testing.T) {
	usage := protocol.Usage{InputTokens: 80, OutputTokens: 20, TotalTokens: 100, CachedInputTokens: 10}
	got := ApplyKiroOccupancy(usage, 40, 1_000)
	if got.InputTokens != 80 || got.OutputTokens != 20 || got.TotalTokens != 100 || got.CachedInputTokens != 10 {
		t.Fatalf("turn usage changed: %#v", got)
	}
	if got.ContextTotalTokens != 400 {
		t.Fatalf("occupancy=%d", got.ContextTotalTokens)
	}
	if got.ContextTotalTokens == got.InputTokens+got.CachedInputTokens {
		t.Fatal("occupancy double-counted cache onto turn input")
	}
}

func TestStreamUsesContextUsagePercentageAsAbsoluteOccupancy(t *testing.T) {
	pct := EncodeEventStreamMessage(map[string]string{":event-type": "contextUsageEvent"}, []byte(`{"contextUsagePercentage":40}`))
	meta := EncodeEventStreamMessage(map[string]string{":event-type": "metadataEvent"}, []byte(`{"usage":{"inputTokens":11,"outputTokens":3}}`))
	s := &stream{buf: append(pct, meta...), window: 1_000}
	ev, err := s.Next()
	if err != nil || ev.Type != protocol.EventDone || ev.Usage == nil {
		t.Fatalf("done=%#v err=%v", ev, err)
	}
	if ev.Usage.InputTokens != 11 || ev.Usage.OutputTokens != 3 || ev.Usage.TotalTokens != 14 {
		t.Fatalf("per-turn=%#v", ev.Usage)
	}
	if ev.Usage.ContextTotalTokens != 400 {
		t.Fatalf("occupancy=%#v", ev.Usage)
	}
}

func TestStreamUsesMetadataContextUsagePercentage(t *testing.T) {
	meta := EncodeEventStreamMessage(map[string]string{":event-type": "metadataEvent"}, []byte(`{"usage":{"inputTokens":11,"outputTokens":3},"contextUsagePercentage":25}`))
	s := &stream{buf: append([]byte(nil), meta...), window: 800}
	ev, err := s.Next()
	if err != nil || ev.Usage == nil || ev.Usage.InputTokens != 11 || ev.Usage.ContextTotalTokens != 200 {
		t.Fatalf("usage=%#v err=%v", ev, err)
	}
}

func TestStreamDoesNotInventOccupancyWithoutPercentage(t *testing.T) {
	meta := EncodeEventStreamMessage(map[string]string{":event-type": "metadataEvent"}, []byte(`{"usage":{"inputTokens":11,"outputTokens":3}}`))
	s := &stream{buf: append([]byte(nil), meta...), window: 1_000}
	ev, err := s.Next()
	if err != nil || ev.Usage == nil || ev.Usage.ContextTotalTokens != 0 {
		t.Fatalf("usage=%#v err=%v", ev, err)
	}
}

func TestStreamContextUsageEventDoesNotCloseAssistantBoundary(t *testing.T) {
	pct := EncodeEventStreamMessage(map[string]string{":event-type": "contextUsageEvent"}, []byte(`{"contextUsagePercentage":12.5}`))
	s := &stream{buf: append([]byte(nil), pct...), window: 200_000, body: io.NopCloser(bytes.NewReader(nil))}
	ev, err := s.Next()
	if err == nil && ev.Type == protocol.EventAssistantBoundary {
		t.Fatalf("context usage closed an assistant boundary: %#v", ev)
	}
	if err != io.EOF {
		t.Fatalf("ev=%#v err=%v", ev, err)
	}
}
