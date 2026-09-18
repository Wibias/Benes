package request

import (
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
)

func TestDecodeMarksChatCompletionsSource(t *testing.T) {
	got, err := Decode(strings.NewReader(`{"model":"p/m","messages":[{"role":"user","content":"hi"}]}`), 1<<20, DecodeOptions{NowMillis: 1})
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != protocol.RequestSourceChatCompletions {
		t.Fatalf("source=%q", got.Source)
	}
}
