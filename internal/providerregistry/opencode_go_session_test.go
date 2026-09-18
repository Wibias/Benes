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
	"time"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/transport"
)

const openCodeGoSessionHeader = "x-opencode-session"

type openCodeGoSessionCapture struct {
	path          string
	session       string
	authorization string
}

func TestOpenCodeGoSessionAffinityStableAcrossWiresAndSeparatesSiblingLanes(t *testing.T) {
	var captures []openCodeGoSessionCapture
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captures = append(captures, openCodeGoSessionCapture{
			path:          r.URL.Path,
			session:       r.Header.Get(openCodeGoSessionHeader),
			authorization: r.Header.Get("Authorization"),
		})
		writeOpenCodeGoSessionTestSSE(w, r.URL.Path)
	}))
	defer upstream.Close()

	registry := buildOpenCodeGoSessionTestRegistry(t, Spec{
		ID:                "opencode-go",
		Protocol:          ProtocolOpenAIChat,
		Endpoint:          upstream.URL + "/v1/chat/completions",
		APIKey:            "test-key",
		DestinationPolicy: transport.DestinationPolicy{AllowPrivateNetwork: true},
	})

	laneA := providers.NewForwardHeaders(map[string]string{
		"x-codex-parent-thread-id": "raw-parent-id",
		"thread-id":                "raw-child-a",
		"session-id":               "raw-session-a",
	})
	for _, model := range []string{"gpt-5.6-luna", "minimax-m3", "kimi-k3"} {
		openAndDrainOpenCodeGoSessionTest(t, registry["opencode-go"], model, laneA)
	}
	if len(captures) != 3 {
		t.Fatalf("captures=%#v", captures)
	}

	first := captures[0].session
	if first == "" {
		t.Fatal("OpenCode Go session header is empty")
	}
	for i, capture := range captures[:3] {
		if capture.session != first {
			t.Fatalf("captures[%d].session=%q want %q; captures=%#v", i, capture.session, first, captures)
		}
	}
	for _, raw := range []string{"raw-parent-id", "raw-child-a", "raw-session-a"} {
		if strings.Contains(first, raw) {
			t.Fatalf("derived session %q exposes raw source identity %q", first, raw)
		}
	}

	laneB := providers.NewForwardHeaders(map[string]string{
		"x-codex-parent-thread-id": "raw-parent-id",
		"thread-id":                "raw-child-b",
		"session-id":               "raw-session-a",
	})
	openAndDrainOpenCodeGoSessionTest(t, registry["opencode-go"], "kimi-k3", laneB)
	if len(captures) != 4 {
		t.Fatalf("captures=%#v", captures)
	}
	if captures[3].session == "" || captures[3].session == first {
		t.Fatalf("sibling lane session=%q first=%q", captures[3].session, first)
	}
}

func TestOpenCodeGoSessionOverrideWinsAcrossWires(t *testing.T) {
	var captures []openCodeGoSessionCapture
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captures = append(captures, openCodeGoSessionCapture{
			path:    r.URL.Path,
			session: r.Header.Get(openCodeGoSessionHeader),
		})
		writeOpenCodeGoSessionTestSSE(w, r.URL.Path)
	}))
	defer upstream.Close()

	registry := buildOpenCodeGoSessionTestRegistry(t, Spec{
		ID:                      "opencode-go",
		Protocol:                ProtocolOpenAIChat,
		Endpoint:                upstream.URL + "/v1/chat/completions",
		APIKey:                  "test-key",
		OpenCodeSessionOverride: "operator-affinity",
		DestinationPolicy:       transport.DestinationPolicy{AllowPrivateNetwork: true},
	})
	headers := providers.NewForwardHeaders(map[string]string{
		"x-codex-parent-thread-id": "raw-parent",
		"thread-id":                "raw-child",
	})
	for _, model := range []string{"gpt-5.6-luna", "minimax-m3", "kimi-k3"} {
		openAndDrainOpenCodeGoSessionTest(t, registry["opencode-go"], model, headers)
	}
	if len(captures) != 3 {
		t.Fatalf("captures=%#v", captures)
	}
	for i, capture := range captures {
		if capture.session != "operator-affinity" {
			t.Fatalf("captures[%d].session=%q want operator-affinity; captures=%#v", i, capture.session, captures)
		}
	}
}

func TestOpenCodeGoSessionAffinitySurvivesTransientRetryAndKeyRotation(t *testing.T) {
	var captures []openCodeGoSessionCapture
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captures = append(captures, openCodeGoSessionCapture{
			path:          r.URL.Path,
			session:       r.Header.Get(openCodeGoSessionHeader),
			authorization: r.Header.Get("Authorization"),
		})
		switch len(captures) {
		case 1:
			w.WriteHeader(http.StatusServiceUnavailable)
		case 2:
			w.WriteHeader(http.StatusTooManyRequests)
		default:
			writeOpenCodeGoSessionTestSSE(w, r.URL.Path)
		}
	}))
	defer upstream.Close()

	registry := buildOpenCodeGoSessionTestRegistry(t, Spec{
		ID:       "opencode-go",
		Protocol: ProtocolOpenAIChat,
		Endpoint: upstream.URL + "/v1/chat/completions",
		APIKey:   "key-a",
		APIKeyPool: []APIKeySlot{
			{ID: "a", Key: "key-a"},
			{ID: "b", Key: "key-b"},
		},
		DestinationPolicy: transport.DestinationPolicy{AllowPrivateNetwork: true},
		Transient5xx: transport.Transient5xxPolicy{
			Enabled:  true,
			Attempts: 2,
			Sleep:    func(context.Context, time.Duration) error { return nil },
		},
	})

	headers := providers.NewForwardHeaders(map[string]string{
		"x-codex-parent-thread-id": "raw-parent",
		"thread-id":                "raw-child",
	})
	openAndDrainOpenCodeGoSessionTest(t, registry["opencode-go"], "gpt-5.6-luna", headers)

	if len(captures) != 3 {
		t.Fatalf("captures=%#v", captures)
	}
	if captures[0].authorization != "Bearer key-a" || captures[1].authorization != "Bearer key-a" || captures[2].authorization != "Bearer key-b" {
		t.Fatalf("authorization sequence=%q, %q, %q", captures[0].authorization, captures[1].authorization, captures[2].authorization)
	}
	if captures[0].session == "" || captures[1].session != captures[0].session || captures[2].session != captures[0].session {
		t.Fatalf("session sequence=%q, %q, %q", captures[0].session, captures[1].session, captures[2].session)
	}
}

func TestOpenCodeGoSessionAffinityOmittedWithoutStableIdentity(t *testing.T) {
	var got string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get(openCodeGoSessionHeader)
		writeOpenCodeGoSessionTestSSE(w, r.URL.Path)
	}))
	defer upstream.Close()

	registry := buildOpenCodeGoSessionTestRegistry(t, Spec{
		ID:                "opencode-go",
		Protocol:          ProtocolOpenAIChat,
		Endpoint:          upstream.URL + "/v1/chat/completions",
		APIKey:            "test-key",
		DestinationPolicy: transport.DestinationPolicy{AllowPrivateNetwork: true},
	})
	openAndDrainOpenCodeGoSessionTest(t, registry["opencode-go"], "kimi-k3", providers.ForwardHeaders{})
	if got != "" {
		t.Fatalf("session=%q want omitted", got)
	}
}

func TestOpenCodeGoSessionAffinityDoesNotAffectOtherProviders(t *testing.T) {
	var got string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get(openCodeGoSessionHeader)
		writeOpenCodeGoSessionTestSSE(w, r.URL.Path)
	}))
	defer upstream.Close()

	registry, err := Build(context.Background(), []Spec{{
		ID:                "custom",
		Protocol:          ProtocolOpenAIChat,
		Endpoint:          upstream.URL + "/v1/chat/completions",
		APIKey:            "test-key",
		DestinationPolicy: transport.DestinationPolicy{AllowPrivateNetwork: true},
	}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	openAndDrainOpenCodeGoSessionTest(t, registry["custom"], "kimi-k3", providers.NewForwardHeaders(map[string]string{
		"x-codex-parent-thread-id": "raw-parent",
		"thread-id":                "raw-child",
	}))
	if got != "" {
		t.Fatalf("non-OpenCode-Go session=%q want omitted", got)
	}
}

func buildOpenCodeGoSessionTestRegistry(t *testing.T, spec Spec) map[string]providers.Responses {
	t.Helper()
	registry, err := Build(context.Background(), []Spec{spec}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func openAndDrainOpenCodeGoSessionTest(t *testing.T, provider providers.Responses, model string, headers providers.ForwardHeaders) {
	t.Helper()
	stream, err := provider.Open(t.Context(), providers.DispatchRequest{
		Parsed: protocol.ParsedRequest{
			Source:          protocol.RequestSourceChatCompletions,
			UpstreamModelID: model,
			Context: protocol.Context{Messages: []protocol.Message{{
				Role:    protocol.RoleUser,
				Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}},
			}}},
		},
		ForwardHeaders: headers,
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

func writeOpenCodeGoSessionTestSSE(w http.ResponseWriter, path string) {
	w.Header().Set("Content-Type", "text/event-stream")
	switch {
	case strings.HasSuffix(path, "/responses"):
		fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n")
	case strings.HasSuffix(path, "/messages"):
		fmt.Fprint(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"m\",\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n")
		fmt.Fprint(w, "event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n")
		fmt.Fprint(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	default:
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"pong\"},\"finish_reason\":\"stop\"}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}
}
