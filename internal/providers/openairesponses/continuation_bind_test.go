package openairesponses

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/resourcebudget"
	"github.com/Wibias/Benes/internal/responses/continuation"
)

func TestOpenBindsPreviousResponseAfterCredentialAndDoesNotCompoundCarriedHistory(t *testing.T) {
	var bodies []map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "text/event-stream")
		id := fmt.Sprintf("resp_%d", len(bodies))
		fmt.Fprintf(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":%q,\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"id\":\"msg_%d\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"answer\"}]}],\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n", id, len(bodies))
	}))
	defer upstream.Close()

	store := continuation.NewStore(continuation.StoreLimits{TTL: time.Hour}, time.Now)
	authority, err := continuation.NewAuthority(store, nil, bytes32('c'))
	if err != nil {
		t.Fatal(err)
	}
	client, err := New(Config{
		Endpoint: upstream.URL, APIKey: "sk-test", HTTPClient: upstream.Client(),
		MaxStreamBytes: 1 << 20, InactivityTimeout: time.Second, Continuation: authority,
	})
	if err != nil {
		t.Fatal(err)
	}

	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 2, MaxTurnBytes: 1 << 20, MaxProcessBytes: 1 << 20, ClassBytes: map[resourcebudget.Class]int64{resourcebudget.ClassContinuation: 1 << 20}})
	firstTurn, err := budget.AcquireTurn(context.Background(), "t1")
	if err != nil {
		t.Fatal(err)
	}
	first := canonicalRequest(t, `{"model":"openai-apikey/gpt-5.6","input":"ask"}`, "gpt-5.6")
	first.Turn = firstTurn
	stream, err := client.Open(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	drainStream(t, stream)
	firstTurn.Close()

	secondTurn, err := budget.AcquireTurn(context.Background(), "t2")
	if err != nil {
		t.Fatal(err)
	}
	second := canonicalRequest(t, `{"model":"openai-apikey/gpt-5.6","previous_response_id":"resp_1","input":[{"role":"user","content":"ask"},{"type":"message","id":"msg_1","role":"assistant","content":[{"type":"output_text","text":"answer"}]},{"role":"user","content":"next"}]}`, "gpt-5.6")
	second.Turn = secondTurn
	stream, err = client.Open(context.Background(), second)
	if err != nil {
		t.Fatal(err)
	}
	drainStream(t, stream)
	secondTurn.Close()

	if len(bodies) != 2 {
		t.Fatalf("requests=%d", len(bodies))
	}
	if bodies[1]["previous_response_id"] != "resp_1" {
		t.Fatalf("second body=%#v", bodies[1])
	}
	input, _ := bodies[1]["input"].([]any)
	if len(input) != 3 {
		t.Fatalf("carried prefix was compounded: %#v", input)
	}
}

func TestOpenDoesNotRestoreForeignCredentialContinuation(t *testing.T) {
	var secondInput []any
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if calls == 2 {
			secondInput, _ = body["input"].([]any)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"id\":\"msg_1\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"answer\"}]}],\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n")
	}))
	defer upstream.Close()

	store := continuation.NewStore(continuation.StoreLimits{TTL: time.Hour}, time.Now)
	authority, err := continuation.NewAuthority(store, nil, bytes32('c'))
	if err != nil {
		t.Fatal(err)
	}
	firstClient, err := New(Config{Endpoint: upstream.URL, APIKey: "sk-one", HTTPClient: upstream.Client(), MaxStreamBytes: 1 << 20, InactivityTimeout: time.Second, Continuation: authority})
	if err != nil {
		t.Fatal(err)
	}
	secondClient, err := New(Config{Endpoint: upstream.URL, APIKey: "sk-two", HTTPClient: upstream.Client(), MaxStreamBytes: 1 << 20, InactivityTimeout: time.Second, Continuation: authority})
	if err != nil {
		t.Fatal(err)
	}
	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 2, MaxTurnBytes: 1 << 20, MaxProcessBytes: 1 << 20, ClassBytes: map[resourcebudget.Class]int64{resourcebudget.ClassContinuation: 1 << 20}})
	turn, _ := budget.AcquireTurn(context.Background(), "a")
	stream, err := firstClient.Open(context.Background(), withTurn(canonicalRequest(t, `{"model":"openai-apikey/gpt-5.6","input":"ask"}`, "gpt-5.6"), turn))
	if err != nil {
		t.Fatal(err)
	}
	drainStream(t, stream)
	turn.Close()

	turn, _ = budget.AcquireTurn(context.Background(), "b")
	stream, err = secondClient.Open(context.Background(), withTurn(canonicalRequest(t, `{"model":"openai-apikey/gpt-5.6","previous_response_id":"resp_1","input":"next"}`, "gpt-5.6"), turn))
	if err != nil {
		t.Fatal(err)
	}
	drainStream(t, stream)
	turn.Close()
	if len(secondInput) != 1 {
		t.Fatalf("foreign owner expanded prefix: %#v", secondInput)
	}
}

func withTurn(dispatch providers.DispatchRequest, turn *resourcebudget.Turn) providers.DispatchRequest {
	dispatch.Turn = turn
	return dispatch
}

func drainStream(t *testing.T, stream providers.EventStream) {
	t.Helper()
	defer stream.Close()
	for {
		event, err := stream.Next()
		if err == io.EOF {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		if event.Type == protocol.EventDone || event.Type == protocol.EventError || event.Type == protocol.EventIncomplete {
			return
		}
	}
}

func bytes32(b byte) []byte {
	out := make([]byte, 32)
	for i := range out {
		out[i] = b
	}
	return out
}

func TestForwardOpenStripsNativePreviousResponseOnMiss(t *testing.T) {
	var got map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n")
	}))
	defer upstream.Close()

	client, err := NewForward(ForwardConfig{
		Endpoint:   "https://chatgpt.com/backend-api/codex/responses",
		HTTPClient: upstream.Client(),
		CredentialAuthority: ForwardCredentialFunc(func(context.Context, providers.DispatchRequest) (ForwardCredential, error) {
			return ForwardCredential{Authorization: "Bearer tok", TrustedAccountID: "acct-1"}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	client.endpoint = upstream.URL
	client.httpClient = upstream.Client()
	dispatch := canonicalRequest(t, `{"model":"openai/gpt-5.6","previous_response_id":"resp_missing","input":"hi"}`, "gpt-5.6")
	dispatch.ForwardHeaders = providers.NewForwardHeaders(map[string]string{"authorization": "Bearer tok"})
	stream, err := client.Open(context.Background(), dispatch)
	if err != nil {
		t.Fatal(err)
	}
	drainStream(t, stream)
	if _, exists := got["previous_response_id"]; exists {
		t.Fatalf("native miss kept previous_response_id: %#v", got)
	}
}

type ForwardCredentialFunc func(context.Context, providers.DispatchRequest) (ForwardCredential, error)

func (f ForwardCredentialFunc) Resolve(ctx context.Context, dispatch providers.DispatchRequest) (ForwardCredential, error) {
	return f(ctx, dispatch)
}
