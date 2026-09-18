package compaction

import (
	"strings"
	"testing"
)

func TestEncodeSummaryRoundTrips(t *testing.T) {
	encoded := EncodeSummary("handoff")
	got, ok := DecodeSummary(encoded)
	if !ok || got != "handoff" {
		t.Fatalf("got=%q ok=%v encoded=%q", got, ok, encoded)
	}
}

func TestDecodeSummaryRejectsNativeOpenAIBlobs(t *testing.T) {
	if _, ok := DecodeSummary("gAAAA-not-ours"); ok {
		t.Fatal("native blob decoded")
	}
}

func TestItemToTextDecodesBenes1AndDegradesOpaqueBlobs(t *testing.T) {
	text := ItemToText(EncodeSummary("keep going"))
	if text != SummaryPrefix+"\n\nkeep going" {
		t.Fatalf("decoded=%q", text)
	}
	if ItemToText("native-blob") != OpaqueNote {
		t.Fatalf("opaque=%q", ItemToText("native-blob"))
	}
}

func TestCompactPromptMirrorsCodexCheckpointInstruction(t *testing.T) {
	if !strings.Contains(CompactPrompt, "CONTEXT CHECKPOINT COMPACTION") || !strings.Contains(CompactPrompt, "What remains to be done") {
		t.Fatalf("prompt=%q", CompactPrompt)
	}
}
