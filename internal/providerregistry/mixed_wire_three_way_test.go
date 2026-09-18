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

func TestMixedWireSelectsExactlyOneOfThreeWires(t *testing.T) {
	boom := errors.New("selected wire failed")
	tests := []struct {
		model string
		want  string
	}{
		{model: "gpt-5.6-luna", want: "responses"},
		{model: "minimax-m3", want: "messages"},
		{model: "kimi-k3", want: "chat"},
		{model: "grok-4.5", want: "chat"},
		{model: "future-model", want: "chat"},
	}
	for _, test := range tests {
		t.Run(test.model, func(t *testing.T) {
			var calls []string
			provider := mixedWireProvider{
				chat:      recordingWire{name: "chat", calls: &calls, err: boom},
				responses: recordingWire{name: "responses", calls: &calls, err: boom},
				messages:  recordingWire{name: "messages", calls: &calls, err: boom},
			}
			_, err := provider.Open(t.Context(), providers.DispatchRequest{
				Parsed: protocol.ParsedRequest{UpstreamModelID: test.model},
			})
			if !errors.Is(err, boom) {
				t.Fatalf("Open(%s) err=%v", test.model, err)
			}
			if len(calls) != 1 || calls[0] != test.want {
				t.Fatalf("Open(%s) calls=%v want exactly [%s]", test.model, calls, test.want)
			}
		})
	}
}

func TestBuildRoutesOpenCodeGoAcrossCurrentThreeEndpoints(t *testing.T) {
	var paths []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "text/event-stream")
		switch {
		case strings.HasSuffix(r.URL.Path, "/responses"):
			fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n")
		case strings.HasSuffix(r.URL.Path, "/messages"):
			if got := r.Header.Get("anthropic-version"); got == "" {
				t.Errorf("messages request missing anthropic-version")
			}
			fmt.Fprint(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"m\",\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n")
			fmt.Fprint(w, "event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n")
			fmt.Fprint(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
		default:
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"pong\"},\"finish_reason\":\"stop\"}]}\n\n")
			fmt.Fprint(w, "data: [DONE]\n\n")
		}
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
		for {
			_, err := stream.Next()
			if errors.Is(err, io.EOF) {
				return
			}
			if err != nil {
				t.Fatalf("Next(%s): %v", model, err)
			}
		}
	}

	open("gpt-5.6-luna")
	open("minimax-m3")
	open("kimi-k3")
	open("grok-4.5")
	if len(paths) != 4 {
		t.Fatalf("paths=%v", paths)
	}
	wantSuffixes := []string{"/v1/responses", "/v1/messages", "/v1/chat/completions", "/v1/chat/completions"}
	for i, suffix := range wantSuffixes {
		if !strings.HasSuffix(paths[i], suffix) {
			t.Fatalf("paths[%d]=%q want suffix %q (all=%v)", i, paths[i], suffix, paths)
		}
	}
}
