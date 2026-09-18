package kiro

import (
	"fmt"
	"io"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
)

const maxCollectedBytes = 8 << 20

func Collect(stream providers.EventStream, maxBytes int) ([]protocol.Event, error) {
	if stream == nil {
		return nil, fmt.Errorf("kiro stream is required")
	}
	if maxBytes <= 0 {
		maxBytes = maxCollectedBytes
	}
	defer stream.Close()
	var (
		out   []protocol.Event
		total int
	)
	for {
		ev, err := stream.Next()
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return out, err
		}
		total += len(ev.Text) + len(ev.Thinking) + len(ev.Data) + len(ev.Arguments)
		if total > maxBytes {
			return out, fmt.Errorf("Kiro buffered collection exceeds byte cap")
		}
		out = append(out, ev)
	}
}
