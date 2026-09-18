package openairesponses

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
)

func TestCompileOmitsStoreWhenCanonicalRequestHasNoStorePolicy(t *testing.T) {
	body, err := Compile(protocol.ParsedRequest{
		Source:          protocol.RequestSourceResponses,
		UpstreamModelID: "gpt-test",
		Options:         protocol.RequestOptions{},
	})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if _, exists := got["store"]; exists {
		t.Fatalf("compiler invented store policy: %s", body)
	}
}

func TestCompilePreservesExplicitStoreFalse(t *testing.T) {
	value := false
	body, err := Compile(protocol.ParsedRequest{
		Source:          protocol.RequestSourceResponses,
		UpstreamModelID: "gpt-test",
		Options:         protocol.RequestOptions{Store: &value},
	})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if string(got["store"]) != "false" {
		t.Fatalf("explicit store:false not preserved: %s", body)
	}
}

func TestForwardCanonicalBodyScopesOmittedStoreToNativeCodexOnly(t *testing.T) {
	request := protocol.ParsedRequest{
		Source:          protocol.RequestSourceResponses,
		UpstreamModelID: "gpt-test",
		Raw:             json.RawMessage(`{"model":"gpt-test","input":"hi"}`),
	}
	body, err := prepareForwardCanonicalBody(request, canonicalForwardResponsesEndpoint, "")
	if err != nil {
		t.Fatal(err)
	}
	var native map[string]json.RawMessage
	if err := json.Unmarshal(body, &native); err != nil {
		t.Fatal(err)
	}
	if string(native["store"]) != "false" {
		t.Fatalf("canonical native forward omitted store:false: %s", body)
	}

	body, err = prepareForwardCanonicalBody(request, "https://gateway.example/codex/responses", "")
	if err != nil {
		t.Fatal(err)
	}
	var custom map[string]json.RawMessage
	if err := json.Unmarshal(body, &custom); err != nil {
		t.Fatal(err)
	}
	if _, exists := custom["store"]; exists {
		t.Fatalf("custom forward gateway received native store policy: %s", body)
	}
}

func TestForwardCanonicalBodyPreservesExplicitStoreValues(t *testing.T) {
	truth := true
	falsehood := false
	for _, value := range []*bool{&truth, &falsehood} {
		body, err := prepareForwardCanonicalBody(protocol.ParsedRequest{
			Source:          protocol.RequestSourceResponses,
			UpstreamModelID: "gpt-test",
			Raw:             json.RawMessage(`{"model":"gpt-test","input":"hi"}`),
			Options:         protocol.RequestOptions{Store: value},
		}, canonicalForwardResponsesEndpoint, "")
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]json.RawMessage
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatal(err)
		}
		want := "false"
		if *value {
			want = "true"
		}
		if string(got["store"]) != want {
			t.Fatalf("explicit store=%v not preserved: %s", *value, body)
		}
	}
}

func TestForwardOpenInjectsStoreFalseOnlyAfterCanonicalForwardAuth(t *testing.T) {
	var got map[string]json.RawMessage
	client, err := NewForward(ForwardConfig{
		Endpoint: testCanonicalForwardResponsesEndpoint,
		HTTPClient: forwardSSEClient(t, func(request *http.Request) {
			raw, err := io.ReadAll(request.Body)
			if err != nil {
				t.Errorf("read body: %v", err)
				return
			}
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Errorf("decode body: %v", err)
			}
		}),
		MaxStreamBytes:    1 << 20,
		InactivityTimeout: time.Second,
		CredentialAuthority: forwardCredentialAuthorityFunc(func(context.Context, providers.DispatchRequest) (ForwardCredential, error) {
			return ForwardCredential{Authorization: "Bearer selected", ChatGPTAccountID: "acct"}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}

	dispatch := canonicalRequest(t, `{"model":"openai/gpt-5.6","input":"hi"}`, "gpt-5.6")
	stream, err := client.Open(context.Background(), dispatch)
	if err != nil {
		t.Fatalf("Open(): %v", err)
	}
	defer stream.Close()
	if _, err := stream.Next(); err != nil {
		t.Fatalf("Next(): %v", err)
	}
	if string(got["store"]) != "false" {
		t.Fatalf("canonical forward omitted store: %v", got)
	}

	dispatch = canonicalRequest(t, `{"model":"openai/gpt-5.6","store":true,"input":"hi"}`, "gpt-5.6")
	stream, err = client.Open(context.Background(), dispatch)
	if err != nil {
		t.Fatalf("explicit store Open(): %v", err)
	}
	defer stream.Close()
	if _, err := stream.Next(); err != nil {
		t.Fatalf("explicit store Next(): %v", err)
	}
	if string(got["store"]) != "true" {
		t.Fatalf("explicit store:true not preserved: %v", got)
	}
}
