package cursor

import (
	"bytes"
	"errors"
	"testing"
)

func TestConnectFramesRoundTripAndBoundPayload(t *testing.T) {
	raw, err := EncodeConnectFrame([]byte("hello"), true)
	if err != nil {
		t.Fatal(err)
	}
	frames, rem, err := DecodeConnectFrames(append(raw, 0x00))
	if err != nil || len(frames) != 1 || !frames[0].EndStream || !bytes.Equal(frames[0].Payload, []byte("hello")) || len(rem) != 1 {
		t.Fatalf("frames=%#v rem=%v err=%v", frames, rem, err)
	}
	if _, err := EncodeConnectFrame(make([]byte, maxConnectPayload+1), false); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("oversize=%v", err)
	}
}
