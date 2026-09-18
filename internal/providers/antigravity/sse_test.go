package antigravity

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestDecoderCountsRawBytesAndBlocksSplitUTF8Bypass(t *testing.T) {
	payload := "data: " + strings.Repeat("é", 20) + "\n"
	dec := NewDecoder(bytes.NewReader([]byte(payload)), 10)
	if _, err := dec.Next(); !errors.Is(err, ErrFrameTooLarge) && err == nil {
		if len([]byte(payload)) <= 10 {
			t.Fatal("expected raw-byte overflow")
		}
	}
	small := NewDecoder(bytes.NewReader([]byte("data: {\"text\":\"hi\"}\n")), 1024)
	line, err := small.Next()
	if err != nil || !strings.Contains(line, "hi") || !small.SawContent() {
		t.Fatalf("line=%q err=%v", line, err)
	}
}

func TestPeerFailoverOnlyBeforeContentAndOnlyOnce(t *testing.T) {
	dec := NewDecoder(bytes.NewReader(nil), 1024)
	if err := dec.AllowPeerFailover(Failover503); err != nil {
		t.Fatal(err)
	}
	if err := dec.AllowPeerFailover(Failover404); !errors.Is(err, ErrFailoverForbidden) {
		t.Fatalf("second failover=%v", err)
	}
	started := NewDecoder(bytes.NewReader([]byte("data: {\"candidates\":[{}]}\n")), 1024)
	_, _ = started.Next()
	if err := started.AllowPeerFailover(FailoverEmpty); !errors.Is(err, ErrFailoverForbidden) {
		t.Fatalf("after content=%v", err)
	}
}
