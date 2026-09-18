package providerregistry

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/transport"
)

func TestBuildPropagatesModelScopedChatReasoningCapabilities(t *testing.T) {
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

	registry, err := Build(context.Background(), []Spec{{
		ID:                "gateway",
		Protocol:          ProtocolOpenAIChat,
		Endpoint:          upstream.URL,
		APIKey:            "test-key",
		DestinationPolicy: transport.DestinationPolicy{AllowPrivateNetwork: true},
		Chat: ChatOptions{
			PreserveReasoningContentModels: []string{"deepseek-r1"},
		},
	}}, Options{})
	if err != nil {
		t.Fatal(err)
	}

	provider := registry["gateway"]
	for _, model := range []string{"deepseek-r1:14b", "deepseek-chat"} {
		stream, err := provider.Open(t.Context(), providers.DispatchRequest{Parsed: reasoningRegistryRequest(model)})
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
}

func reasoningRegistryRequest(model string) protocol.ParsedRequest {
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
