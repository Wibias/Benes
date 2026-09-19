package openairesponses

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/providers"
)

func TestResponsesClientUsesCallerUserAgentCompatibilityFallback(t *testing.T) {
	var gotUA string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_ua\",\"status\":\"completed\",\"output\":[]}}\n\n")
	}))
	t.Cleanup(upstream.Close)

	client, err := New(Config{
		Endpoint:          upstream.URL,
		APIKey:            "key",
		HTTPClient:        upstream.Client(),
		MaxStreamBytes:    1 << 20,
		InactivityTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	dispatch := canonicalRequest(t, `{"model":"openai-apikey/gpt-5.6","store":false,"input":"hi"}`, "gpt-5.6")
	dispatch.ForwardHeaders = providers.NewForwardHeaders(map[string]string{
		"user-agent": "codex-cli/9.9 compatibility-marker",
	})
	stream, err := client.Open(context.Background(), dispatch)
	if err != nil {
		t.Fatal(err)
	}
	_ = stream.Close()
	if gotUA != "codex-cli/9.9 compatibility-marker" {
		t.Fatalf("user-agent=%q", gotUA)
	}
}
