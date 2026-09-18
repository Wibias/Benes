package kiro

import "testing"

func TestEventStreamRoundTripAndRejectsBadCRC(t *testing.T) {
	frame := EncodeEventStreamMessage(map[string]string{":event-type": "assistantResponseEvent"}, []byte(`{"content":"hi"}`))
	msg, err := DecodeEventStreamMessage(frame)
	if err != nil || msg.Headers[":event-type"] != "assistantResponseEvent" || string(msg.Payload) != `{"content":"hi"}` {
		t.Fatalf("msg=%#v err=%v", msg, err)
	}
	frame[len(frame)-1] ^= 0xff
	if _, err := DecodeEventStreamMessage(frame); err == nil {
		t.Fatal("bad crc")
	}
}
