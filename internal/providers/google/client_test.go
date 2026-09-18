package google

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
)

func TestOpenPostsWireModelAndReadsSSEText(t *testing.T) {
	var gotPath, gotKey string
	var gotBody map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path + "?" + r.URL.RawQuery
		gotKey = r.Header.Get("x-goog-api-key")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hello\"}]}}]}\n\n")
	}))
	t.Cleanup(upstream.Close)
	client, err := New(context.Background(), Config{APIKey: "gk-test", HTTPClient: upstream.Client(), Endpoint: "https://generativelanguage.googleapis.com"})
	if err != nil {
		t.Fatal(err)
	}
	// Point the request at httptest while still validating identity/URL construction.
	id, _ := ResolveIdentity(KindAIStudio, "gemini-3.7-flash", WireRenameDefault)
	url, _ := GenerateURL(Endpoint{Kind: KindAIStudio, BaseURL: "https://generativelanguage.googleapis.com"}, id, true)
	if !strings.Contains(url, "gemini-3.7-flash-tiered") {
		t.Fatalf("wire url=%s", url)
	}
	client.testOrigin = upstream.URL
	stream, err := client.Open(context.Background(), providers.DispatchRequest{Parsed: protocol.ParsedRequest{
		ModelID: "gemini-3.7-flash",
		Context: protocol.Context{Messages: []protocol.Message{{
			Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}},
		}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	ev, err := stream.Next()
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}
	if ev.Text != "hello" || ev.Type != protocol.EventTextDelta {
		t.Fatalf("event=%#v", ev)
	}
	if gotKey != "gk-test" || !strings.Contains(gotPath, "gemini-3.7-flash-tiered") {
		t.Fatalf("path=%s key=%s", gotPath, gotKey)
	}
}

func TestVertexOpenKeepsRequestedModelAndUsesAPIKey(t *testing.T) {
	var gotPath, gotKey, gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.Header.Get("x-goog-api-key")
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"ok\"}]}}]}\n\n")
	}))
	t.Cleanup(upstream.Close)
	client, err := New(context.Background(), Config{Kind: KindVertex, APIKey: "vk-test", HTTPClient: upstream.Client()})
	if err != nil {
		t.Fatal(err)
	}
	client.testOrigin = upstream.URL
	stream, err := client.Open(context.Background(), providers.DispatchRequest{Parsed: protocol.ParsedRequest{
		ModelID: "gemini-3.7-flash",
		Context: protocol.Context{Messages: []protocol.Message{{
			Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}},
		}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if _, err := stream.Next(); err != nil && err != io.EOF {
		t.Fatal(err)
	}
	if gotKey != "vk-test" || gotAuth != "" || strings.Contains(gotPath, "tiered") || !strings.Contains(gotPath, "/publishers/google/models/gemini-3.7-flash:") {
		t.Fatalf("path=%s key=%s auth=%s", gotPath, gotKey, gotAuth)
	}
}

func TestOpenRotatesAPIKeyOn429BeforeSameKeyRetry(t *testing.T) {
	var keys []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		keys = append(keys, r.Header.Get("x-goog-api-key"))
		if len(keys) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"error":{"status":"RESOURCE_EXHAUSTED","message":"rate limit"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"ok\"}]}}]}\n\n")
	}))
	t.Cleanup(upstream.Close)
	client, err := New(context.Background(), Config{
		Keys:       []KeySlot{{ID: "a", Key: "key-a"}, {ID: "b", Key: "key-b"}},
		HTTPClient: upstream.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	client.testOrigin = upstream.URL
	stream, err := client.Open(context.Background(), providers.DispatchRequest{Parsed: protocol.ParsedRequest{
		ModelID: "gemini-3.5-flash",
		Context: protocol.Context{Messages: []protocol.Message{{
			Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}},
		}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if len(keys) != 2 || keys[0] == keys[1] {
		t.Fatalf("keys=%v", keys)
	}
}

func TestStreamTruncatedToolTurnFailsClosed(t *testing.T) {
	s := &stream{buf: "data: {\"candidates\":[{\"finishReason\":\"MAX_TOKENS\",\"content\":{\"parts\":[{\"functionCall\":{\"name\":\"lookup\",\"id\":\"c1\"}}]}}]}\n"}
	ev, err := s.Next()
	if err != nil || ev.Type != protocol.EventToolCallEnd {
		t.Fatalf("call=%#v err=%v", ev, err)
	}
	ev, err = s.Next()
	if err != nil || ev.Type != protocol.EventError || !strings.Contains(ev.Message, "truncated") {
		t.Fatalf("term=%#v err=%v", ev, err)
	}
}

func TestStreamFailsClosedOnMalformedClaimedModelFrameAfterValidText(t *testing.T) {
	s := &stream{buf: "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hello\"}]}}]}\ndata: {\"candidates\":\"broken\"}\n"}
	ev, err := s.Next()
	if err != nil || ev.Type != protocol.EventTextDelta || ev.Text != "hello" {
		t.Fatalf("first=%#v err=%v", ev, err)
	}
	ev, err = s.Next()
	if err != nil || ev.Type != protocol.EventError || !strings.Contains(ev.Message, "malformed") {
		t.Fatalf("malformed claimed-model frame was skipped: %#v err=%v", ev, err)
	}
}

func TestStreamFailsClosedOnMalformedCandidatePartsShape(t *testing.T) {
	s := &stream{buf: "data: {\"candidates\":[{\"content\":{\"parts\":\"not-an-array\"}}]}\n"}
	ev, err := s.Next()
	if err != nil || ev.Type != protocol.EventError || !strings.Contains(ev.Message, "malformed") {
		t.Fatalf("malformed parts shape was skipped: %#v err=%v", ev, err)
	}
}

func TestStreamStillCompletesOnUsageOnlyFrames(t *testing.T) {
	s := &stream{buf: "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"ok\"}]}}]}\ndata: {\"usageMetadata\":{\"promptTokenCount\":3,\"candidatesTokenCount\":1}}\n"}
	ev, err := s.Next()
	if err != nil || ev.Type != protocol.EventTextDelta || ev.Text != "ok" {
		t.Fatalf("text=%#v err=%v", ev, err)
	}
	ev, err = s.Next()
	if err != nil || ev.Type != protocol.EventDone {
		t.Fatalf("usage-only frame should not fail the stream: %#v err=%v", ev, err)
	}
}

func TestVertexRequiresProjectAndLocationWithoutAPIKey(t *testing.T) {
	if _, err := New(context.Background(), Config{Kind: KindVertex}); err == nil {
		t.Fatal("expected missing vertex auth")
	}
	if _, err := New(context.Background(), Config{Kind: KindVertex, Project: "p", Location: "US-CENTRAL1"}); err == nil {
		t.Fatal("uppercase location")
	}
	if _, err := New(context.Background(), Config{Kind: KindVertex, Project: "p", Location: "us-central1"}); err != nil {
		t.Fatal(err)
	}
}

func TestVertexOpenUsesADCBearerWhenNoAPIKey(t *testing.T) {
	globalADC = adcCache{}
	path := writeADCFile(t, `{"type":"authorized_user","client_id":"cid","client_secret":"csec","refresh_token":"rt-secret"}`)
	var gotAuth, gotKey string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotKey = r.Header.Get("x-goog-api-key")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"ok\"}]}}]}\n\n")
	}))
	t.Cleanup(upstream.Close)
	client, err := New(context.Background(), Config{
		Kind:       KindVertex,
		Project:    "proj",
		Location:   "us-central1",
		HTTPClient: upstream.Client(),
		ADC: ADCEnv{
			Environ: map[string]string{"GOOGLE_APPLICATION_CREDENTIALS": path},
			HTTP: &http.Client{Transport: adcRoundTrip(func(req *http.Request) (*http.Response, error) {
				return adcJSONResponse(200, map[string]any{"access_token": "ya29.live", "expires_in": 3600}), nil
			})},
			Now:   func() time.Time { return time.Unix(1_700_000_000, 0) },
			Sleep: func(time.Duration) {},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	client.testOrigin = upstream.URL
	stream, err := client.Open(context.Background(), providers.DispatchRequest{Parsed: protocol.ParsedRequest{
		ModelID: "gemini-3.7-flash",
		Context: protocol.Context{Messages: []protocol.Message{{
			Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}},
		}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if _, err := stream.Next(); err != nil && err != io.EOF {
		t.Fatal(err)
	}
	if gotAuth != "Bearer ya29.live" || gotKey != "" {
		t.Fatalf("auth=%q key=%q", gotAuth, gotKey)
	}
}
