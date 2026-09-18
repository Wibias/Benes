package openairesponses

import (
	"context"
	"encoding/json"
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
	"github.com/Wibias/Benes/internal/resourcebudget"
	"github.com/Wibias/Benes/internal/responses/parsed"
	requestwire "github.com/Wibias/Benes/internal/responses/request"
	"github.com/Wibias/Benes/internal/transport"
)

func canonicalRequest(t *testing.T, body, upstreamModel string) providers.DispatchRequest {
	t.Helper()
	wire, err := requestwire.Decode(strings.NewReader(body), 1<<20)
	if err != nil {
		t.Fatalf("Decode(%s): %v", body, err)
	}
	request, err := parsed.Build(wire, 1)
	if err != nil {
		t.Fatalf("Build(%s): %v", body, err)
	}
	request.UpstreamModelID = upstreamModel
	return providers.DispatchRequest{Parsed: request}
}

func TestValidateMigratedRequestRejectsStatefulAndUnsupportedShapes(t *testing.T) {
	for _, body := range []string{
		`{"model":"openai-apikey/gpt-5.6","background":true}`,
		`{"model":"openai-apikey/gpt-5.6","tools":[{"type":"web_search"}]}`,
		`{"model":"openai-apikey/gpt-5.6","input":[{"type":"reasoning","summary":[]}]}`,
		`{"model":"openai-apikey/gpt-5.6","include":["reasoning.encrypted_content"]}`,
	} {
		request := canonicalRequest(t, body, "gpt-5.6")
		if err := ValidateMigratedRequest(request.Parsed); err == nil {
			t.Fatalf("ValidateMigratedRequest accepted %s", body)
		}
	}
}

func TestValidateMigratedRequestAllowsPreviousResponseID(t *testing.T) {
	request := canonicalRequest(t, `{"model":"openai-apikey/gpt-5.6","previous_response_id":"resp_1","input":"hi"}`, "gpt-5.6")
	if err := ValidateMigratedRequest(request.Parsed); err != nil {
		t.Fatalf("ValidateMigratedRequest(): %v", err)
	}
}

func TestValidateMigratedRequestAllowsMessagesAndFunctionContinuation(t *testing.T) {
	body := `{"model":"openai-apikey/gpt-5.6","store":false,"input":[{"role":"user","content":"hi"},{"type":"function_call","id":"fc_old","call_id":"c1","name":"lookup","arguments":"{}"},{"type":"function_call_output","id":"out_old","call_id":"c1","output":"ok"}],"tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}]}`
	request := canonicalRequest(t, body, "gpt-5.6")
	if err := ValidateMigratedRequest(request.Parsed); err != nil {
		t.Fatalf("ValidateMigratedRequest(): %v", err)
	}
}

func TestOpenForcesStreamingStatelessBodyAndOwnAuthorization(t *testing.T) {
	var gotAuth, gotCaller, gotWindow string
	var gotBody map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotCaller = r.Header.Get("X-Caller-Secret")
		gotWindow = r.Header.Get("X-Codex-Window-ID")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":1,\"output_tokens\":2,\"total_tokens\":3}}}\n\n")
	}))
	defer upstream.Close()

	client, err := New(Config{Endpoint: upstream.URL, APIKey: "upstream-key", HTTPClient: upstream.Client(), MaxStreamBytes: 1 << 20, InactivityTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	request := canonicalRequest(t, `{"model":"openai-apikey/gpt-5.6","stream":false,"store":false,"input":[{"role":"user","content":"hi"}],"future_field":{"x":1}}`, "gpt-5.6")
	request.ForwardHeaders = providers.NewForwardHeaders(map[string]string{
		"authorization":     "caller-auth-marker",
		"x-codex-window-id": "window-1",
		"x-caller-secret":   "unknown-marker",
	})
	stream, err := client.Open(context.Background(), request)
	if err != nil {
		t.Fatalf("Open(): %v", err)
	}
	defer stream.Close()
	event, err := stream.Next()
	if err != nil {
		t.Fatalf("Next(): %v", err)
	}
	if event.Type != protocol.EventDone || event.Usage == nil || event.Usage.TotalTokens != 3 {
		t.Fatalf("event=%#v", event)
	}
	if gotAuth != "Bearer upstream-key" {
		t.Fatalf("Authorization=%q", gotAuth)
	}
	if gotCaller != "" {
		t.Fatalf("caller secret forwarded: %q", gotCaller)
	}
	if gotWindow != "" {
		t.Fatalf("forward metadata leaked upstream: %q", gotWindow)
	}
	if gotBody["model"] != "gpt-5.6" || gotBody["stream"] != true || gotBody["store"] != false {
		t.Fatalf("body=%#v", gotBody)
	}
	if _, ok := gotBody["future_field"]; ok {
		t.Fatalf("unknown Raw field leaked upstream: %#v", gotBody)
	}
}

func TestStreamMapsTextAndFunctionCallLifecycle(t *testing.T) {
	upstream := sseServer(t, []string{
		`{"type":"response.output_text.delta","delta":"hello"}`,
		`{"type":"response.output_item.added","item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"lookup","arguments":"","status":"in_progress"}}`,
		`{"type":"response.function_call_arguments.delta","item_id":"fc_1","delta":"{\"q\":\"x\"}"}`,
		`{"type":"response.function_call_arguments.done","item_id":"fc_1","arguments":"{\"q\":\"x\"}"}`,
		`{"type":"response.completed","response":{"usage":{"input_tokens":5,"input_tokens_details":{"cached_tokens":2},"output_tokens":3,"output_tokens_details":{"reasoning_tokens":1},"total_tokens":8}}}`,
	})
	defer upstream.Close()
	client, _ := New(Config{Endpoint: upstream.URL, APIKey: "k", HTTPClient: upstream.Client(), MaxStreamBytes: 1 << 20, InactivityTimeout: time.Second})
	stream, err := client.Open(context.Background(), canonicalRequest(t, `{"model":"openai-apikey/gpt-5.6","store":false}`, "gpt-5.6"))
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()

	var got []protocol.Event
	for {
		event, err := stream.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Next(): %v", err)
		}
		got = append(got, event)
	}
	want := []protocol.EventType{protocol.EventTextDelta, protocol.EventToolCallStart, protocol.EventToolCallDelta, protocol.EventToolCallEnd, protocol.EventDone}
	if len(got) != len(want) {
		t.Fatalf("events=%#v", got)
	}
	for i := range want {
		if got[i].Type != want[i] {
			t.Fatalf("event[%d]=%s want=%s", i, got[i].Type, want[i])
		}
	}
	if got[2].Arguments != `{"q":"x"}` {
		t.Fatalf("arguments=%q", got[2].Arguments)
	}
	if got[4].Usage == nil || got[4].Usage.CachedInputTokens != 2 || got[4].Usage.ReasoningOutputTokens != 1 {
		t.Fatalf("usage=%#v", got[4].Usage)
	}
}

func TestStreamMapsSparseLifecycleFixtureToTextAndDone(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: response.created\n")
		fmt.Fprint(w, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_example\",\"object\":\"response\"}}\n\n")
		fmt.Fprint(w, "event: response.output_item.added\n")
		fmt.Fprint(w, "data: {\"type\":\"response.output_item.added\",\"item\":{\"id\":\"msg_example\",\"type\":\"message\"}}\n\n")
		fmt.Fprint(w, "event: response.output_text.delta\n")
		fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\n")
		fmt.Fprint(w, "event: response.completed\n")
		fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_example\",\"object\":\"response\"}}\n\n")
	}))
	defer upstream.Close()
	client, _ := New(Config{Endpoint: upstream.URL, APIKey: "k", HTTPClient: upstream.Client(), MaxStreamBytes: 1 << 20, InactivityTimeout: time.Second})
	stream, err := client.Open(context.Background(), canonicalRequest(t, `{"model":"openai-apikey/gpt-5.6","store":false}`, "gpt-5.6"))
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()

	var got []protocol.Event
	for {
		event, err := stream.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Next(): %v", err)
		}
		got = append(got, event)
	}
	if len(got) != 2 || got[0].Type != protocol.EventTextDelta || got[0].Text != "hello" || got[1].Type != protocol.EventDone {
		t.Fatalf("events=%#v", got)
	}
}

func TestStreamFailsClosedOnUnsupportedWebSearchEvent(t *testing.T) {
	upstream := sseServer(t, []string{`{"type":"response.output_item.added","item":{"type":"web_search_call","id":"ws_1","status":"in_progress"}}`})
	defer upstream.Close()
	client, _ := New(Config{Endpoint: upstream.URL, APIKey: "k", HTTPClient: upstream.Client(), MaxStreamBytes: 1 << 20, InactivityTimeout: time.Second})
	stream, err := client.Open(context.Background(), canonicalRequest(t, `{"model":"openai-apikey/gpt-5.6","store":false}`, "gpt-5.6"))
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if _, err := stream.Next(); !errors.Is(err, ErrUnsupportedUpstreamEvent) {
		t.Fatalf("err=%v", err)
	}
}

func TestOpenDoesNotExposeUpstreamErrorBody(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":{"message":"SECRET-UPSTREAM-BODY"}}`)
	}))
	defer upstream.Close()
	client, _ := New(Config{Endpoint: upstream.URL, APIKey: "k", HTTPClient: upstream.Client()})
	_, err := client.Open(context.Background(), canonicalRequest(t, `{"model":"openai-apikey/gpt-5.6","store":false}`, "gpt-5.6"))
	if err == nil || strings.Contains(err.Error(), "SECRET-UPSTREAM-BODY") {
		t.Fatalf("err=%v", err)
	}
}

func TestOpenExposesSanitizedHTTP400Detail(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":{"message":"Unknown parameter: 'max_output_tokens'."}}`)
	}))
	defer upstream.Close()
	client, _ := New(Config{Endpoint: upstream.URL, APIKey: "k", HTTPClient: upstream.Client()})
	_, err := client.Open(context.Background(), canonicalRequest(t, `{"model":"openai-apikey/gpt-5.6","store":false}`, "gpt-5.6"))
	if err == nil || !strings.Contains(err.Error(), "Unknown parameter: 'max_output_tokens'.") {
		t.Fatalf("err=%v", err)
	}
	if strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("err=%v", err)
	}
}

func sseServer(t *testing.T, payloads []string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, payload := range payloads {
			fmt.Fprintf(w, "data: %s\n\n", payload)
		}
	}))
}

func TestOpenRejectsUnsupportedShapeBeforeNetwork(t *testing.T) {
	var reached bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer upstream.Close()
	client, err := New(Config{Endpoint: upstream.URL, APIKey: "k", HTTPClient: upstream.Client()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Open(context.Background(), canonicalRequest(t, `{"model":"openai-apikey/gpt-5.6","store":false,"tools":[{"type":"web_search"}]}`, "gpt-5.6"))
	if !errors.Is(err, ErrUnsupportedRequestShape) {
		t.Fatalf("err=%v, want ErrUnsupportedRequestShape", err)
	}
	if reached {
		t.Fatal("unsupported request reached upstream")
	}
}

func TestValidateMigratedRequestRejectsParallelToolCalls(t *testing.T) {
	request := canonicalRequest(t, `{"model":"openai-apikey/gpt-4o","store":false,"parallel_tool_calls":true}`, "gpt-4o")
	if err := ValidateMigratedRequest(request.Parsed); !errors.Is(err, ErrUnsupportedRequestShape) {
		t.Fatalf("err=%v", err)
	}
}

func TestOpenRejectsNonSSESuccessResponse(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"completed"}`)
	}))
	defer upstream.Close()
	client, _ := New(Config{Endpoint: upstream.URL, APIKey: "k", HTTPClient: upstream.Client()})
	_, err := client.Open(context.Background(), canonicalRequest(t, `{"model":"openai-apikey/gpt-4o","store":false}`, "gpt-4o"))
	if err == nil || !strings.Contains(err.Error(), "text/event-stream") {
		t.Fatalf("err=%v", err)
	}
}

func TestNewHardenedUsesValidatedPinnedTransport(t *testing.T) {
	upstream := sseServer(t, []string{`{"type":"response.completed","response":{}}`})
	defer upstream.Close()
	client, err := NewHardened(context.Background(), Config{
		Endpoint:          upstream.URL,
		APIKey:            "k",
		DestinationPolicy: transport.DestinationPolicy{AllowPrivateNetwork: true},
	})
	if err != nil {
		t.Fatalf("NewHardened(): %v", err)
	}
	stream, err := client.Open(context.Background(), canonicalRequest(t, `{"model":"openai-apikey/gpt-4o","store":false}`, "gpt-4o"))
	if err != nil {
		t.Fatalf("Open(): %v", err)
	}
	defer stream.Close()
	event, err := stream.Next()
	if err != nil {
		t.Fatalf("Next(): %v", err)
	}
	if event.Type != protocol.EventDone {
		t.Fatalf("event=%#v", event)
	}
}

func TestOpenRetriesTransient503WhenPolicyEnabled(t *testing.T) {
	hits := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":1,\"output_tokens\":2,\"total_tokens\":3}}}\n\n")
	}))
	defer upstream.Close()
	client, err := New(Config{
		Endpoint: upstream.URL, APIKey: "upstream-key", HTTPClient: upstream.Client(),
		MaxStreamBytes: 1 << 20, InactivityTimeout: time.Second,
		Transient5xx: transport.Transient5xxPolicy{Enabled: true, Attempts: 2, Sleep: func(context.Context, time.Duration) error {
			return nil
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := client.Open(context.Background(), canonicalRequest(t, `{"model":"openai-apikey/gpt-4o","store":false,"input":"hi"}`, "gpt-4o"))
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if hits != 2 {
		t.Fatalf("hits=%d", hits)
	}
}

func TestOpenDoesNotRetry503WhenPolicyDisabled(t *testing.T) {
	hits := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer upstream.Close()
	client, err := New(Config{Endpoint: upstream.URL, APIKey: "k", HTTPClient: upstream.Client(), MaxStreamBytes: 1 << 20, InactivityTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Open(context.Background(), canonicalRequest(t, `{"model":"openai-apikey/gpt-4o","store":false,"input":"hi"}`, "gpt-4o"))
	if err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("err=%v", err)
	}
	if hits != 1 {
		t.Fatalf("hits=%d", hits)
	}
}

func TestOpenStopsTransientRetriesAtTurnPhysicalSendBudget(t *testing.T) {
	hits := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, `{"error":"temporary"}`)
	}))
	t.Cleanup(upstream.Close)

	client, err := New(Config{
		Endpoint:   upstream.URL,
		APIKey:     "key",
		HTTPClient: upstream.Client(),
		Transient5xx: transport.Transient5xxPolicy{
			Enabled:  true,
			Attempts: 4,
			Sleep:    func(context.Context, time.Duration) error { return nil },
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxPhysicalSends: 2})
	turn, err := budget.AcquireTurn(context.Background(), "responses-budget")
	if err != nil {
		t.Fatal(err)
	}
	defer turn.Close()

	dispatch := canonicalRequest(t, `{"model":"openai-apikey/gpt-5.6","input":"hello"}`, "gpt-5.6")
	dispatch.Turn = turn
	_, err = client.Open(context.Background(), dispatch)
	if !errors.Is(err, resourcebudget.ErrPhysicalSendBudgetExceeded) {
		t.Fatalf("err=%v", err)
	}
	if hits != 2 {
		t.Fatalf("physical hits=%d want=2", hits)
	}
	records := turn.PhysicalSends()
	if len(records) != 2 || records[0].Reason != "openai-responses" || records[1].Reason != "openai-responses" {
		t.Fatalf("records=%#v", records)
	}
}

