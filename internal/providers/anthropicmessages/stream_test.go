package anthropicmessages

import (
	"io"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/responses/sse"
)

func TestStreamParsesTextUsageAndTerminalStop(t *testing.T) {
	wire := frame("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"m","stop_reason":null,"usage":{"input_tokens":7,"output_tokens":0,"cache_read_input_tokens":2,"cache_creation_input_tokens":1}}}`) +
		frame("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`) +
		frame("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`) +
		frame("content_block_stop", `{"type":"content_block_stop","index":0}`) +
		frame("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":3}}`) +
		frame("message_stop", `{"type":"message_stop"}`)
	stream := NewStream(strings.NewReader(wire), sse.Limits{})

	text, err := stream.Next()
	if err != nil || text.Type != protocol.EventTextDelta || text.Text != "hello" {
		t.Fatalf("text=%#v err=%v", text, err)
	}
	done, err := stream.Next()
	if err != nil || done.Type != protocol.EventDone || done.StopReason != "end_turn" || done.Usage == nil {
		t.Fatalf("done=%#v err=%v", done, err)
	}
	if done.Usage.InputTokens != 10 || done.Usage.OutputTokens != 3 || done.Usage.TotalTokens != 13 || done.Usage.CacheReadInputTokens != 2 || done.Usage.CachedInputTokens != 2 || done.Usage.CacheCreationInputTokens != 1 {
		t.Fatalf("usage=%#v", done.Usage)
	}
	if _, err := stream.Next(); err != io.EOF {
		t.Fatalf("terminal Next err=%v", err)
	}
}

func TestStreamRecomputesTotalInputWhenCacheUsageArrivesLater(t *testing.T) {
	wire := frame("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"m","usage":{"input_tokens":5,"output_tokens":0}}}`) +
		frame("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2,"cache_read_input_tokens":11,"cache_creation_input_tokens":3}}`) +
		frame("message_stop", `{"type":"message_stop"}`)
	stream := NewStream(strings.NewReader(wire), sse.Limits{})

	done, err := stream.Next()
	if err != nil || done.Type != protocol.EventDone || done.Usage == nil {
		t.Fatalf("done=%#v err=%v", done, err)
	}
	if done.Usage.InputTokens != 19 || done.Usage.TotalTokens != 21 {
		t.Fatalf("usage=%#v", done.Usage)
	}
}

func TestStreamParsesThinkingSignatureAndToolUse(t *testing.T) {
	wire := frame("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"m","usage":{"input_tokens":1,"output_tokens":0}}}`) +
		frame("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`) +
		frame("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"hmm"}}`) +
		frame("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig"}}`) +
		frame("content_block_stop", `{"type":"content_block_stop","index":0}`) +
		frame("content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_1","name":"fs__read","input":{}}}`) +
		frame("content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"path\":\"/README.md\"}"}}`) +
		frame("content_block_stop", `{"type":"content_block_stop","index":1}`) +
		frame("message_delta", `{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":4}}`) +
		frame("message_stop", `{"type":"message_stop"}`)
	stream := NewStream(strings.NewReader(wire), sse.Limits{})

	want := []protocol.EventType{
		protocol.EventThinkingDelta,
		protocol.EventThinkingSignature,
		protocol.EventToolCallStart,
		protocol.EventToolCallDelta,
		protocol.EventToolCallEnd,
		protocol.EventDone,
	}
	var got []protocol.Event
	for range want {
		event, err := stream.Next()
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, event)
	}
	for i := range want {
		if got[i].Type != want[i] {
			t.Fatalf("event[%d]=%#v want %s", i, got[i], want[i])
		}
	}
	if got[0].Thinking != "hmm" || got[1].Signature != "sig" {
		t.Fatalf("thinking events=%#v %#v", got[0], got[1])
	}
	if got[2].ID != "toolu_1" || got[2].Name != "fs__read" || got[3].Arguments != `{"path":"/README.md"}` || got[4].ID != "toolu_1" {
		t.Fatalf("tool events=%#v", got[2:5])
	}
	if got[5].StopReason != "tool_use" || got[5].Usage == nil || got[5].Usage.OutputTokens != 4 {
		t.Fatalf("done=%#v", got[5])
	}
}

func TestStreamClosesRedactedThinkingBlock(t *testing.T) {
	wire := frame("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"m","usage":{"input_tokens":1,"output_tokens":0}}}`) +
		frame("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"redacted_thinking","data":"opaque"}}`) +
		frame("content_block_stop", `{"type":"content_block_stop","index":0}`) +
		frame("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`) +
		frame("message_stop", `{"type":"message_stop"}`)
	stream := NewStream(strings.NewReader(wire), sse.Limits{})

	redacted, err := stream.Next()
	if err != nil || redacted.Type != protocol.EventRedactedThinking || redacted.Data != "opaque" {
		t.Fatalf("redacted=%#v err=%v", redacted, err)
	}
	done, err := stream.Next()
	if err != nil || done.Type != protocol.EventDone || done.StopReason != "end_turn" {
		t.Fatalf("done=%#v err=%v", done, err)
	}
}

func TestStreamFailsClosedOnMalformedFrame(t *testing.T) {
	stream := NewStream(strings.NewReader(frame("content_block_delta", `{not-json}`)), sse.Limits{})
	event, err := stream.Next()
	if err != nil {
		t.Fatal(err)
	}
	if event.Type != protocol.EventError || event.HTTPStatus != 502 {
		t.Fatalf("event=%#v", event)
	}
}

func TestStreamFailsClosedWhenMessageStopIsMissing(t *testing.T) {
	wire := frame("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"m","usage":{"input_tokens":1,"output_tokens":0}}}`) +
		frame("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`) +
		frame("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"partial"}}`)
	stream := NewStream(strings.NewReader(wire), sse.Limits{})
	first, err := stream.Next()
	if err != nil || first.Type != protocol.EventTextDelta {
		t.Fatalf("first=%#v err=%v", first, err)
	}
	terminal, err := stream.Next()
	if err != nil || terminal.Type != protocol.EventError || !strings.Contains(terminal.Message, "terminal") {
		t.Fatalf("terminal=%#v err=%v", terminal, err)
	}
}

func frame(name, payload string) string {
	return "event: " + name + "\n" + "data: " + payload + "\n\n"
}
