package antigravity

import (
	"io"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/resourcebudget"
)

func TestStream_EmitsToolEndWithArgsAndThoughtSignature(t *testing.T) {
	s := &stream{
		pending: `data: {"candidates":[{"content":{"parts":[{"functionCall":{"name":"benes_fabric_delegate","id":"c9","args":{"instruction":"CHILD_INSTRUCTION_SECRET"}},"thoughtSignature":"sig-live"}]}}]}`,
		firstErr: nil,
	}
	ev, err := s.Next()
	if err != nil || ev.Type != protocol.EventToolCallStart || ev.Arguments == "" || !strings.Contains(ev.Arguments, "CHILD_INSTRUCTION_SECRET") {
		t.Fatalf("start ev=%+v err=%v", ev, err)
	}
	if len(ev.ProviderMetadata) == 0 || !strings.Contains(string(ev.ProviderMetadata), "sig-live") {
		t.Fatalf("start metadata=%s", ev.ProviderMetadata)
	}
	ev, err = s.Next()
	if err != nil || ev.Type != protocol.EventToolCallEnd {
		t.Fatalf("end ev=%+v err=%v", ev, err)
	}
	if !strings.Contains(ev.Arguments, "CHILD_INSTRUCTION_SECRET") {
		t.Fatalf("end args=%s", ev.Arguments)
	}
	if !strings.Contains(string(ev.ProviderMetadata), "sig-live") {
		t.Fatalf("end metadata=%s", ev.ProviderMetadata)
	}
	_, err = s.Next()
	if err != io.EOF && err == nil {
		// may block/read; close path: pending empty then body nil -> loop
	}
	_ = resourcebudget.ClassToolArguments
}
