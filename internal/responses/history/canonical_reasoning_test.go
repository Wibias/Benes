package history

import (
	"encoding/json"
	"testing"
)

func TestCanonicalReasoningSignatureMatchesJSONStringifyShapeAndEscaping(t *testing.T) {
	raw := json.RawMessage("{\"ignored\":1,\"encrypted_content\":\"x/y\",\"summary\":[{\"ignored\":true,\"text\":\"<\\u2028>\",\"type\":\"summary_text\"}],\"id\":\"rs\",\"type\":\"reasoning\"}")
	got, err := canonicalReasoningSignature(raw)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\"type\":\"reasoning\",\"id\":\"rs\",\"summary\":[{\"type\":\"summary_text\",\"text\":\"<\u2028>\"}],\"encrypted_content\":\"x/y\"}"
	if got != want {
		t.Fatalf("got=%q\nwant=%q", got, want)
	}
}

func TestCanonicalReasoningSignaturePreservesLoneSurrogateLikeModernJSONStringify(t *testing.T) {
	got, err := canonicalReasoningSignature(json.RawMessage(`{"type":"reasoning","summary":[{"type":"summary_text","text":"\uD800"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"type":"reasoning","summary":[{"type":"summary_text","text":"\ud800"}]}`
	if got != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

func TestCanonicalReasoningSignatureCombinesValidSurrogatePair(t *testing.T) {
	got, err := canonicalReasoningSignature(json.RawMessage(`{"type":"reasoning","content":[{"type":"reasoning_text","text":"\uD83D\uDE00"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"type":"reasoning","content":[{"type":"reasoning_text","text":"😀"}]}`
	if got != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

func TestCanonicalReasoningSignatureRejectsMalformedKnownFields(t *testing.T) {
	for _, raw := range []string{
		`{"type":"reasoning","id":7}`,
		`{"type":"reasoning","summary":null}`,
		`{"type":"reasoning","summary":[{"type":"wrong","text":"x"}]}`,
		`{"type":"reasoning","content":[{"type":"reasoning_text","text":7}]}`,
		`{"type":"reasoning","encrypted_content":null}`,
	} {
		if _, err := canonicalReasoningSignature(json.RawMessage(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}
