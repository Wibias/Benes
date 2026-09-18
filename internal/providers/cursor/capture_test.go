package cursor

import "testing"

func TestCaptureCheckpointIgnoresUnsafeFrames(t *testing.T) {
	if _, ok := CaptureCheckpoint([]byte(`{"text":"hi"}`), "c", "i", "m", "d"); ok {
		t.Fatal("text frame")
	}
	cp, ok := CaptureCheckpoint([]byte(`{"conversationCheckpointUpdate":{"token":"abc"}}`), "c", "i", "m", "d")
	if !ok || string(cp.Bytes) != `{"token":"abc"}` || cp.ConversationID != "c" {
		t.Fatalf("cp=%#v ok=%v", cp, ok)
	}
}
