package openaichat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/credentials"
	"github.com/Wibias/Benes/internal/gatewayrouting"
	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/resourcebudget"
	"github.com/Wibias/Benes/internal/timeline"
	"github.com/Wibias/Benes/internal/transport"
)

func TestNewChatClientValidatesEndpointCredentialAndHTTPClient(t *testing.T) {
	validHTTP := &http.Client{}
	for _, tc := range []struct {
		name   string
		config Config
	}{
		{name: "userinfo", config: Config{Endpoint: "https://user:pass@example.com/v1/chat/completions", APIKey: "k", HTTPClient: validHTTP}},
		{name: "scheme", config: Config{Endpoint: "ftp://example.com/v1/chat/completions", APIKey: "k", HTTPClient: validHTTP}},
		{name: "credential", config: Config{Endpoint: "https://example.com/v1/chat/completions", HTTPClient: validHTTP}},
		{name: "client", config: Config{Endpoint: "https://example.com/v1/chat/completions", APIKey: "k"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := New(tc.config); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

type captureRoundTrip struct {
	requests []*http.Request
	handler  func(*http.Request) (*http.Response, error)
}

func (c *captureRoundTrip) RoundTrip(req *http.Request) (*http.Response, error) {
	c.requests = append(c.requests, req.Clone(req.Context()))
	if c.handler != nil {
		return c.handler(req)
	}
	return sseChatResponse(), nil
}

func sseChatResponse() *http.Response {
	body := "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n data: [DONE]\n\n"
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestChatClientOpenSeparatesXAIOAuthProxyFromAPIKeyOrigin(t *testing.T) {
	oauthTrip := &captureRoundTrip{}
	oauthClient, err := New(Config{
		Endpoint:   "https://cli-chat-proxy.grok.com/v1/chat/completions",
		APIKey:     "oauth-bearer",
		HTTPClient: &http.Client{Transport: oauthTrip},
	})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := oauthClient.Open(context.Background(), providers.DispatchRequest{Parsed: protocol.ParsedRequest{UpstreamModelID: "grok-4.6"}})
	if err != nil {
		t.Fatal(err)
	}
	stream.Close()
	if len(oauthTrip.requests) != 1 {
		t.Fatalf("oauth requests=%d", len(oauthTrip.requests))
	}
	oauthReq := oauthTrip.requests[0]
	if oauthReq.URL.Host != "cli-chat-proxy.grok.com" || oauthReq.Header.Get("Authorization") != "Bearer oauth-bearer" {
		t.Fatalf("oauth request url=%s auth=%q", oauthReq.URL, oauthReq.Header.Get("Authorization"))
	}
	if oauthReq.Header.Get("x-xai-token-auth") != "xai-grok-cli" ||
		oauthReq.Header.Get("x-grok-client-identifier") != "grok-shell" ||
		oauthReq.Header.Get("x-grok-req-id") == "" {
		t.Fatalf("oauth headers=%v", oauthReq.Header)
	}

	keyTrip := &captureRoundTrip{}
	keyClient, err := New(Config{
		Endpoint:   "https://api.x.ai/v1/chat/completions",
		APIKey:     "sk-xai-live",
		HTTPClient: &http.Client{Transport: keyTrip},
	})
	if err != nil {
		t.Fatal(err)
	}
	keyStream, err := keyClient.Open(context.Background(), providers.DispatchRequest{Parsed: protocol.ParsedRequest{UpstreamModelID: "grok-4.6"}})
	if err != nil {
		t.Fatal(err)
	}
	keyStream.Close()
	if len(keyTrip.requests) != 1 {
		t.Fatalf("key requests=%d", len(keyTrip.requests))
	}
	keyReq := keyTrip.requests[0]
	if keyReq.URL.Host != "api.x.ai" || keyReq.Header.Get("Authorization") != "Bearer sk-xai-live" {
		t.Fatalf("key request url=%s auth=%q", keyReq.URL, keyReq.Header.Get("Authorization"))
	}
	if keyReq.Header.Get("x-xai-token-auth") != "" || keyReq.Header.Get("x-grok-client-identifier") != "" {
		t.Fatalf("API-key request carried CLI headers: %v", keyReq.Header)
	}
}

func TestChatClientOAuth401IsTerminalOnSameAccountOrigin(t *testing.T) {
	trip := &captureRoundTrip{handler: func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusUnauthorized,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"error":"unauthorized"}`)),
		}, nil
	}}
	client, err := New(Config{
		Endpoint:   "https://cli-chat-proxy.grok.com/v1/chat/completions",
		APIKey:     "oauth-bearer",
		HTTPClient: &http.Client{Transport: trip},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Open(context.Background(), providers.DispatchRequest{Parsed: protocol.ParsedRequest{UpstreamModelID: "grok-4.6"}})
	if err == nil {
		t.Fatal("expected 401 to be terminal")
	}
	if len(trip.requests) != 1 {
		t.Fatalf("requests=%d", len(trip.requests))
	}
	req := trip.requests[0]
	if req.URL.Host != "cli-chat-proxy.grok.com" || req.Header.Get("Authorization") != "Bearer oauth-bearer" {
		t.Fatalf("401 recovery hopped transport: url=%s auth=%q", req.URL, req.Header.Get("Authorization"))
	}
}

func TestChatClientOAuthRetryKeepsRequestIdentity(t *testing.T) {
	var reqIDs []string
	attempts := 0
	trip := &captureRoundTrip{handler: func(req *http.Request) (*http.Response, error) {
		attempts++
		reqIDs = append(reqIDs, req.Header.Get("x-grok-req-id"))
		if attempts == 1 {
			return &http.Response{
				StatusCode: http.StatusBadGateway,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("bad gateway")),
			}, nil
		}
		return sseChatResponse(), nil
	}}
	client, err := New(Config{
		Endpoint:     "https://cli-chat-proxy.grok.com/v1/chat/completions",
		APIKey:       "oauth-bearer",
		HTTPClient:   &http.Client{Transport: trip},
		Transient5xx: transport.Transient5xxPolicy{Enabled: true, Attempts: 2, Sleep: func(context.Context, time.Duration) error { return nil }},
	})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := client.Open(context.Background(), providers.DispatchRequest{Parsed: protocol.ParsedRequest{UpstreamModelID: "grok-4.6"}})
	if err != nil {
		t.Fatal(err)
	}
	stream.Close()
	if attempts != 2 || len(reqIDs) != 2 || reqIDs[0] == "" || reqIDs[0] != reqIDs[1] {
		t.Fatalf("attempts=%d ids=%v", attempts, reqIDs)
	}
	if trip.requests[0].Header.Get("Authorization") != trip.requests[1].Header.Get("Authorization") ||
		trip.requests[0].URL.Host != trip.requests[1].URL.Host {
		t.Fatal("retry hopped bearer or origin")
	}
}

func TestChatClientOpenForcesUpstreamStreamingAndOwnAuthorization(t *testing.T) {
	var gotAuth, gotAccept, gotContentType, gotWindow string
	var gotBody map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAccept = r.Header.Get("Accept")
		gotContentType = r.Header.Get("Content-Type")
		gotWindow = r.Header.Get("X-Codex-Window-ID")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"},\"finish_reason\":\"stop\"}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()

	client, err := New(Config{
		Endpoint:   upstream.URL,
		APIKey:     "upstream-key",
		HTTPClient: upstream.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	var _ providers.Responses = client

	request := protocol.ParsedRequest{
		ModelID:         "selector/client",
		UpstreamModelID: "gpt-5.6",
		Stream:          false,
		Raw:             json.RawMessage(`{"model":"WRONG","stream":false,"future_field":"no"}`),
		Context: protocol.Context{Messages: []protocol.Message{{
			Role:    protocol.RoleUser,
			Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}},
		}}},
	}
	stream, err := client.Open(context.Background(), providers.DispatchRequest{
		Parsed: request,
		ForwardHeaders: providers.NewForwardHeaders(map[string]string{
			"authorization":     "caller-auth-marker",
			"x-codex-window-id": "window-1",
		}),
	})
	if err != nil {
		t.Fatalf("Open(): %v", err)
	}
	defer stream.Close()

	first, err := stream.Next()
	if err != nil || first.Type != protocol.EventTextDelta || first.Text != "hello" {
		t.Fatalf("first=%#v err=%v", first, err)
	}
	done, err := stream.Next()
	if err != nil || done.Type != protocol.EventDone {
		t.Fatalf("done=%#v err=%v", done, err)
	}
	if _, err := stream.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("terminal err=%v", err)
	}

	if gotAuth != "Bearer upstream-key" || gotAccept != "text/event-stream" || !strings.HasPrefix(gotContentType, "application/json") {
		t.Fatalf("headers auth=%q accept=%q content-type=%q", gotAuth, gotAccept, gotContentType)
	}
	if gotWindow != "" {
		t.Fatalf("forward metadata leaked upstream: %q", gotWindow)
	}
	if gotBody["model"] != "gpt-5.6" || gotBody["stream"] != true {
		t.Fatalf("body=%#v", gotBody)
	}
	if _, ok := gotBody["future_field"]; ok {
		t.Fatalf("preserved raw field leaked: %#v", gotBody)
	}
}

func TestChatClientResolvesCredentialRefFromStore(t *testing.T) {
	store, err := credentials.NewFileStore(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := store.Put("chat", []byte("from-store"))
	if err != nil {
		t.Fatal(err)
	}
	var gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()
	client, err := New(Config{
		Endpoint:      upstream.URL,
		HTTPClient:    upstream.Client(),
		Credentials:   store,
		CredentialRef: ref,
	})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := client.Open(context.Background(), providers.DispatchRequest{Parsed: protocol.ParsedRequest{UpstreamModelID: "m"}})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if _, err := stream.Next(); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer from-store" {
		t.Fatalf("auth=%q", gotAuth)
	}
}

func TestChatOpenReservesTranslatorStreamAndBlobBudgetClasses(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"},\"finish_reason\":\"stop\"}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()
	client, err := New(Config{Endpoint: upstream.URL, APIKey: "k", HTTPClient: upstream.Client(), MaxStreamBytes: 1 << 20, InactivityTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 1, MaxTurnBytes: 1 << 20, MaxProcessBytes: 1 << 20})
	turn, err := budget.AcquireTurn(context.Background(), "t")
	if err != nil {
		t.Fatal(err)
	}
	defer turn.Close()
	stream, err := client.Open(context.Background(), providers.DispatchRequest{
		Turn: turn,
		Parsed: protocol.ParsedRequest{
			UpstreamModelID: "gpt-5.6",
			Context: protocol.Context{Messages: []protocol.Message{{
				Role: protocol.RoleUser,
				Content: []protocol.ContentPart{
					{Type: protocol.ContentText, Text: "hi"},
					{Type: protocol.ContentImage, ImageURL: "data:image/png;base64,AAAA"},
				},
			}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := stream.Next(); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatal(err)
		}
	}
	_ = stream.Close()
	metrics := budget.Metrics()
	if metrics.Bytes[resourcebudget.ClassTranslator] == 0 || metrics.Bytes[resourcebudget.ClassStreamPending] == 0 || metrics.Bytes[resourcebudget.ClassBlob] == 0 {
		t.Fatalf("missing class reservations: %#v", metrics.Bytes)
	}
}

func TestClinePassDeepSeekV4TargetsOnlyFlashAndPro(t *testing.T) {
	if !clinePassDeepSeekV4("cline-pass", "deepseek-v4-flash") || !clinePassDeepSeekV4("cline-pass", "deepseek-v4-pro") {
		t.Fatal("flash/pro must be targeted")
	}
	if clinePassDeepSeekV4("cline-pass", "minimax-m3") || clinePassDeepSeekV4("openrouter", "deepseek-v4-flash") {
		t.Fatal("non-target models/providers must stay unchanged")
	}
}

func TestChatClientDoesNotExposeNonSuccessBody(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":{"message":"SECRET-UPSTREAM-BODY"}}`)
	}))
	defer upstream.Close()
	client, err := New(Config{Endpoint: upstream.URL, APIKey: "k", HTTPClient: upstream.Client()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Open(context.Background(), providers.DispatchRequest{Parsed: protocol.ParsedRequest{UpstreamModelID: "m"}})
	if err == nil || strings.Contains(err.Error(), "SECRET-UPSTREAM-BODY") || !strings.Contains(err.Error(), "HTTP 401") {
		t.Fatalf("err=%v", err)
	}
}

func TestChatClientRejectsNonSSESuccess(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[]}`)
	}))
	defer upstream.Close()
	client, err := New(Config{Endpoint: upstream.URL, APIKey: "k", HTTPClient: upstream.Client()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Open(context.Background(), providers.DispatchRequest{Parsed: protocol.ParsedRequest{UpstreamModelID: "m"}})
	if err == nil || !strings.Contains(err.Error(), "text/event-stream") {
		t.Fatalf("err=%v", err)
	}
}

func TestChatClientBoundsCopiedStreamBytes(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q},\"finish_reason\":null}]}\n\n", strings.Repeat("x", 4096))
	}))
	defer upstream.Close()
	client, err := New(Config{
		Endpoint:          upstream.URL,
		APIKey:            "k",
		HTTPClient:        upstream.Client(),
		MaxStreamBytes:    128,
		InactivityTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := client.Open(context.Background(), providers.DispatchRequest{Parsed: protocol.ParsedRequest{UpstreamModelID: "m"}})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if _, err := stream.Next(); err == nil {
		t.Fatal("expected bounded stream error")
	}
}

func TestNewHardenedChatClientUsesValidatedPinnedTransport(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()

	client, err := NewHardened(context.Background(), Config{
		Endpoint:          upstream.URL,
		APIKey:            "k",
		DestinationPolicy: transport.DestinationPolicy{AllowPrivateNetwork: true},
	})
	if err != nil {
		t.Fatalf("NewHardened(): %v", err)
	}
	stream, err := client.Open(context.Background(), providers.DispatchRequest{Parsed: protocol.ParsedRequest{UpstreamModelID: "m"}})
	if err != nil {
		t.Fatalf("Open(): %v", err)
	}
	defer stream.Close()
	event, err := stream.Next()
	if err != nil || event.Type != protocol.EventTextDelta || event.Text != "ok" {
		t.Fatalf("event=%#v err=%v", event, err)
	}
}

type rewriteHostTransport struct {
	base http.RoundTripper
	to   *url.URL
}

func (t rewriteHostTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.URL.Scheme = t.to.Scheme
	clone.URL.Host = t.to.Host
	clone.Host = t.to.Host
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(clone)
}

func TestChatClientAppliesCanonicalVercelGatewayRoutingAndOverride(t *testing.T) {
	var gotBody map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()
	target, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	client, err := New(Config{
		Endpoint: "https://ai-gateway.vercel.sh/v1/chat/completions",
		APIKey:   "k",
		HTTPClient: &http.Client{Transport: rewriteHostTransport{
			base: upstream.Client().Transport,
			to:   target,
		}},
		GatewayRouting: gatewayrouting.Settings{
			Provider: gatewayrouting.Policy{Only: []string{"anthropic"}, Sort: gatewayrouting.SortCost},
			Models: map[string]gatewayrouting.Policy{
				"anthropic/claude-sonnet-5": {Order: []string{"vertex", "anthropic"}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	trace := timeline.New("gw-route", 8)
	stream, err := client.Open(timeline.WithTrace(context.Background(), trace), providers.DispatchRequest{
		Parsed: protocol.ParsedRequest{UpstreamModelID: "anthropic/claude-sonnet-5"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if _, err := stream.Next(); err != nil {
		t.Fatal(err)
	}
	gateway, _ := gotBody["providerOptions"].(map[string]any)["gateway"].(map[string]any)
	if gateway == nil {
		t.Fatalf("missing gateway options: %#v", gotBody)
	}
	if _, ok := gateway["only"]; ok {
		t.Fatalf("model override must replace provider only: %#v", gateway)
	}
	if gateway["sort"] != nil {
		t.Fatalf("model override must replace provider sort: %#v", gateway)
	}
	order, _ := gateway["order"].([]any)
	if len(order) != 2 || order[0] != "vertex" || order[1] != "anthropic" {
		t.Fatalf("order=%#v", gateway["order"])
	}
	if _, ok := gotBody["service_tier"]; ok {
		t.Fatal("must not invent service_tier")
	}
	if _, ok := gotBody["allow_fallbacks"]; ok {
		t.Fatal("must not emit OpenRouter allow_fallbacks")
	}
	if _, ok := gotBody["provider"]; ok {
		t.Fatal("must not emit OpenRouter provider pin")
	}
	found := false
	for _, event := range trace.Events() {
		if strings.Contains(event.Cause, `"applied":true`) && strings.Contains(event.Cause, "requested_gateway_routing") {
			found = true
		}
		if strings.Contains(event.Cause, "served") || strings.Contains(event.Cause, "backend served") {
			t.Fatalf("diagnostics claimed served backend: %s", event.Cause)
		}
	}
	if !found {
		t.Fatalf("missing requested routing diagnostic: %#v", trace.Events())
	}
}

func TestChatClientDoesNotApplyGatewayRoutingToLookalikeHost(t *testing.T) {
	var gotBody map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()
	client, err := New(Config{
		Endpoint:   upstream.URL,
		APIKey:     "k",
		HTTPClient: upstream.Client(),
		ProviderID: "vercel-ai-gateway",
		GatewayRouting: gatewayrouting.Settings{
			Provider: gatewayrouting.Policy{Only: []string{"anthropic"}, Sort: gatewayrouting.SortTTFT},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := client.Open(context.Background(), providers.DispatchRequest{
		Parsed: protocol.ParsedRequest{UpstreamModelID: "anthropic/claude-sonnet-5"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if _, err := stream.Next(); err != nil {
		t.Fatal(err)
	}
	if _, ok := gotBody["providerOptions"]; ok {
		t.Fatalf("lookalike host received vendor fields: %#v", gotBody)
	}
}

func TestChatClientEmptyGatewayPolicyKeepsCompiledBody(t *testing.T) {
	var raw []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()
	target, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	client, err := New(Config{
		Endpoint: "https://ai-gateway.vercel.sh/v1/chat/completions",
		APIKey:   "k",
		HTTPClient: &http.Client{Transport: rewriteHostTransport{
			base: upstream.Client().Transport,
			to:   target,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := client.Open(context.Background(), providers.DispatchRequest{
		Parsed: protocol.ParsedRequest{UpstreamModelID: "anthropic/claude-sonnet-5"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	_, _ = stream.Next()
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if _, ok := body["providerOptions"]; ok {
		t.Fatalf("empty policy added providerOptions: %s", raw)
	}
}

func TestChatClientRetriesTransient503WhenPolicyEnabled(t *testing.T) {
	hits := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()
	client, err := New(Config{
		Endpoint:   upstream.URL,
		APIKey:     "k",
		HTTPClient: upstream.Client(),
		Transient5xx: transport.Transient5xxPolicy{Enabled: true, Attempts: 2, Sleep: func(context.Context, time.Duration) error {
			return nil
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := client.Open(context.Background(), providers.DispatchRequest{
		Parsed: protocol.ParsedRequest{
			UpstreamModelID: "gpt-5.6",
			Context: protocol.Context{Messages: []protocol.Message{{
				Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}},
			}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if hits != 2 {
		t.Fatalf("hits=%d", hits)
	}
}

func TestChatClientDoesNotRetry503WhenPolicyDisabled(t *testing.T) {
	hits := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer upstream.Close()
	client, err := New(Config{Endpoint: upstream.URL, APIKey: "k", HTTPClient: upstream.Client()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Open(context.Background(), providers.DispatchRequest{
		Parsed: protocol.ParsedRequest{UpstreamModelID: "gpt-5.6"},
	})
	if err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("err=%v", err)
	}
	if hits != 1 {
		t.Fatalf("hits=%d", hits)
	}
}
