package openairesponses

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/providers"
)

func fileAffinityDispatch(t *testing.T) providers.DispatchRequest {
	t.Helper()
	return canonicalRequest(t, `{
		"model":"openai/gpt-5.6",
		"input":[{
			"role":"user",
			"content":[
				{"type":"input_image","file_id":"file-img","detail":"high"},
				{"type":"input_file","file_id":"file-doc","filename":"doc.pdf"},
				{"type":"input_file","file_data":"ZGF0YQ==","filename":"inline.txt"}
			]
		}]
	}`, "gpt-5.6")
}

func successfulForwardResponse() *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(
			"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_ok\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n",
		)),
	}
}

func fileContentBlocks(t *testing.T, body map[string]any) []any {
	t.Helper()
	input, ok := body["input"].([]any)
	if !ok || len(input) != 1 {
		t.Fatalf("input=%#v", body["input"])
	}
	message, ok := input[0].(map[string]any)
	if !ok {
		t.Fatalf("message=%#v", input[0])
	}
	content, ok := message["content"].([]any)
	if !ok {
		t.Fatalf("content=%#v", message["content"])
	}
	return content
}

func contentBlockByType(t *testing.T, blocks []any, typ string, index int) map[string]any {
	t.Helper()
	seen := 0
	for _, raw := range blocks {
		block, ok := raw.(map[string]any)
		if !ok || block["type"] != typ {
			continue
		}
		if seen == index {
			return block
		}
		seen++
	}
	t.Fatalf("missing %s block %d in %#v", typ, index, blocks)
	return nil
}

func TestForwardPreservesResponsesFileReferences(t *testing.T) {
	var got map[string]any
	client, err := NewForward(ForwardConfig{
		Endpoint: testCanonicalForwardResponsesEndpoint,
		HTTPClient: &http.Client{Transport: forwardRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			if err := json.NewDecoder(req.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			return successfulForwardResponse(), nil
		})},
		CredentialAuthority: forwardCredentialAuthorityFunc(func(context.Context, providers.DispatchRequest) (ForwardCredential, error) {
			return ForwardCredential{
				Authorization:    "Bearer account-a",
				ChatGPTAccountID: "chat-a",
				TrustedAccountID: "account-a",
			}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}

	stream, err := client.Open(context.Background(), fileAffinityDispatch(t))
	if err != nil {
		t.Fatal(err)
	}
	drainStream(t, stream)

	blocks := fileContentBlocks(t, got)
	image := contentBlockByType(t, blocks, "input_image", 0)
	if image["file_id"] != "file-img" || image["detail"] != "high" {
		t.Fatalf("image=%#v", image)
	}
	file := contentBlockByType(t, blocks, "input_file", 0)
	if file["file_id"] != "file-doc" || file["filename"] != "doc.pdf" {
		t.Fatalf("file=%#v", file)
	}
	inline := contentBlockByType(t, blocks, "input_file", 1)
	if inline["file_data"] != "ZGF0YQ==" || inline["filename"] != "inline.txt" {
		t.Fatalf("inline=%#v", inline)
	}
}

func TestForwardQuotaRetryDoesNotReplayFileReferenceAcrossAccounts(t *testing.T) {
	calls := 0
	client, err := NewForward(ForwardConfig{
		Endpoint: testCanonicalForwardResponsesEndpoint,
		HTTPClient: &http.Client{Transport: forwardRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			if calls > 1 {
				t.Fatalf("file-bound request replayed across accounts: auth=%q", req.Header.Get("Authorization"))
			}
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"error":"quota"}`)),
			}, nil
		})},
		CredentialAuthority: forwardAttemptAuthorityFunc(func(context.Context, providers.DispatchRequest) (ForwardAttempt, error) {
			return ForwardAttempt{
				Credential: ForwardCredential{
					Authorization:    "Bearer account-a",
					ChatGPTAccountID: "chat-a",
					TrustedAccountID: "account-a",
				},
				RetryQuota: func(context.Context) (ForwardAttempt, bool, error) {
					return ForwardAttempt{Credential: ForwardCredential{
						Authorization:    "Bearer account-b",
						ChatGPTAccountID: "chat-b",
						TrustedAccountID: "account-b",
					}}, true, nil
				},
			}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := client.Open(context.Background(), fileAffinityDispatch(t)); err == nil || !strings.Contains(err.Error(), "429") {
		t.Fatalf("err=%v", err)
	}
	if calls != 1 {
		t.Fatalf("physical sends=%d want=1", calls)
	}
}

func TestForwardAuthRetryKeepsFileReferenceOnSameAccount(t *testing.T) {
	calls := 0
	var retryBody map[string]any
	client, err := NewForward(ForwardConfig{
		Endpoint: testCanonicalForwardResponsesEndpoint,
		HTTPClient: &http.Client{Transport: forwardRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			switch calls {
			case 1:
				return &http.Response{
					StatusCode: http.StatusUnauthorized,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(`{"error":"expired"}`)),
				}, nil
			case 2:
				if got := req.Header.Get("Authorization"); got != "Bearer refreshed-a" {
					t.Fatalf("retry auth=%q", got)
				}
				if err := json.NewDecoder(req.Body).Decode(&retryBody); err != nil {
					t.Fatal(err)
				}
				return successfulForwardResponse(), nil
			default:
				t.Fatalf("unexpected physical send %d", calls)
				return nil, nil
			}
		})},
		CredentialAuthority: forwardAttemptAuthorityFunc(func(context.Context, providers.DispatchRequest) (ForwardAttempt, error) {
			return ForwardAttempt{
				Credential: ForwardCredential{
					Authorization:    "Bearer stale-a",
					ChatGPTAccountID: "chat-a",
					TrustedAccountID: "account-a",
				},
				RetryAuth: func(context.Context) (ForwardAttempt, bool, error) {
					return ForwardAttempt{Credential: ForwardCredential{
						Authorization:    "Bearer refreshed-a",
						ChatGPTAccountID: "chat-a",
						TrustedAccountID: "account-a",
					}}, true, nil
				},
			}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}

	stream, err := client.Open(context.Background(), fileAffinityDispatch(t))
	if err != nil {
		t.Fatal(err)
	}
	drainStream(t, stream)

	blocks := fileContentBlocks(t, retryBody)
	if got := contentBlockByType(t, blocks, "input_image", 0)["file_id"]; got != "file-img" {
		t.Fatalf("retried image file_id=%#v", got)
	}
	if calls != 2 {
		t.Fatalf("physical sends=%d want=2", calls)
	}
}
