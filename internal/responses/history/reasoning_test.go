package history

import (
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/responses/reasoning"
	"github.com/Wibias/Benes/internal/responses/request"
)

func TestUnsignedReasoningSiblingsMergeWithNewline(t *testing.T) {
	in := request.Input{Items: []request.Item{
		item(t, `{"type":"reasoning","id":"r1","summary":[{"type":"summary_text","text":"one"}]}`),
		item(t, `{"type":"reasoning","id":"r2","summary":[{"type":"summary_text","text":"two"}]}`),
		item(t, `{"role":"assistant","content":"answer"}`),
	}}
	ctx := parse(t, in)
	m := ctx.Messages[0]
	if len(m.Content) != 2 || m.Content[0].Type != protocol.ContentThinking || m.Content[0].Thinking != "one\ntwo" || m.Content[0].ItemID != "r2" {
		t.Fatalf("%+v", m.Content)
	}
}

func TestSignedReasoningSiblingsStaySeparate(t *testing.T) {
	e1 := enc(t, reasoning.Envelope{Signature: "s1"})
	e2 := enc(t, reasoning.Envelope{Signature: "s2"})
	in := request.Input{Items: []request.Item{
		item(t, `{"type":"reasoning","summary":[{"type":"summary_text","text":"one"}],"encrypted_content":`+j(t, e1)+`}`),
		item(t, `{"type":"reasoning","summary":[{"type":"summary_text","text":"two"}],"encrypted_content":`+j(t, e2)+`}`),
		item(t, `{"role":"assistant","content":"answer"}`),
	}}
	ctx := parse(t, in)
	c := ctx.Messages[0].Content
	if len(c) != 3 || c[0].Signature != "s1" || c[1].Signature != "s2" {
		t.Fatalf("%+v", c)
	}
}

func TestHiddenTextAndKiroReplay(t *testing.T) {
	hidden := enc(t, reasoning.Envelope{Signature: "sig", Text: "secret"})
	kiro := enc(t, reasoning.Envelope{KiroRedacted: "kms"})
	in := request.Input{Items: []request.Item{
		item(t, `{"type":"reasoning","summary":[],"encrypted_content":`+j(t, hidden)+`}`),
		item(t, `{"role":"assistant","content":"answer"}`),
		item(t, `{"type":"reasoning","encrypted_content":`+j(t, kiro)+`}`),
	}}
	ctx := parse(t, in)
	m := ctx.Messages[0]
	if m.Content[0].Thinking != "secret" || m.KiroRedactedReasoning != "kms" {
		t.Fatalf("%+v", m)
	}
}

func TestNativeEncryptedOnlyDoesNotCreateTurn(t *testing.T) {
	ctx := parse(t, request.Input{Items: []request.Item{item(t, `{"type":"reasoning","encrypted_content":"native"}`)}})
	if len(ctx.Messages) != 0 {
		t.Fatalf("%+v", ctx.Messages)
	}
}

func TestReasoningFallbackSignatureMatchesJSONStringifyKnownShape(t *testing.T) {
	raw := `{"type":"reasoning","unknown":"drop","id":"r","summary":[],"content":[{"type":"reasoning_text","text":"<&>  "}],"encrypted_content":"opaque"}`
	ctx := parse(t, request.Input{Items: []request.Item{item(t, raw), item(t, `{"role":"assistant","content":"x"}`)}})
	got := ctx.Messages[0].Content[0].Signature
	want := "{\"type\":\"reasoning\",\"id\":\"r\",\"summary\":[],\"content\":[{\"type\":\"reasoning_text\",\"text\":\"<&>  \"}],\"encrypted_content\":\"opaque\"}"
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestReasoningFallbackSignaturePreservesLoneSurrogate(t *testing.T) {
	raw := `{"type":"reasoning","summary":[{"type":"summary_text","text":"a\/b\u0008\ud83d\ude00\ud800"}]}`
	ctx := parse(t, request.Input{Items: []request.Item{item(t, raw), item(t, `{"role":"assistant","content":"x"}`)}})
	got := ctx.Messages[0].Content[0].Signature
	want := `{"type":"reasoning","summary":[{"type":"summary_text","text":"a/b\b😀\ud800"}]}`
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestMalformedKnownReasoningFailsClosed(t *testing.T) {
	_, err := Build(&request.Request{Model: "m", Input: request.Input{Items: []request.Item{item(t, `{"type":"reasoning","summary":null}`)}}}, 1)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestReasoningFallbackSignatureMatchesJSONStringifyEscapes(t *testing.T) {
	raw := `{"type":"reasoning","summary":[{"type":"summary_text","text":"a\/b\u0008\ud83d\ude00\u2028","unknown":"drop"}],"unknown":"drop"}`
	ctx := parse(t, request.Input{Items: []request.Item{item(t, raw), item(t, `{"role":"assistant","content":"x"}`)}})
	got := ctx.Messages[0].Content[0].Signature
	want := "{\"type\":\"reasoning\",\"summary\":[{\"type\":\"summary_text\",\"text\":\"a/b\\b😀 \"}]}"
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}
