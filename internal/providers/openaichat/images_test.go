package openaichat

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/capability"
	"github.com/Wibias/Benes/internal/providers"
)

func TestChatClientRelayImageUsesOwnAPIKey(t *testing.T) {
	var gotAuth, gotPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		_, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"created":1,"data":[]}`))
	}))
	t.Cleanup(upstream.Close)
	client, err := New(Config{
		Endpoint:   upstream.URL + "/v1/chat/completions",
		APIKey:     "sk-chat",
		HTTPClient: upstream.Client(),
		Capability: capability.Policy{AuthClass: "api-key", Endpoint: "https://api.openai.com/v1/chat/completions"},
	})
	if err != nil {
		t.Fatal(err)
	}
	status, _, _, err := client.RelayImage(context.Background(), providers.DispatchRequest{}, providers.ImageRelayRequest{
		Kind:        providers.ImageRelayGenerations,
		Body:        []byte(`{"model":"gpt-image-2","prompt":"a cat"}`),
		ContentType: "application/json",
	})
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK {
		t.Fatalf("status=%d", status)
	}
	if gotAuth != "Bearer sk-chat" {
		t.Fatalf("auth=%q", gotAuth)
	}
	if !strings.HasSuffix(gotPath, "/v1/images/generations") {
		t.Fatalf("path=%q", gotPath)
	}
}

func TestChatClientSupportsImageRelayOnlyForProvenOpenAI(t *testing.T) {
	openAI, err := New(Config{
		Endpoint:   "https://api.openai.com/v1/chat/completions",
		APIKey:     "sk",
		HTTPClient: &http.Client{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !openAI.SupportsImageRelay() {
		t.Fatal("api.openai.com chat client must support image relay")
	}
	xai, err := New(Config{
		Endpoint:   "https://api.x.ai/v1/chat/completions",
		APIKey:     "sk",
		HTTPClient: &http.Client{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if xai.SupportsImageRelay() {
		t.Fatal("xAI chat client must not be treated as an OpenAI image upstream")
	}
}
