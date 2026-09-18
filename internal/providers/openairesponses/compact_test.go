package openairesponses

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
	benesreasoning "github.com/Wibias/Benes/internal/responses/reasoning"
)

func TestOfficialOpenAIResponsesEndpointDetectsNativeCompact(t *testing.T) {
	if !officialOpenAIResponsesEndpoint("https://api.openai.com/v1/responses") {
		t.Fatal("official API should support native compact")
	}
	if officialOpenAIResponsesEndpoint("https://example.com/v1/responses") {
		t.Fatal("arbitrary gateway must not use native compact")
	}
	client, err := New(Config{Endpoint: "https://api.openai.com/v1/responses", APIKey: "k", HTTPClient: &http.Client{Timeout: time.Second}})
	if err != nil {
		t.Fatal(err)
	}
	if !client.SupportsNativeCompact() {
		t.Fatal("client")
	}
}

func TestPrepareNativeCompactBodyStripsReasoningAndBenesr1(t *testing.T) {
	body := []byte(`{"model":"gpt-5","reasoning":{"effort":"high"},"input":[{"type":"reasoning","encrypted_content":"` + benesreasoning.Prefix + `abc","content":[{"type":"reasoning_text","text":"raw"}]}]}`)
	got, err := prepareNativeCompactBody(body)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(got, &fields); err != nil {
		t.Fatal(err)
	}
	if _, ok := fields["reasoning"]; ok {
		t.Fatalf("reasoning survived: %s", got)
	}
	input, _ := fields["input"].([]any)
	item, _ := input[0].(map[string]any)
	if _, ok := item["encrypted_content"]; ok {
		t.Fatalf("benesr1 survived: %#v", item)
	}
	content, _ := item["content"].([]any)
	if len(content) != 0 {
		t.Fatalf("content=%#v", content)
	}
}

func TestClientCompactPostsJSONToCompactEndpoint(t *testing.T) {
	var gotPath, gotAuth, gotAccept string
	var gotBody map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotAccept = r.Header.Get("Accept")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode: %v", err)
		}
		w.Header().Set("Retry-After", "5")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()
	client, err := New(Config{Endpoint: upstream.URL + "/v1/responses", APIKey: "upstream-key", HTTPClient: upstream.Client(), MaxStreamBytes: 1 << 20, InactivityTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	status, header, payload, err := client.Compact(context.Background(), providers.DispatchRequest{}, []byte(`{"model":"gpt-5","reasoning":{"effort":"xhigh"},"input":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK {
		t.Fatalf("status=%d", status)
	}
	if !strings.HasSuffix(gotPath, "/responses/compact") {
		t.Fatalf("path=%q", gotPath)
	}
	if gotAuth != "Bearer upstream-key" || gotAccept != "application/json" {
		t.Fatalf("auth=%q accept=%q", gotAuth, gotAccept)
	}
	if _, ok := gotBody["reasoning"]; ok {
		t.Fatalf("body=%#v", gotBody)
	}
	if header.Get("Retry-After") != "5" || string(payload) != `{"ok":true}` {
		t.Fatalf("header=%v payload=%s", header, payload)
	}
}

func TestClientCompactRejectsOversizedResponse(t *testing.T) {
	client, err := New(Config{
		Endpoint: "https://example.invalid/v1/responses",
		APIKey:   "k",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode:    http.StatusOK,
				ContentLength: compactResponseMaxBytes + 1,
				Header:        http.Header{"Content-Type": []string{"application/json"}},
				Body:          io.NopCloser(strings.NewReader("{}")),
				Request:       request,
			}, nil
		})},
		MaxStreamBytes:    1 << 20,
		InactivityTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, err = client.Compact(context.Background(), providers.DispatchRequest{}, []byte(`{"model":"gpt-5"}`))
	if err != ErrCompactResponseTooLarge {
		t.Fatalf("err=%v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func TestStreamMapsCompactionOutputItem(t *testing.T) {
	upstream := sseServer(t, []string{
		`{"type":"response.output_item.added","item":{"type":"compaction","id":"cmp_1","encrypted_content":"native-blob"}}`,
		`{"type":"response.output_item.done","item":{"type":"compaction","id":"cmp_1","encrypted_content":"native-blob"}}`,
		`{"type":"response.completed","response":{}}`,
	})
	defer upstream.Close()
	client, _ := New(Config{Endpoint: upstream.URL, APIKey: "k", HTTPClient: upstream.Client(), MaxStreamBytes: 1 << 20, InactivityTimeout: time.Second})
	stream, err := client.Open(context.Background(), canonicalRequest(t, `{"model":"openai-apikey/gpt-5.6","store":false}`, "gpt-5.6"))
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	event, err := stream.Next()
	if err != nil {
		t.Fatal(err)
	}
	if event.Type != protocol.EventCompaction || event.ID != "cmp_1" || event.Data != "native-blob" {
		t.Fatalf("event=%#v", event)
	}
	event, err = stream.Next()
	if err != nil {
		t.Fatal(err)
	}
	if event.Type != protocol.EventDone {
		t.Fatalf("event=%#v", event)
	}
}

func TestForwardCompactUsesCanonicalCompactURL(t *testing.T) {
	var gotURL string
	client, err := NewForward(ForwardConfig{
		Endpoint: testCanonicalForwardResponsesEndpoint,
		HTTPClient: &http.Client{Transport: forwardRoundTripFunc(func(request *http.Request) (*http.Response, error) {
			gotURL = request.URL.String()
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			}, nil
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !client.SupportsNativeCompact() || !client.NativeCodexForward() {
		t.Fatal("forward client should be native compact")
	}
	status, _, payload, err := client.Compact(context.Background(), providers.DispatchRequest{
		ForwardHeaders: providers.NewForwardHeaders(map[string]string{"Authorization": "Bearer caller-oauth"}),
		Parsed:         protocol.ParsedRequest{UpstreamModelID: "gpt-5"},
	}, []byte(`{"model":"gpt-5","input":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || string(payload) != `{"ok":true}` {
		t.Fatalf("status=%d payload=%s", status, payload)
	}
	if gotURL != testCanonicalForwardResponsesEndpoint+"/compact" {
		t.Fatalf("url=%q", gotURL)
	}
}

func TestForwardSearchUsesCanonicalAlphaSearchURL(t *testing.T) {
	var gotURL string
	client, err := NewForward(ForwardConfig{
		Endpoint: testCanonicalForwardResponsesEndpoint,
		HTTPClient: &http.Client{Transport: forwardRoundTripFunc(func(request *http.Request) (*http.Response, error) {
			gotURL = request.URL.String()
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"encrypted_output":null,"output":"ok","results":[]}`)),
			}, nil
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	status, _, payload, err := client.Search(context.Background(), providers.DispatchRequest{
		ForwardHeaders: providers.NewForwardHeaders(map[string]string{"Authorization": "Bearer caller-oauth"}),
		Parsed:         protocol.ParsedRequest{UpstreamModelID: "gpt-5"},
	}, []byte(`{"id":"search-session","model":"gpt-5","commands":{"search_query":[{"q":"benes"}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || !strings.Contains(string(payload), `"output":"ok"`) {
		t.Fatalf("status=%d payload=%s", status, payload)
	}
	if gotURL != testCanonicalForwardResponsesEndpoint+"/alpha/search" {
		t.Fatalf("url=%q", gotURL)
	}
}

func TestAppendCompactionTriggerKeepsCompiledInput(t *testing.T) {
	body, err := appendCompactionTrigger([]byte(`{"model":"gpt-5","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(body, &fields); err != nil {
		t.Fatal(err)
	}
	input, _ := fields["input"].([]any)
	if len(input) != 2 {
		t.Fatalf("input=%#v", input)
	}
	item, _ := input[1].(map[string]any)
	if item["type"] != "compaction_trigger" {
		t.Fatalf("item=%#v", item)
	}
}
