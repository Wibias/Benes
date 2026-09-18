package openaichat

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
)

func TestClientPreservesReasoningOnlyForConfiguredModels(t *testing.T) {
	bodies := make(chan map[string]any, 2)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		bodies <- body
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()

	client, err := New(Config{
		Endpoint:                       upstream.URL,
		APIKey:                         "test-key",
		HTTPClient:                     upstream.Client(),
		PreserveReasoningContentModels: []string{"deepseek-r1"},
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, model := range []string{"deepseek-r1:14b", "deepseek-chat"} {
		stream, err := client.Open(t.Context(), providers.DispatchRequest{Parsed: reasoningCapabilityRequest(model)})
		if err != nil {
			t.Fatalf("Open(%q): %v", model, err)
		}
		_ = stream.Close()
	}

	preserved := <-bodies
	preservedMessage := preserved["messages"].([]any)[0].(map[string]any)
	if preservedMessage["reasoning_content"] != "think" {
		t.Fatalf("preserved message=%#v", preservedMessage)
	}

	notPreserved := <-bodies
	notPreservedMessage := notPreserved["messages"].([]any)[0].(map[string]any)
	if _, ok := notPreservedMessage["reasoning_content"]; ok {
		t.Fatalf("unconfigured model received reasoning_content: %#v", notPreservedMessage)
	}
	if notPreservedMessage["content"] != "answer" {
		t.Fatalf("unconfigured message=%#v", notPreservedMessage)
	}
}

func TestClientSendsReasoningSplitOnlyForConfiguredModels(t *testing.T) {
	bodies := make(chan map[string]any, 2)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		bodies <- body
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()

	client, err := New(Config{
		Endpoint:             upstream.URL,
		APIKey:               "test-key",
		HTTPClient:           upstream.Client(),
		ReasoningSplitModels: []string{"MiniMax-M2.7"},
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, model := range []string{"MiniMax-M2.7", "deepseek-r1"} {
		stream, err := client.Open(t.Context(), providers.DispatchRequest{Parsed: reasoningCapabilityRequest(model)})
		if err != nil {
			t.Fatalf("Open(%q): %v", model, err)
		}
		_ = stream.Close()
	}

	split := <-bodies
	if split["reasoning_split"] != true {
		t.Fatalf("minimax body=%#v", split)
	}
	other := <-bodies
	if _, exists := other["reasoning_split"]; exists {
		t.Fatalf("other model received reasoning_split: %#v", other)
	}
}

func reasoningCapabilityRequest(model string) protocol.ParsedRequest {
	return protocol.ParsedRequest{
		UpstreamModelID: model,
		Context: protocol.Context{Messages: []protocol.Message{{
			Role: protocol.RoleAssistant,
			Content: []protocol.ContentPart{
				{Type: protocol.ContentThinking, Thinking: "think"},
				{Type: protocol.ContentText, Text: "answer"},
			},
		}}},
	}
}
