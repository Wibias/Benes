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
)

func TestPrepareCanonicalBodyAllowsStructuredNamespaceTools(t *testing.T) {
	request := canonicalRequest(t, `{"model":"openai-apikey/gpt-5.6","store":false,"tools":[{"type":"namespace","name":"exec","tools":[{"type":"function","name":"exec","parameters":{"type":"object"}}]}]}`, "gpt-5.6")
	body, err := prepareCanonicalBody(request.Parsed)
	if err != nil {
		t.Fatalf("structured namespace rejected: %v", err)
	}
	var compiled map[string]any
	if err := json.Unmarshal(body, &compiled); err != nil {
		t.Fatal(err)
	}
	tools, ok := compiled["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("compiled tools=%#v", compiled["tools"])
	}
	tool, ok := tools[0].(map[string]any)
	if !ok || tool["name"] != "exec__exec" {
		t.Fatalf("compiled tool=%#v", tools[0])
	}
}

func TestStreamRebindsMaterializedWireAliasToLogicalToolIdentity(t *testing.T) {
	var upstreamBody map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&upstreamBody); err != nil {
			t.Errorf("decode upstream body: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"type\":\"response.output_item.added\",\"item\":{\"type\":\"function_call\",\"id\":\"fc_1\",\"call_id\":\"call_1\",\"name\":\"exec__exec\",\"arguments\":\"\",\"status\":\"in_progress\"}}\n\n")
	}))
	defer upstream.Close()

	client, err := New(Config{
		Endpoint:          upstream.URL,
		APIKey:            "k",
		HTTPClient:        upstream.Client(),
		MaxStreamBytes:    1 << 20,
		InactivityTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}

	request := canonicalRequest(t, `{"model":"openai-apikey/gpt-5.6","store":false,"tools":[{"type":"namespace","name":"exec","tools":[{"type":"function","name":"exec","parameters":{"type":"object"}}]}]}`, "gpt-5.6")
	stream, err := client.Open(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()

	event, err := stream.Next()
	if err != nil {
		t.Fatal(err)
	}
	if event.Type != protocol.EventToolCallStart {
		t.Fatalf("event type=%s", event.Type)
	}
	if event.Name != "exec" || event.Namespace != "exec" {
		t.Fatalf("tool identity=(%q,%q), want (exec,exec)", event.Namespace, event.Name)
	}

	tools, ok := upstreamBody["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("upstream tools=%#v", upstreamBody["tools"])
	}
	tool, ok := tools[0].(map[string]any)
	if !ok || tool["name"] != "exec__exec" {
		t.Fatalf("upstream tool=%#v", tools[0])
	}
}

func TestToolIdentityStreamDoesNotSynthesizeUndeclaredNamespace(t *testing.T) {
	request := protocol.ParsedRequest{Context: protocol.Context{Tools: []protocol.Tool{{
		Namespace: "exec", Name: "exec", Parameters: map[string]any{"type": "object"},
	}}}}
	stream := wrapToolIdentityStream(&singleToolEventStream{event: protocol.Event{
		Type: protocol.EventToolCallStart, ID: "call_1", Name: "ghost__exec",
	}}, request)
	event, err := stream.Next()
	if err != nil {
		t.Fatal(err)
	}
	if event.Name != "ghost__exec" || event.Namespace != "" {
		t.Fatalf("undeclared alias gained identity: namespace=%q name=%q", event.Namespace, event.Name)
	}
}

func TestToolIdentityStreamKeepsDefaultExecBare(t *testing.T) {
	request := protocol.ParsedRequest{Context: protocol.Context{Tools: []protocol.Tool{{
		Name: "exec", Parameters: map[string]any{"type": "object"},
	}}}}
	stream := wrapToolIdentityStream(&singleToolEventStream{event: protocol.Event{
		Type: protocol.EventToolCallStart, ID: "call_1", Name: "exec",
	}}, request)
	event, err := stream.Next()
	if err != nil {
		t.Fatal(err)
	}
	if event.Name != "exec" || event.Namespace != "" {
		t.Fatalf("default exec identity changed: namespace=%q name=%q", event.Namespace, event.Name)
	}
}

func TestMaterializedToolIdentitiesKeepSameNameNamespacesDistinct(t *testing.T) {
	request := protocol.ParsedRequest{Context: protocol.Context{Tools: []protocol.Tool{
		{Namespace: "a", Name: "lookup", Parameters: map[string]any{"type": "object"}},
		{Namespace: "b", Name: "lookup", Parameters: map[string]any{"type": "object"}},
	}}}
	byWire, err := materializedToolIdentities(request)
	if err != nil {
		t.Fatal(err)
	}
	if got := byWire["a__lookup"]; got.Namespace != "a" || got.Name != "lookup" {
		t.Fatalf("a mapping=%+v", got)
	}
	if got := byWire["b__lookup"]; got.Namespace != "b" || got.Name != "lookup" {
		t.Fatalf("b mapping=%+v", got)
	}
}

func TestMaterializedToolIdentitiesExcludeDeferredToolsUntilRequired(t *testing.T) {
	request := protocol.ParsedRequest{Context: protocol.Context{Tools: []protocol.Tool{
		{Name: "tool_search", ToolSearch: true, Parameters: map[string]any{"type": "object"}},
		{Namespace: "a", Name: "lookup", LoadedFromToolSearch: true, Parameters: map[string]any{"type": "object"}},
	}}}
	byWire, err := materializedToolIdentities(request)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := byWire["a__lookup"]; ok {
		t.Fatalf("deferred alias was authorized before materialization")
	}

	request.Options.ToolChoice = &protocol.ToolChoice{Kind: protocol.ToolChoiceNamed, Name: "a.lookup"}
	byWire, err = materializedToolIdentities(request)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := byWire["a__lookup"]; !ok || got.Namespace != "a" || got.Name != "lookup" {
		t.Fatalf("required deferred mapping=%+v present=%v", got, ok)
	}
}

type singleToolEventStream struct {
	event protocol.Event
	used  bool
}

func (s *singleToolEventStream) Next() (protocol.Event, error) {
	if s.used {
		return protocol.Event{}, io.EOF
	}
	s.used = true
	return s.event, nil
}

func (s *singleToolEventStream) Close() error { return nil }
