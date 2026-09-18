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

func TestBuildDefaultsReasoningPlaceholderModelsToPreserveList(t *testing.T) {
	body := capturePlaceholderBody(t, ChatOptions{
		PreserveReasoningContentModels: []string{"deepseek-r1"},
	}, toolRoundWithoutReasoning("deepseek-r1"))

	messages := body["messages"].([]any)
	assistant := messages[0].(map[string]any)
	if assistant["reasoning_content"] != " " {
		t.Fatalf("assistant=%#v", assistant)
	}
}

func TestBuildAllowsExplicitPlaceholderOptOut(t *testing.T) {
	body := capturePlaceholderBody(t, ChatOptions{
		PreserveReasoningContentModels:     []string{"minimax-thinking"},
		RequiresReasoningPlaceholderModels: []string{},
	}, toolRoundWithoutReasoning("minimax-thinking"))

	messages := body["messages"].([]any)
	assistant := messages[0].(map[string]any)
	if _, exists := assistant["reasoning_content"]; exists {
		t.Fatalf("assistant=%#v", assistant)
	}
}

func TestBuildRepairsOrphanToolResultWithRequiredReasoningPlaceholder(t *testing.T) {
	body := capturePlaceholderBody(t, ChatOptions{
		PreserveReasoningContentModels: []string{"deepseek-r1"},
	}, protocol.ParsedRequest{
		UpstreamModelID: "deepseek-r1",
		Context: protocol.Context{Messages: []protocol.Message{{
			Role:          protocol.RoleToolResult,
			ToolCallID:    "orphan",
			ToolName:      "search",
			ToolNamespace: "mcp",
			Content:       []protocol.ContentPart{{Type: protocol.ContentText, Text: "result"}},
		}}},
	})

	messages := body["messages"].([]any)
	if len(messages) != 2 {
		t.Fatalf("messages=%#v", messages)
	}
	assistant := messages[0].(map[string]any)
	if assistant["role"] != "assistant" || assistant["reasoning_content"] != " " {
		t.Fatalf("assistant=%#v", assistant)
	}
	calls := assistant["tool_calls"].([]any)
	if calls[0].(map[string]any)["id"] != "orphan" {
		t.Fatalf("calls=%#v", calls)
	}
}

func capturePlaceholderBody(t *testing.T, chat ChatOptions, request protocol.ParsedRequest) map[string]any {
	t.Helper()
	bodies := make(chan map[string]any, 1)
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
		Chat:              chat,
	}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := registry["gateway"].Open(t.Context(), providers.DispatchRequest{Parsed: request})
	if err != nil {
		t.Fatal(err)
	}
	_ = stream.Close()
	return <-bodies
}

func toolRoundWithoutReasoning(model string) protocol.ParsedRequest {
	return protocol.ParsedRequest{
		UpstreamModelID: model,
		Context: protocol.Context{Messages: []protocol.Message{
			{
				Role: protocol.RoleAssistant,
				Content: []protocol.ContentPart{{
					Type:       protocol.ContentToolCall,
					ToolCallID: "c1",
					ToolName:   "search",
					Arguments:  map[string]any{"q": "x"},
				}},
			},
			{
				Role:       protocol.RoleToolResult,
				ToolCallID: "c1",
				ToolName:   "search",
				Content:    []protocol.ContentPart{{Type: protocol.ContentText, Text: "ok"}},
			},
		}},
	}
}
