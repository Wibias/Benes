package google

import "testing"

func TestTruncatedTurnAndStopReason(t *testing.T) {
	if !TruncatedTurn("MALFORMED_FUNCTION_CALL", 0) {
		t.Fatal("malformed")
	}
	if TruncatedTurn("MAX_TOKENS", 0) || !TruncatedTurn("MAX_TOKENS", 1) {
		t.Fatal("max tokens")
	}
	if StopReason("MAX_TOKENS") != "max_tokens" || StopReason("SAFETY") != "content_filter" {
		t.Fatal("stop")
	}
}
