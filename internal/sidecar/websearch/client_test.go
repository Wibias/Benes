package websearch

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

	"github.com/Wibias/Benes/internal/authpublic"
)

func TestValidateSelectionFailsClosedOffResponsesKeyCapability(t *testing.T) {
	base := Config{
		Enabled: true, ProviderID: "opencode-zen", Endpoint: "https://opencode.ai/v1/search",
		AuthClass: "key", Wire: "openai-responses", ModelID: "deepseek-v4-flash",
		AllowedModels: []string{"deepseek-v4-flash"}, APIKey: "k",
	}
	if err := ValidateSelection(base); err != nil {
		t.Fatal(err)
	}
	off := base
	off.Enabled = false
	if err := ValidateSelection(off); !errors.Is(err, ErrUnsupportedSelection) {
		t.Fatalf("disabled=%v", err)
	}
	forward := base
	forward.AuthClass = "forward"
	if err := ValidateSelection(forward); !errors.Is(err, ErrUnsupportedSelection) {
		t.Fatalf("forward=%v", err)
	}
	chat := base
	chat.Wire = "openai-chat"
	if err := ValidateSelection(chat); !errors.Is(err, ErrUnsupportedSelection) {
		t.Fatalf("chat=%v", err)
	}
	anthropic := base
	anthropic.Wire = "anthropic-messages"
	if err := ValidateSelection(anthropic); err != nil {
		t.Fatalf("anthropic=%v", err)
	}
	searchJSON := base
	searchJSON.Wire = "search-json"
	if err := ValidateSelection(searchJSON); err != nil {
		t.Fatalf("search-json=%v", err)
	}
	wrongModel := base
	wrongModel.ModelID = "other"
	if err := ValidateSelection(wrongModel); !errors.Is(err, ErrUnsupportedSelection) {
		t.Fatalf("model=%v", err)
	}
}

func TestSearchSendsBearerKeyRefusesRedirectsAndRedactsBodies(t *testing.T) {
	var gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path == "/secret" {
			http.Redirect(w, r, "/final", http.StatusFound)
			return
		}
		if r.URL.Path == "/final" {
			t.Error("redirect was followed")
		}
		if r.URL.Path == "/fail" {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"error":"SECRET-UPSTREAM-BODY sk-leaked"}`)
			return
		}
		io.WriteString(w, `{"sources":[{"url":"https://example.com","title":"Example"}]}`)
	}))
	defer upstream.Close()
	client, err := New(Config{
		Enabled: true, ProviderID: "opencode-zen", Endpoint: upstream.URL,
		AuthClass: "key", Wire: "openai-responses", ModelID: "deepseek-v4-flash",
		AllowedModels: []string{"deepseek-v4-flash"}, APIKey: "sidecar-key", HTTPClient: upstream.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := client.Search(context.Background(), "benes")
	if err != nil || len(got.Sources) != 1 || got.Sources[0].URL != "https://example.com" {
		t.Fatalf("search=%#v err=%v", got, err)
	}
	if gotAuth != "Bearer sidecar-key" {
		t.Fatalf("auth=%q", gotAuth)
	}

	failing, err := New(Config{
		Enabled: true, ProviderID: "opencode-zen", Endpoint: upstream.URL + "/fail",
		AuthClass: "key", Wire: "openai-responses", ModelID: "deepseek-v4-flash",
		AllowedModels: []string{"deepseek-v4-flash"}, APIKey: "sidecar-key", HTTPClient: upstream.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = failing.Search(context.Background(), "benes")
	if err == nil || strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), "sk-leaked") {
		t.Fatalf("leaked=%v", err)
	}

	redirecting := *failing
	redirecting.endpoint = upstream.URL + "/secret"
	_, err = redirecting.Search(context.Background(), "benes")
	var sidecar authpublic.SidecarError
	if err == nil || !errors.As(err, &sidecar) || sidecar.Status != http.StatusFound {
		t.Fatalf("redirect=%v", err)
	}
}

func TestSearchJSONSanitizesProviderCitationSources(t *testing.T) {
	longURL := "https://example.com/" + strings.Repeat("x", 2048)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{
			"text":"ok",
			"sources":[
				{"url":"javascript:alert(1)","title":"xss"},
				{"url":"https://user:pass@evil.example/","title":"creds"},
				{"url":" https://padded.example","title":"padded"},
				{"url":"https://example.com","title":"Example"},
				{"url":"https://example.com","title":"Duplicate"},
				{"url":"https://second.example","title":"Dirty\u0000title"},
				{"url":"`+longURL+`","title":"too-long"}
			]
		}`)
	}))
	t.Cleanup(upstream.Close)
	client, err := New(Config{
		Enabled: true, ProviderID: "opencode-zen", Endpoint: upstream.URL,
		AuthClass: "key", Wire: "openai-responses", ModelID: "deepseek-v4-flash",
		AllowedModels: []string{"deepseek-v4-flash"}, APIKey: "sidecar-key", HTTPClient: upstream.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := client.Search(context.Background(), "benes")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Sources) != 2 {
		t.Fatalf("sources=%#v", got.Sources)
	}
	if got.Sources[0].URL != "https://example.com" || got.Sources[0].Title != "Example" {
		t.Fatalf("first=%#v", got.Sources[0])
	}
	if got.Sources[1].URL != "https://second.example" || got.Sources[1].Title != "" {
		t.Fatalf("second=%#v", got.Sources[1])
	}
}

func TestSearchJSONKeepsValidSourcesAfterRejectingEarlierUnsafeOnes(t *testing.T) {
	sources := make([]string, 0, 9)
	for i := 0; i < 8; i++ {
		sources = append(sources, fmt.Sprintf(`{"url":"javascript:alert(%d)"}`, i))
	}
	sources = append(sources, `{"url":"https://kept.example","title":"Kept"}`)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"sources":[%s]}`, strings.Join(sources, ","))
	}))
	t.Cleanup(upstream.Close)
	client, err := New(Config{
		Enabled: true, ProviderID: "opencode-zen", Endpoint: upstream.URL,
		AuthClass: "key", Wire: "openai-responses", ModelID: "deepseek-v4-flash",
		AllowedModels: []string{"deepseek-v4-flash"}, APIKey: "sidecar-key", HTTPClient: upstream.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := client.Search(context.Background(), "benes")
	if err != nil || len(got.Sources) != 1 || got.Sources[0].URL != "https://kept.example" {
		t.Fatalf("search=%#v err=%v", got, err)
	}
}

func TestSearchParsesAnthropicEventStream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"from sse\"}}\n\n")
	}))
	t.Cleanup(upstream.Close)
	client, err := New(Config{
		Enabled: true, ProviderID: "opencode-zen", Endpoint: upstream.URL,
		AuthClass: "key", Wire: "openai-responses", ModelID: "deepseek-v4-flash",
		AllowedModels: []string{"deepseek-v4-flash"}, APIKey: "sidecar-key", HTTPClient: upstream.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := client.Search(context.Background(), "benes")
	if err != nil || got.Text != "from sse" {
		t.Fatalf("search=%#v err=%v", got, err)
	}
}

func TestSearchAnthropicMessagesPostsHostedWebSearchBody(t *testing.T) {
	var gotBody map[string]any
	var gotAccept, gotVersion string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		gotVersion = r.Header.Get("anthropic-version")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n")
	}))
	t.Cleanup(upstream.Close)
	client, err := New(Config{
		Enabled: true, ProviderID: "anthropic", Endpoint: upstream.URL,
		AuthClass: "key", Wire: "anthropic-messages", ModelID: "claude-sonnet-5",
		AllowedModels: []string{"claude-sonnet-5"}, APIKey: "sidecar-key", HTTPClient: upstream.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := client.Search(context.Background(), "benes")
	if err != nil || got.Text != "ok" {
		t.Fatalf("search=%#v err=%v", got, err)
	}
	if gotAccept != "text/event-stream" || gotVersion != "2023-06-01" {
		t.Fatalf("headers accept=%q version=%q", gotAccept, gotVersion)
	}
	if gotBody["model"] != "claude-sonnet-5" || gotBody["stream"] != true {
		t.Fatalf("body=%#v", gotBody)
	}
	tools, _ := gotBody["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("tools=%#v", gotBody["tools"])
	}
	tool, _ := tools[0].(map[string]any)
	if tool["type"] != "web_search_20250305" || tool["name"] != "web_search" {
		t.Fatalf("tool=%#v", tool)
	}
}

func TestSearchKeepsLongAnthropicEventStreamText(t *testing.T) {
	answer := strings.Repeat("a", 5000)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":%q}}\n\n", answer)
	}))
	t.Cleanup(upstream.Close)
	client, err := New(Config{
		Enabled: true, ProviderID: "opencode-zen", Endpoint: upstream.URL,
		AuthClass: "key", Wire: "openai-responses", ModelID: "deepseek-v4-flash",
		AllowedModels: []string{"deepseek-v4-flash"}, APIKey: "sidecar-key", HTTPClient: upstream.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := client.Search(context.Background(), "benes")
	if err != nil || got.Text != answer {
		t.Fatalf("len=%d err=%v", len(got.Text), err)
	}
}
