package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providerregistry"
	"github.com/Wibias/Benes/internal/providers"
)

func TestProjectProviderSpecsAllowsOpenCodeGoSessionHeaderOverride(t *testing.T) {
	var gotSession string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSession = r.Header.Get("x-opencode-session")
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"pong\"},\"finish_reason\":\"stop\"}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()

	projection := ProjectProviderSpecs(DiskConfig{Providers: map[string]json.RawMessage{
		"opencode-go": providerJSON(t, fmt.Sprintf(`{
			"adapter":"openai-chat",
			"baseUrl":%q,
			"apiKey":"test-key",
			"allowPrivateNetwork":true,
			"headers":{"X-OpenCode-Session":"operator-affinity"}
		}`, upstream.URL+"/v1")),
	}})
	if len(projection.Skipped) != 0 || len(projection.Specs) != 1 {
		t.Fatalf("projection=%#v", projection)
	}

	registry, err := providerregistry.Build(context.Background(), projection.Specs, providerregistry.Options{})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := registry["opencode-go"].Open(t.Context(), providers.DispatchRequest{
		Parsed: protocol.ParsedRequest{
			Source:          protocol.RequestSourceChatCompletions,
			UpstreamModelID: "kimi-k3",
			Context: protocol.Context{Messages: []protocol.Message{{
				Role:    protocol.RoleUser,
				Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}},
			}}},
		},
		ForwardHeaders: providers.NewForwardHeaders(map[string]string{
			"x-codex-parent-thread-id": "raw-parent",
			"thread-id":                "raw-child",
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	for {
		_, err := stream.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if gotSession != "operator-affinity" {
		t.Fatalf("x-opencode-session=%q want operator-affinity", gotSession)
	}
}

func TestProjectProviderSpecsRejectsAdditionalOpenCodeGoHeaders(t *testing.T) {
	projection := ProjectProviderSpecs(DiskConfig{Providers: map[string]json.RawMessage{
		"opencode-go": providerJSON(t, `{
			"adapter":"openai-chat",
			"baseUrl":"https://opencode.ai/zen/go/v1",
			"apiKey":"test-key",
			"headers":{"x-opencode-session":"operator-affinity","x-test":"blocked"}
		}`),
	}})
	if len(projection.Specs) != 0 || len(projection.Skipped) != 1 {
		t.Fatalf("projection=%#v", projection)
	}
	if projection.Skipped[0] != (ProviderProjectionSkip{ID: "opencode-go", Code: "unsupported_field", Field: "headers"}) {
		t.Fatalf("skip=%#v", projection.Skipped[0])
	}
}
