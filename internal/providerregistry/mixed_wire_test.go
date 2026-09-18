package providerregistry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/transport"
)

type recordingWire struct {
	name  string
	calls *[]string
	err   error
}

func (w recordingWire) Open(_ context.Context, _ providers.DispatchRequest) (providers.EventStream, error) {
	*w.calls = append(*w.calls, w.name)
	if w.err != nil {
		return nil, w.err
	}
	return eofStream{}, nil
}

type eofStream struct{}

func (eofStream) Next() (protocol.Event, error) { return protocol.Event{}, io.EOF }
func (eofStream) Close() error                  { return nil }

func TestMixedWireSelectsResponsesBeforeSendAndDoesNotRetry(t *testing.T) {
	var calls []string
	chatErr := errors.New("chat failed")
	provider := mixedWireProvider{
		chat:      recordingWire{name: "chat", calls: &calls, err: chatErr},
		responses: recordingWire{name: "responses", calls: &calls},
	}

	if _, err := provider.Open(t.Context(), providers.DispatchRequest{
		Parsed: protocol.ParsedRequest{UpstreamModelID: "gpt-5.6-luna"},
	}); err != nil {
		t.Fatalf("Open gpt-5.6-luna: %v", err)
	}
	if len(calls) != 1 || calls[0] != "responses" {
		t.Fatalf("gpt-5.6-luna calls=%v", calls)
	}

	calls = nil
	if _, err := provider.Open(t.Context(), providers.DispatchRequest{
		Parsed: protocol.ParsedRequest{ModelID: "kimi-k3"},
	}); err == nil || !errors.Is(err, chatErr) {
		t.Fatalf("Open kimi-k3 err=%v", err)
	}
	if len(calls) != 1 || calls[0] != "chat" {
		t.Fatalf("kimi-k3 calls=%v (must not retry Responses)", calls)
	}
}

func TestMixedWirePrefersUpstreamModelID(t *testing.T) {
	var calls []string
	provider := mixedWireProvider{
		chat:      recordingWire{name: "chat", calls: &calls},
		responses: recordingWire{name: "responses", calls: &calls},
	}
	if _, err := provider.Open(t.Context(), providers.DispatchRequest{
		Parsed: protocol.ParsedRequest{ModelID: "kimi-k3", UpstreamModelID: "grok-4.6"},
	}); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if len(calls) != 1 || calls[0] != "responses" {
		t.Fatalf("calls=%v", calls)
	}
}

func TestSiblingEndpointsFromChat(t *testing.T) {
	responses, err := responsesEndpointFromChat("https://opencode.ai/zen/go/v1/chat/completions")
	if err != nil {
		t.Fatal(err)
	}
	if responses != "https://opencode.ai/zen/go/v1/responses" {
		t.Fatalf("responses=%q", responses)
	}
	messages, err := messagesEndpointFromChat("https://opencode.ai/zen/go/v1/chat/completions")
	if err != nil {
		t.Fatal(err)
	}
	if messages != "https://opencode.ai/zen/go/v1/messages" {
		t.Fatalf("messages=%q", messages)
	}
	if _, err := responsesEndpointFromChat("https://opencode.ai/zen/go/v1"); err == nil {
		t.Fatal("expected responses derivation error")
	}
	if _, err := messagesEndpointFromChat("https://opencode.ai/zen/go/v1"); err == nil {
		t.Fatal("expected messages derivation error")
	}
}

func TestBuildRoutesOpenCodeGoByExactModelWire(t *testing.T) {
	var paths []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "text/event-stream")
		if strings.HasSuffix(r.URL.Path, "/responses") {
			fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n")
			return
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"pong\"},\"finish_reason\":\"stop\"}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()

	registry, err := Build(context.Background(), []Spec{{
		ID:                "opencode-go",
		Protocol:          ProtocolOpenAIChat,
		Endpoint:          upstream.URL + "/v1/chat/completions",
		APIKey:            "test-key",
		DestinationPolicy: transport.DestinationPolicy{AllowPrivateNetwork: true},
	}}, Options{})
	if err != nil {
		t.Fatal(err)
	}

	open := func(model string) {
		t.Helper()
		stream, err := registry["opencode-go"].Open(t.Context(), providers.DispatchRequest{
			Parsed: protocol.ParsedRequest{
				Source:          protocol.RequestSourceChatCompletions,
				UpstreamModelID: model,
				Context: protocol.Context{Messages: []protocol.Message{{
					Role:    protocol.RoleUser,
					Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}},
				}}},
			},
		})
		if err != nil {
			t.Fatalf("Open(%s): %v", model, err)
		}
		defer stream.Close()
		if _, err := stream.Next(); err != nil && !errors.Is(err, io.EOF) {
			t.Fatalf("Next(%s): %v", model, err)
		}
	}

	open("gpt-5.6-luna")
	open("kimi-k3")
	if len(paths) != 2 || !strings.HasSuffix(paths[0], "/v1/responses") || !strings.HasSuffix(paths[1], "/v1/chat/completions") {
		t.Fatalf("paths=%v", paths)
	}
}

func TestBuildLeavesUnrelatedChatProviderOnChatForGrok(t *testing.T) {
	var paths []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"pong\"},\"finish_reason\":\"stop\"}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()

	registry, err := Build(context.Background(), []Spec{{
		ID:                "openrouter",
		Protocol:          ProtocolOpenAIChat,
		Endpoint:          upstream.URL + "/v1/chat/completions",
		APIKey:            "test-key",
		DestinationPolicy: transport.DestinationPolicy{AllowPrivateNetwork: true},
	}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := registry["openrouter"].Open(t.Context(), providers.DispatchRequest{
		Parsed: protocol.ParsedRequest{
			Source:          protocol.RequestSourceChatCompletions,
			UpstreamModelID: "grok-4.5",
			Context: protocol.Context{Messages: []protocol.Message{{
				Role:    protocol.RoleUser,
				Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}},
			}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if _, err := stream.Next(); err != nil && !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
	if len(paths) != 1 || !strings.HasSuffix(paths[0], "/v1/chat/completions") {
		t.Fatalf("paths=%v", paths)
	}
}

func TestBuildFailsClosedWhenOpenCodeGoChatEndpointCannotDeriveResponses(t *testing.T) {
	upstream := httptest.NewServer(nil)
	defer upstream.Close()
	_, err := Build(context.Background(), []Spec{{
		ID:                "opencode-go",
		Protocol:          ProtocolOpenAIChat,
		Endpoint:          upstream.URL,
		APIKey:            "test-key",
		DestinationPolicy: transport.DestinationPolicy{AllowPrivateNetwork: true},
	}}, Options{})
	if err == nil || !strings.Contains(err.Error(), "Responses endpoint") {
		t.Fatalf("err=%v", err)
	}
}
