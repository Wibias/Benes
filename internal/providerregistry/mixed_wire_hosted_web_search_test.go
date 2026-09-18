package providerregistry

import (
	"context"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
)

type captureWire struct {
	called   int
	dispatch providers.DispatchRequest
}

func (w *captureWire) Open(_ context.Context, dispatch providers.DispatchRequest) (providers.EventStream, error) {
	w.called++
	w.dispatch = dispatch
	return eofStream{}, nil
}

func TestMixedWireNativeHostedSearchCapabilityFollowsResponsesMatrix(t *testing.T) {
	provider := mixedWireProvider{responsesEndpoint: "https://opencode.ai/zen/go/v1/responses"}
	for _, model := range []string{"muse-spark-1.3-contributor", "muse-spark-1.2-contributor", "gpt-5.6-luna", "grok-4.6"} {
		if !provider.SupportsNativeHostedWebSearch(model) {
			t.Fatalf("Responses model %q did not expose native hosted search", model)
		}
	}
	for _, model := range []string{"kimi-k3", "minimax-m3", "unknown-model"} {
		if provider.SupportsNativeHostedWebSearch(model) {
			t.Fatalf("non-Responses model %q exposed native hosted search", model)
		}
	}
}

func TestMixedWireSanitizesMuseOnlyOnResponsesDispatch(t *testing.T) {
	responses := &captureWire{}
	chat := &captureWire{}
	messages := &captureWire{}
	provider := mixedWireProvider{
		chat:              chat,
		responses:         responses,
		messages:          messages,
		responsesEndpoint: "https://opencode.ai/zen/go/v1/responses",
	}
	indexed := true
	baseTool := protocol.HostedWebSearchTool{
		Type:               protocol.HostedWebSearchWebSearch,
		SearchContextSize:  "medium",
		SearchContentTypes: []string{"text", "image"},
		IndexedWebAccess:   &indexed,
	}

	_, err := provider.Open(t.Context(), providers.DispatchRequest{Parsed: protocol.ParsedRequest{
		UpstreamModelID:      "muse-spark-1.3-contributor",
		HostedWebSearchTools: []protocol.HostedWebSearchTool{baseTool},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if responses.called != 1 || chat.called != 0 || messages.called != 0 {
		t.Fatalf("wire calls responses=%d chat=%d messages=%d", responses.called, chat.called, messages.called)
	}
	got := responses.dispatch.Parsed.HostedWebSearchTools[0]
	if got.SearchContentTypes != nil || got.IndexedWebAccess != nil || got.SearchContextSize != "medium" {
		t.Fatalf("Muse request not narrowly sanitized: %#v", got)
	}

	responses.called = 0
	_, err = provider.Open(t.Context(), providers.DispatchRequest{Parsed: protocol.ParsedRequest{
		UpstreamModelID:      "gpt-5.6-luna",
		HostedWebSearchTools: []protocol.HostedWebSearchTool{baseTool},
	}})
	if err != nil {
		t.Fatal(err)
	}
	got = responses.dispatch.Parsed.HostedWebSearchTools[0]
	if got.SearchContentTypes == nil || got.IndexedWebAccess == nil {
		t.Fatalf("unrelated Responses model was sanitized: %#v", got)
	}

	_, err = provider.Open(t.Context(), providers.DispatchRequest{Parsed: protocol.ParsedRequest{
		UpstreamModelID:      "kimi-k3",
		HostedWebSearchTools: []protocol.HostedWebSearchTool{baseTool},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if chat.called != 1 {
		t.Fatalf("chat model did not stay on chat wire: calls=%d", chat.called)
	}
}
