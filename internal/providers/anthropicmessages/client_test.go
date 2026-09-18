package anthropicmessages

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
)

func TestClientUsesNativeAnthropicHeadersByDefault(t *testing.T) {
	type capture struct {
		header http.Header
		body   map[string]any
	}
	captured := make(chan capture, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		captured <- capture{header: r.Header.Clone(), body: body}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, anthropicTextStream("pong"))
	}))
	defer upstream.Close()

	client, err := New(Config{Endpoint: upstream.URL + "/v1/messages", APIKey: "test-secret", HTTPClient: upstream.Client()})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := client.Open(context.Background(), providers.DispatchRequest{Parsed: minimalRequest("m")})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	event, err := stream.Next()
	if err != nil || event.Type != protocol.EventTextDelta || event.Text != "pong" {
		t.Fatalf("event=%#v err=%v", event, err)
	}

	got := <-captured
	if got.header.Get("x-api-key") != "test-secret" {
		t.Fatalf("x-api-key=%q", got.header.Get("x-api-key"))
	}
	if got.header.Get("Authorization") != "" {
		t.Fatalf("unexpected Authorization=%q", got.header.Get("Authorization"))
	}
	if got.header.Get("anthropic-version") != AnthropicVersion {
		t.Fatalf("anthropic-version=%q", got.header.Get("anthropic-version"))
	}
	if got.header.Get("Accept") != "text/event-stream" || got.header.Get("Content-Type") != "application/json" {
		t.Fatalf("headers=%#v", got.header)
	}
	if got.body["model"] != "m" || got.body["stream"] != true {
		t.Fatalf("body=%#v", got.body)
	}
}

func TestClientBearerTransportNeverEmitsXAPIKey(t *testing.T) {
	captured := make(chan http.Header, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured <- r.Header.Clone()
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, anthropicTextStream("ok"))
	}))
	defer upstream.Close()

	client, err := New(Config{
		Endpoint:        upstream.URL + "/v1/messages",
		APIKey:          "test-secret",
		APIKeyTransport: KeyTransportBearer,
		HTTPClient:      upstream.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := client.Open(t.Context(), providers.DispatchRequest{Parsed: minimalRequest("m")})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if _, err := stream.Next(); err != nil {
		t.Fatal(err)
	}
	header := <-captured
	if header.Get("Authorization") != "Bearer test-secret" || header.Get("x-api-key") != "" {
		t.Fatalf("auth headers=%#v", header)
	}
}

func TestNewHardenedRejectsLoopbackByDefault(t *testing.T) {
	upstream := httptest.NewServer(nil)
	defer upstream.Close()

	_, err := NewHardened(t.Context(), Config{
		Endpoint: upstream.URL + "/v1/messages",
		APIKey:   "test-secret",
	})
	if err == nil {
		t.Fatal("NewHardened accepted a loopback upstream without AllowPrivateNetwork")
	}
}

func TestClientKeepsUpstreamErrorBodyPrivate(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":{"message":"SECRET-UPSTREAM-BODY"}}`)
	}))
	defer upstream.Close()
	client, err := New(Config{Endpoint: upstream.URL + "/v1/messages", APIKey: "test-secret", HTTPClient: upstream.Client()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Open(t.Context(), providers.DispatchRequest{Parsed: minimalRequest("m")})
	if err == nil {
		t.Fatal("expected upstream error")
	}
	if strings.Contains(err.Error(), "SECRET-UPSTREAM-BODY") || strings.Contains(err.Error(), "test-secret") {
		t.Fatalf("private upstream data leaked: %v", err)
	}
	if !strings.Contains(err.Error(), "HTTP 401") {
		t.Fatalf("err=%v", err)
	}
}

func TestClientRejectsNonSSESuccess(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{}`)
	}))
	defer upstream.Close()
	client, err := New(Config{Endpoint: upstream.URL + "/v1/messages", APIKey: "test-secret", HTTPClient: upstream.Client()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Open(t.Context(), providers.DispatchRequest{Parsed: minimalRequest("m")})
	if err == nil || !strings.Contains(err.Error(), "text/event-stream") {
		t.Fatalf("err=%v", err)
	}
}

func TestClientCancellationClosesStream(t *testing.T) {
	started := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		fmt.Fprint(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"m\",\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n")
		flusher.Flush()
		close(started)
		<-r.Context().Done()
	}))
	defer upstream.Close()
	client, err := New(Config{Endpoint: upstream.URL + "/v1/messages", APIKey: "test-secret", HTTPClient: upstream.Client()})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	stream, err := client.Open(ctx, providers.DispatchRequest{Parsed: minimalRequest("m")})
	if err != nil {
		t.Fatal(err)
	}
	<-started
	cancel()
	if err := stream.Close(); err != nil && err != io.ErrClosedPipe {
		t.Fatalf("Close: %v", err)
	}
}

func minimalRequest(model string) protocol.ParsedRequest {
	return protocol.ParsedRequest{
		UpstreamModelID: model,
		Context: protocol.Context{Messages: []protocol.Message{{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "ping"}}}}},
	}
}

func anthropicTextStream(text string) string {
	return "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"m\",\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":" + mustJSON(text) + "}}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
}

func mustJSON(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
