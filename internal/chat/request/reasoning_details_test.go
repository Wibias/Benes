package request

import (
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
)

func TestDecodeAssistantReasoningDetails(t *testing.T) {
	body := `{
		"model":"minimax/MiniMax-M2.7",
		"messages":[
			{"role":"user","content":"hi"},
			{"role":"assistant","content":"hello","reasoning_content":"The user is asking","reasoning_details":[{"type":"reasoning.text","id":"rs_1","format":"MiniMax-response-v1","index":0,"text":"The user is asking"}]}
		]
	}`
	got, err := Decode(strings.NewReader(body), 1<<20, DecodeOptions{NowMillis: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Context.Messages) != 2 {
		t.Fatalf("messages=%#v", got.Context.Messages)
	}
	assistant := got.Context.Messages[1]
	if len(assistant.Content) != 2 {
		t.Fatalf("content=%#v", assistant.Content)
	}
	if assistant.Content[0].Type != protocol.ContentThinking || assistant.Content[0].Thinking != "The user is asking" {
		t.Fatalf("thinking=%#v", assistant.Content[0])
	}
	meta := assistant.Content[0].ProviderMetadata
	if meta == nil || meta.MiniMax == nil || len(meta.MiniMax.ReasoningDetails) != 1 {
		t.Fatalf("meta=%#v", meta)
	}
	detail := meta.MiniMax.ReasoningDetails[0]
	if detail.ID != "rs_1" || detail.Type != "reasoning.text" || detail.Format != "MiniMax-response-v1" || detail.Text != "The user is asking" || detail.Index == nil || *detail.Index != 0 {
		t.Fatalf("detail=%#v", detail)
	}
	if assistant.Content[1].Type != protocol.ContentText || assistant.Content[1].Text != "hello" {
		t.Fatalf("text=%#v", assistant.Content[1])
	}
}

func TestDecodeMalformedReasoningDetailsDoesNotCorruptContent(t *testing.T) {
	body := `{
		"model":"minimax/MiniMax-M2.7",
		"messages":[
			{"role":"assistant","content":"hello","reasoning_details":{"text":"nope"}}
		]
	}`
	got, err := Decode(strings.NewReader(body), 1<<20, DecodeOptions{NowMillis: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Context.Messages) != 1 || len(got.Context.Messages[0].Content) != 1 {
		t.Fatalf("messages=%#v", got.Context.Messages)
	}
	if got.Context.Messages[0].Content[0].Type != protocol.ContentText || got.Context.Messages[0].Content[0].Text != "hello" {
		t.Fatalf("content=%#v", got.Context.Messages[0].Content)
	}
}
