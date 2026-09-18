package openairesponses

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
)

func TestOpenUsesCanonicalBodyForChatSource(t *testing.T) {
	var got map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{}}\n\n")
	}))
	defer upstream.Close()

	client, err := New(Config{Endpoint: upstream.URL, APIKey: "k", HTTPClient: upstream.Client(), InactivityTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	request := protocol.ParsedRequest{
		Source:          protocol.RequestSourceChatCompletions,
		UpstreamModelID: "gpt-real",
		Raw:             json.RawMessage(`{"model":"raw-chat","future_field":{"x":1}}`),
		Context: protocol.Context{Messages: []protocol.Message{{
			Role: protocol.RoleUser,
			Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "canonical"}},
		}}},
	}
	stream, err := client.Open(context.Background(), providers.DispatchRequest{Parsed: request})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if _, err := stream.Next(); err != nil {
		t.Fatal(err)
	}
	if got["model"] != "gpt-real" {
		t.Fatalf("body=%#v", got)
	}
	if _, ok := got["future_field"]; ok {
		t.Fatalf("raw leaked: %#v", got)
	}
}
