package cursor

import "testing"

func TestEncodeProtoStringRoundTripTag(t *testing.T) {
	raw := EncodeProtoString(1, "gpt-5.4")
	if len(raw) < 3 || raw[0] != 0x0a {
		t.Fatalf("wire=%x", raw)
	}
	n := int(raw[1])
	if string(raw[2:2+n]) != "gpt-5.4" {
		t.Fatalf("payload=%x", raw)
	}
}
