package openairesponses

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/transport"
)

func TestClientRelayImageUsesProviderKeyAndDerivedPath(t *testing.T) {
	var gotAuth, gotPath, gotCaller string
	var gotBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotCaller = r.Header.Get("X-Caller-Secret")
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"created":1,"data":[{"b64_json":"QQ=="}]}`))
	}))
	t.Cleanup(upstream.Close)
	client, err := New(Config{
		Endpoint:   upstream.URL + "/v1/responses",
		APIKey:     "sk-upstream",
		HTTPClient: upstream.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	status, header, payload, err := client.RelayImage(context.Background(), providers.DispatchRequest{
		ForwardHeaders: providers.NewForwardHeaders(map[string]string{
			"authorization":   "Bearer local-secret",
			"x-caller-secret": "do-not-forward",
		}),
	}, providers.ImageRelayRequest{
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
	if gotAuth != "Bearer sk-upstream" {
		t.Fatalf("auth=%q", gotAuth)
	}
	if gotCaller != "" {
		t.Fatalf("unknown header leaked: %q", gotCaller)
	}
	if !strings.HasSuffix(gotPath, "/v1/images/generations") {
		t.Fatalf("path=%q", gotPath)
	}
	if !strings.Contains(gotBody, "a cat") {
		t.Fatalf("body=%s", gotBody)
	}
	if header.Get("Content-Type") == "" {
		t.Fatal("missing content type")
	}
	if !strings.Contains(string(payload), "b64_json") {
		t.Fatalf("payload=%s", payload)
	}
}

func TestResponsesBodyLimitDoesNotApplyToImageRelay(t *testing.T) {
	var gotBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"created":1,"data":[]}`))
	}))
	t.Cleanup(upstream.Close)

	client, err := NewHardened(context.Background(), Config{
		Endpoint:          upstream.URL + "/v1/responses",
		APIKey:            "sk-upstream",
		DestinationPolicy: transport.DestinationPolicy{AllowPrivateNetwork: true},
		TransportOptions: transport.ClientOptions{
			MaxRequestBodyBytes:  16,
			RequestBodyLimitPath: "/v1/responses",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	imageBody := []byte(`{"model":"gpt-image-2","prompt":"this image request is deliberately larger than the Responses text-body limit"}`)
	status, _, _, err := client.RelayImage(context.Background(), providers.DispatchRequest{}, providers.ImageRelayRequest{
		Kind:        providers.ImageRelayGenerations,
		Body:        imageBody,
		ContentType: "application/json",
	})
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK {
		t.Fatalf("status=%d", status)
	}
	if gotBody != string(imageBody) {
		t.Fatalf("body=%q", gotBody)
	}
}

func TestForwardClientRelayImageUsesPoolCredentialNotAdmission(t *testing.T) {
	var gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"created":1,"data":[]}`))
	}))
	t.Cleanup(upstream.Close)
	client := &ForwardClient{
		endpoint:            upstream.URL + "/backend-api/codex/responses",
		nativeForward:       true,
		httpClient:          upstream.Client(),
		credentialAuthority: staticForwardAuthority{authorization: "Bearer chatgpt-pool-token"},
	}
	status, _, _, err := client.RelayImage(context.Background(), providers.DispatchRequest{
		ForwardHeaders: providers.NewForwardHeadersWithBlockedAuthorization(map[string]string{
			"authorization": "Bearer local-secret",
		}, true),
	}, providers.ImageRelayRequest{
		Kind:        providers.ImageRelayEdits,
		Body:        []byte(`{"model":"gpt-image-2","prompt":"edit"}`),
		ContentType: "application/json",
	})
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK {
		t.Fatalf("status=%d", status)
	}
	if gotAuth != "Bearer chatgpt-pool-token" {
		t.Fatalf("auth=%q", gotAuth)
	}
	if gotAuth == "Bearer local-secret" {
		t.Fatal("admission secret was relayed upstream")
	}
}

type staticForwardAuthority struct {
	authorization string
}

func (s staticForwardAuthority) Resolve(context.Context, providers.DispatchRequest) (ForwardCredential, error) {
	return ForwardCredential{Authorization: s.authorization}, nil
}
