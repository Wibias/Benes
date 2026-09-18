package openaichat

import (
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
)

func TestStreamParsesIncrementalReasoningDetails(t *testing.T) {
	events := collectChatEvents(t, chatSSE(
		`{"choices":[{"delta":{"reasoning_details":[{"type":"reasoning.text","id":"rs_1","format":"MiniMax-response-v1","index":0,"text":"The user"}]},"finish_reason":null}]}`,
		`{"choices":[{"delta":{"reasoning_details":[{"type":"reasoning.text","id":"rs_1","format":"MiniMax-response-v1","index":0,"text":" is asking"}]},"finish_reason":null}]}`,
		`{"choices":[{"delta":{"content":"hello"},"finish_reason":"stop"}]}`,
		`[DONE]`,
	))
	if len(events) != 4 {
		t.Fatalf("events=%#v", events)
	}
	if events[0].Type != protocol.EventReasoningRawDelta || events[0].Text != "The user" {
		t.Fatalf("first=%#v", events[0])
	}
	if events[1].Type != protocol.EventReasoningRawDelta || events[1].Text != " is asking" {
		t.Fatalf("second=%#v", events[1])
	}
	detail, ok := protocol.MiniMaxReasoningDetailFromEvent(events[0])
	if !ok || detail.ID != "rs_1" || detail.Type != "reasoning.text" || detail.Format != "MiniMax-response-v1" || detail.Index == nil || *detail.Index != 0 {
		t.Fatalf("detail=%#v ok=%v", detail, ok)
	}
	if events[2].Type != protocol.EventTextDelta || events[2].Text != "hello" {
		t.Fatalf("text=%#v", events[2])
	}
	if events[3].Type != protocol.EventDone {
		t.Fatalf("done=%#v", events[3])
	}
}

func TestStreamDoesNotDuplicateReasoningContentWhenDetailsPresent(t *testing.T) {
	events := collectChatEvents(t, chatSSE(
		`{"choices":[{"delta":{"reasoning_content":"The user","reasoning_details":[{"type":"reasoning.text","id":"rs_1","format":"MiniMax-response-v1","index":0,"text":"The user"}]},"finish_reason":null}]}`,
		`{"choices":[{"delta":{"reasoning_content":"The user is asking","reasoning_details":[{"type":"reasoning.text","id":"rs_1","format":"MiniMax-response-v1","index":0,"text":" is asking"}]},"finish_reason":null}]}`,
		`{"choices":[{"delta":{"content":"hello"},"finish_reason":"stop"}]}`,
		`[DONE]`,
	))
	if len(events) != 4 {
		t.Fatalf("events=%#v", events)
	}
	if events[0].Type != protocol.EventReasoningRawDelta || events[0].Text != "The user" {
		t.Fatalf("first=%#v", events[0])
	}
	if events[1].Type != protocol.EventReasoningRawDelta || events[1].Text != " is asking" {
		t.Fatalf("second=%#v", events[1])
	}
	detail, ok := protocol.MiniMaxReasoningDetailFromEvent(events[0])
	if !ok || detail.ID != "rs_1" {
		t.Fatalf("detail=%#v ok=%v", detail, ok)
	}
	if events[2].Type != protocol.EventTextDelta || events[2].Text != "hello" {
		t.Fatalf("text=%#v", events[2])
	}
}

func TestStreamDiffsCumulativeReasoningDetails(t *testing.T) {
	events := collectChatEvents(t, chatSSE(
		`{"choices":[{"delta":{"reasoning_details":[{"id":"rs_1","text":"The user"}]},"finish_reason":null}]}`,
		`{"choices":[{"delta":{"reasoning_details":[{"id":"rs_1","text":"The user is asking"}]},"finish_reason":"stop"}]}`,
		`[DONE]`,
	))
	if len(events) != 3 {
		t.Fatalf("events=%#v", events)
	}
	if events[0].Text != "The user" || events[1].Text != " is asking" {
		t.Fatalf("texts=%q %q", events[0].Text, events[1].Text)
	}
}

func TestStreamMalformedReasoningDetailsFailsClosed(t *testing.T) {
	events := collectChatEvents(t, chatSSE(
		`{"choices":[{"delta":{"reasoning_details":{"text":"nope"}},"finish_reason":null}]}`,
	))
	if len(events) != 1 || events[0].Type != protocol.EventError || !strings.Contains(events[0].Message, "reasoning_details") {
		t.Fatalf("events=%#v", events)
	}
}

func TestCompileEmitsReasoningDetailsAndSplit(t *testing.T) {
	index := 0
	req := protocol.ParsedRequest{
		UpstreamModelID: "MiniMax-M2.7",
		Stream:          true,
		Context: protocol.Context{Messages: []protocol.Message{{
			Role: protocol.RoleAssistant,
			Content: []protocol.ContentPart{{
				Type:     protocol.ContentThinking,
				Thinking: "The user is asking",
				ProviderMetadata: &protocol.ProviderOpaqueMetadata{MiniMax: &protocol.MiniMaxOpaqueMetadata{
					ReasoningDetails: []protocol.MiniMaxReasoningDetail{{
						Type:   "reasoning.text",
						ID:     "rs_1",
						Format: "MiniMax-response-v1",
						Index:  &index,
						Text:   "The user is asking",
					}},
				}},
			}, {
				Type: protocol.ContentText,
				Text: "hello",
			}},
		}}},
	}
	raw, err := Compile(req, CompileOptions{PreserveReasoningContent: true, ReasoningSplit: true})
	if err != nil {
		t.Fatal(err)
	}
	body := decodeBody(t, raw)
	if body["reasoning_split"] != true {
		t.Fatalf("body=%s", raw)
	}
	msg := body["messages"].([]any)[0].(map[string]any)
	if msg["content"] != "hello" || msg["reasoning_content"] != "The user is asking" {
		t.Fatalf("msg=%#v", msg)
	}
	details, ok := msg["reasoning_details"].([]any)
	if !ok || len(details) != 1 {
		t.Fatalf("details=%#v", msg["reasoning_details"])
	}
	detail := details[0].(map[string]any)
	if detail["id"] != "rs_1" || detail["type"] != "reasoning.text" || detail["format"] != "MiniMax-response-v1" || detail["text"] != "The user is asking" {
		t.Fatalf("detail=%#v", detail)
	}
}

func TestCompileOmitsReasoningDetailsWithoutSplit(t *testing.T) {
	index := 0
	req := protocol.ParsedRequest{
		UpstreamModelID: "deepseek-r1",
		Context: protocol.Context{Messages: []protocol.Message{{
			Role: protocol.RoleAssistant,
			Content: []protocol.ContentPart{{
				Type:     protocol.ContentThinking,
				Thinking: "The user is asking",
				ProviderMetadata: &protocol.ProviderOpaqueMetadata{MiniMax: &protocol.MiniMaxOpaqueMetadata{
					ReasoningDetails: []protocol.MiniMaxReasoningDetail{{
						Type:  "reasoning.text",
						ID:    "rs_1",
						Index: &index,
						Text:  "The user is asking",
					}},
				}},
			}, {
				Type: protocol.ContentText,
				Text: "hello",
			}},
		}}},
	}
	raw, err := Compile(req, CompileOptions{PreserveReasoningContent: true})
	if err != nil {
		t.Fatal(err)
	}
	body := decodeBody(t, raw)
	msg := body["messages"].([]any)[0].(map[string]any)
	if msg["content"] != "hello" || msg["reasoning_content"] != "The user is asking" {
		t.Fatalf("msg=%#v", msg)
	}
	if _, exists := msg["reasoning_details"]; exists {
		t.Fatalf("unexpected reasoning_details in %#v", msg)
	}
}

func TestCompileOmitsReasoningSplitForOtherModels(t *testing.T) {
	req := protocol.ParsedRequest{
		UpstreamModelID: "deepseek-r1",
		Context: protocol.Context{Messages: []protocol.Message{{
			Role:    protocol.RoleUser,
			Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}},
		}}},
	}
	raw, err := Compile(req, CompileOptions{PreserveReasoningContent: true})
	if err != nil {
		t.Fatal(err)
	}
	body := decodeBody(t, raw)
	if _, exists := body["reasoning_split"]; exists {
		t.Fatalf("unexpected reasoning_split in %s", raw)
	}
}

func TestUnknownReasoningDetailTypeIsPreserved(t *testing.T) {
	events := collectChatEvents(t, chatSSE(
		`{"choices":[{"delta":{"reasoning_details":[{"type":"reasoning.unknown","id":"rs_x","text":"opaque"}]},"finish_reason":"stop"}]}`,
		`[DONE]`,
	))
	if len(events) != 2 || events[0].Text != "opaque" {
		t.Fatalf("events=%#v", events)
	}
	detail, ok := protocol.MiniMaxReasoningDetailFromEvent(events[0])
	if !ok || detail.Type != "reasoning.unknown" || detail.ID != "rs_x" {
		t.Fatalf("detail=%#v ok=%v", detail, ok)
	}
}
