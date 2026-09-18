package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/providers/openairesponses"
	"github.com/Wibias/Benes/internal/transport"
)

func TestResponsesCommitsSparseCustomGatewayLifecycle(t *testing.T) {
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

	provider, err := openairesponses.NewHardened(context.Background(), openairesponses.Config{
		Endpoint:          upstream.URL,
		APIKey:            "upstream-key",
		DestinationPolicy: transport.DestinationPolicy{AllowPrivateNetwork: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{DataPlaneToken: "local-secret", Providers: map[string]Provider{"custom": provider}})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"custom/gpt-compatible","store":false,"stream":true,"input":"hi"}`))
	req.Header.Set("Authorization", "Bearer local-secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	payloads := sseJSONPayloads(t, rr.Body.String())
	var types []string
	var completed map[string]any
	for _, payload := range payloads {
		typeName, _ := payload["type"].(string)
		types = append(types, typeName)
		if typeName == "response.completed" {
			completed, _ = payload["response"].(map[string]any)
		}
	}
	want := []string{
		"response.created", "response.output_item.added", "response.content_part.added",
		"response.output_text.delta", "response.output_text.done", "response.content_part.done",
		"response.output_item.done", "response.completed",
	}
	if len(types) != len(want) {
		t.Fatalf("types=%v", types)
	}
	for i := range want {
		if types[i] != want[i] {
			t.Fatalf("types[%d]=%s want=%s all=%v", i, types[i], want[i], types)
		}
	}
	if completed == nil || completed["status"] != "completed" || completed["parallel_tool_calls"] != true || completed["tool_choice"] != "auto" {
		t.Fatalf("completed=%#v", completed)
	}
	output, _ := completed["output"].([]any)
	if len(output) != 1 {
		t.Fatalf("output=%#v", completed["output"])
	}
	item, _ := output[0].(map[string]any)
	if item["type"] != "message" || item["role"] != "assistant" {
		t.Fatalf("item=%#v", item)
	}
	content, _ := item["content"].([]any)
	if len(content) != 1 {
		t.Fatalf("content=%#v", item["content"])
	}
	part, _ := content[0].(map[string]any)
	if part["type"] != "output_text" || part["text"] != "hello" {
		t.Fatalf("part=%#v", part)
	}
	if _, ok := part["annotations"].([]any); !ok {
		t.Fatalf("annotations=%#v", part["annotations"])
	}
}

func sseJSONPayloads(t *testing.T, body string) []map[string]any {
	t.Helper()
	var payloads []map[string]any
	for _, block := range strings.Split(body, "\n\n") {
		block = strings.TrimSpace(block)
		if !strings.HasPrefix(block, "data:") {
			continue
		}
		raw := strings.TrimSpace(strings.TrimPrefix(block, "data:"))
		if raw == "" {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			t.Fatalf("decode %q: %v", raw, err)
		}
		payloads = append(payloads, payload)
	}
	if len(payloads) == 0 {
		t.Fatalf("no SSE payloads in %q", body)
	}
	return payloads
}
