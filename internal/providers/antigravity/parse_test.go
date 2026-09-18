package antigravity

import "testing"

func TestParseCCAFrameReadsWrappedCandidates(t *testing.T) {
	frame := parseCCAFrame(`{"response":{"candidates":[{"content":{"parts":[{"text":"hi"}]}}]}}`)
	if frame.text != "hi" {
		t.Fatalf("frame=%#v", frame)
	}
	errFrame := parseCCAFrame(`{"error":{"status":"FAILED_PRECONDITION"}}`)
	if errFrame.errKind != FailureGeoblock {
		t.Fatalf("geoblock=%#v", errFrame)
	}
	empty := parseCCAFrame(`{}`)
	if empty.kind != FailoverEmpty {
		t.Fatalf("empty=%#v", empty)
	}
}
