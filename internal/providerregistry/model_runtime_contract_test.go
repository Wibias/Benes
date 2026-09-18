package providerregistry

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/providers/openairesponses"
)

func TestModelRuntimeContractIsExplicitAndConservative(t *testing.T) {
	for _, protocolID := range []Protocol{
		ProtocolOpenAIResponses, ProtocolOpenAIChat, ProtocolAnthropicMessages,
		ProtocolGoogleAntigravity, ProtocolGoogle, ProtocolGoogleVertex, ProtocolKiro,
	} {
		contract, ok := ModelRuntimeContract(protocolID)
		if !ok {
			t.Fatalf("missing runtime contract for %q", protocolID)
		}
		if len(contract.APITypes) != 3 {
			t.Fatalf("api contract %q=%+v", protocolID, contract)
		}
		if contract.SupportsToolUse != nil || contract.SupportsStreaming != nil {
			t.Fatalf("protocol %q fabricated model-level tool/stream certainty: %+v", protocolID, contract)
		}
	}
	cursor, ok := ModelRuntimeContract(ProtocolCursor)
	if !ok || len(cursor.APITypes) != 3 || cursor.SupportsToolUse == nil || *cursor.SupportsToolUse || cursor.SupportsStreaming != nil {
		t.Fatalf("cursor contract=%+v ok=%v", cursor, ok)
	}
	if _, ok := ModelRuntimeContract(Protocol("future-unknown")); ok {
		t.Fatal("unknown protocol received an advertised runtime contract")
	}
}

func TestOpenAIResponsesAdvertisedAPITypesReachActualOpenPath(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n")
	}))
	defer upstream.Close()
	client, err := openairesponses.New(openairesponses.Config{
		Endpoint: upstream.URL, APIKey: "k", HTTPClient: upstream.Client(),
		MaxStreamBytes: 1 << 20, InactivityTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}

	contract, ok := ModelRuntimeContract(ProtocolOpenAIResponses)
	if !ok {
		t.Fatal("missing OpenAI Responses runtime contract")
	}
	wantSources := map[string]protocol.RequestSource{
		"responses":          protocol.RequestSourceResponses,
		"chat_completions":   protocol.RequestSourceChatCompletions,
		"anthropic_messages": protocol.RequestSourceAnthropicMessages,
	}
	for _, apiType := range contract.APITypes {
		source, ok := wantSources[apiType]
		if !ok {
			t.Fatalf("unexpected api type %q", apiType)
		}
		t.Run(apiType, func(t *testing.T) {
			req := protocol.ParsedRequest{
				Source: source, ModelID: "openai/gpt-test", UpstreamModelID: "gpt-test", Stream: true,
				Context: protocol.Context{Messages: []protocol.Message{{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}}}}},
			}
			if source == protocol.RequestSourceResponses {
				req.Raw = []byte(`{"model":"openai/gpt-test","input":"hi"}`)
			}
			stream, err := client.Open(context.Background(), providers.DispatchRequest{Parsed: req})
			if err != nil {
				t.Fatalf("Open(%s): %v", source, err)
			}
			defer stream.Close()
			if _, err := stream.Next(); err != nil {
				t.Fatalf("Next(%s): %v", source, err)
			}
		})
	}
}
