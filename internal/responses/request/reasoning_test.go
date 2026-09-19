package request

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	benesreasoning "github.com/Wibias/Benes/internal/responses/reasoning"
)

func reasoningItem(t *testing.T, value string) Item {
	t.Helper()
	var raw map[string]any
	if err := json.Unmarshal([]byte(value), &raw); err != nil {
		t.Fatal(err)
	}
	return Item{Type: "reasoning", Raw: json.RawMessage(value)}
}

func envelope(t *testing.T, e benesreasoning.Envelope) string {
	t.Helper()
	got, err := benesreasoning.Encode(e)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestDecodeRequestFeedsReasoningWireDecoder(t *testing.T) {
	enc := envelope(t, benesreasoning.Envelope{Signature: "sig", Text: "hidden"})
	body := `{"model":"gpt-5","input":[{"type":"reasoning","id":"rs_1","summary":[],"encrypted_content":` + mustJSON(t, enc) + `}]}`
	req, err := Decode(strings.NewReader(body), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Input.Items) != 1 {
		t.Fatalf("items=%d", len(req.Input.Items))
	}
	got, ok, err := req.Input.Items[0].DecodeReasoning()
	if err != nil {
		t.Fatal(err)
	}
	if !ok || got.ItemID != "rs_1" || got.Signature != "sig" || got.EffectiveThinkingText != "hidden" {
		t.Fatalf("got %+v ok=%v", got, ok)
	}
}

func TestDecodeReasoningPrefersSummaryOverContentLikeTypeScript(t *testing.T) {
	item := reasoningItem(t, `{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"sum "},{"text":"mary"}],"content":[{"type":"reasoning_text","text":"raw"}]}`)
	got, ok, err := item.DecodeReasoning()
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("reasoning item not recognized")
	}
	if got.ItemID != "rs_1" || got.VisibleText != "sum mary" || got.EffectiveThinkingText != "sum mary" {
		t.Fatalf("got %+v", got)
	}
}

func TestDecodeReasoningFallsBackToContentText(t *testing.T) {
	item := reasoningItem(t, `{"type":"reasoning","content":[{"type":"reasoning_text","text":"raw-a"},{"text":7},{"text":"raw-b"}]}`)
	got, ok, err := item.DecodeReasoning()
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("reasoning item not recognized")
	}
	if got.VisibleText != "raw-araw-b" || got.EffectiveThinkingText != "raw-araw-b" {
		t.Fatalf("got %+v", got)
	}
}

func TestDecodeReasoningEnvelopeHiddenTextOverridesVisibleText(t *testing.T) {
	enc := envelope(t, benesreasoning.Envelope{Signature: "sig", Redacted: []string{"r1", "r2"}, Text: "hidden"})
	item := reasoningItem(t, `{"type":"reasoning","id":"rs_1","summary":[{"text":"visible"}],"encrypted_content":`+mustJSON(t, enc)+`}`)
	got, ok, err := item.DecodeReasoning()
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("reasoning item not recognized")
	}
	if !got.HasEnvelope || got.EffectiveThinkingText != "hidden" || got.Signature != "sig" {
		t.Fatalf("got %+v", got)
	}
	if len(got.Redacted) != 2 || got.Redacted[0] != "r1" || got.Redacted[1] != "r2" {
		t.Fatalf("redacted=%q", got.Redacted)
	}
}

func TestDecodeReasoningKiroOnlyIsProviderStateNotThinkingText(t *testing.T) {
	enc := envelope(t, benesreasoning.Envelope{KiroRedacted: "kms"})
	item := reasoningItem(t, `{"type":"reasoning","encrypted_content":`+mustJSON(t, enc)+`}`)
	got, ok, err := item.DecodeReasoning()
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("reasoning item not recognized")
	}
	if !got.HasEnvelope || got.KiroReasoning.Member != protocol.KiroReasoningRedactedContent || got.KiroReasoning.Value != "kms" || got.EffectiveThinkingText != "" {
		t.Fatalf("got %+v", got)
	}
}

func TestDecodeReasoningNativeEncryptedOnlyStaysOpaque(t *testing.T) {
	item := reasoningItem(t, `{"type":"reasoning","id":"rs_native","encrypted_content":"native-secret"}`)
	got, ok, err := item.DecodeReasoning()
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("reasoning item not recognized")
	}
	if got.HasEnvelope || got.EncryptedContent != "native-secret" || got.EffectiveThinkingText != "" || got.Signature != "" || len(got.Redacted) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestDecodeReasoningMalformedEncryptedEnvelopeDoesNotInventReplay(t *testing.T) {
	item := reasoningItem(t, `{"type":"reasoning","summary":[{"text":"visible"}],"encrypted_content":"benesr1:%%%"}`)
	got, ok, err := item.DecodeReasoning()
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("reasoning item not recognized")
	}
	if got.HasEnvelope || got.EffectiveThinkingText != "visible" || got.EncryptedContent != "benesr1:%%%" {
		t.Fatalf("got %+v", got)
	}
}

func TestDecodeReasoningWrongItemTypeDoesNotParse(t *testing.T) {
	item := Item{Type: "message", Raw: json.RawMessage(`{"type":"message"}`)}
	if _, ok, err := item.DecodeReasoning(); err != nil || ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}

func TestDecodeReasoningMalformedRawFailsClosedWithoutPanic(t *testing.T) {
	item := Item{Type: "reasoning", Raw: json.RawMessage(`{"type":"reasoning"`)}
	if _, ok, err := item.DecodeReasoning(); !ok || err == nil {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}

func TestDecodeReasoningMalformedKnownFieldReturnsError(t *testing.T) {
	item := reasoningItem(t, `{"type":"reasoning","summary":{"text":"not-an-array"}}`)
	if _, ok, err := item.DecodeReasoning(); !ok || err == nil {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}

func mustJSON(t *testing.T, v string) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestDecodeReasoningUnknownKiroMemberDoesNotInventReplay(t *testing.T) {
	encrypted := benesreasoning.Prefix + base64.StdEncoding.EncodeToString([]byte(`{"krc":"opaque","krk":"unknown"}`))
	item := reasoningItem(t, `{"type":"reasoning","summary":[{"text":"visible"}],"encrypted_content":`+mustJSON(t, encrypted)+`}`)
	got, ok, err := item.DecodeReasoning()
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if got.HasEnvelope || got.KiroReasoning.Value != "" || got.EffectiveThinkingText != "visible" {
		t.Fatalf("got=%+v", got)
	}
}

