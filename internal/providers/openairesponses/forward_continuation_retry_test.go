package openairesponses

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/resourcebudget"
	"github.com/Wibias/Benes/internal/responses/continuation"
)

func TestForwardQuotaRetryRebindsContinuationToAlternateCredential(t *testing.T) {
	store := continuation.NewStore(
		continuation.StoreLimits{TTL: time.Hour},
		time.Now,
	)
	continuationAuthority, err := continuation.NewAuthority(store, nil, bytes32('8'))
	if err != nil {
		t.Fatal(err)
	}

	resolveCalls := 0
	credentialAuthority := forwardAttemptAuthorityFunc(
		func(context.Context, providers.DispatchRequest) (ForwardAttempt, error) {
			resolveCalls++
			switch resolveCalls {
			case 1:
				return ForwardAttempt{
					Credential: ForwardCredential{
						Authorization:    "Bearer account-a",
						ChatGPTAccountID: "chat-a",
						TrustedAccountID: "account-a",
					},
				}, nil

			case 2:
				return ForwardAttempt{
					Credential: ForwardCredential{
						Authorization:    "Bearer account-a",
						ChatGPTAccountID: "chat-a",
						TrustedAccountID: "account-a",
					},
					RetryQuota: func(context.Context) (ForwardAttempt, bool, error) {
						return ForwardAttempt{
							Credential: ForwardCredential{
								Authorization:    "Bearer account-b",
								ChatGPTAccountID: "chat-b",
								TrustedAccountID: "account-b",
							},
						}, true, nil
					},
				}, nil

			case 3:
				return ForwardAttempt{
					Credential: ForwardCredential{
						Authorization:    "Bearer account-b",
						ChatGPTAccountID: "chat-b",
						TrustedAccountID: "account-b",
					},
				}, nil

			default:
				t.Fatalf("unexpected credential resolution call %d", resolveCalls)
				return ForwardAttempt{}, nil
			}
		},
	)

	sse := func(id string) *http.Response {
		messageID := "msg_" + id
		body := fmt.Sprintf(
			`data: {"type":"response.completed","response":{"id":%q,"status":"completed","output":[{"type":"message","id":%q,"role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`+"\n\n",
			id,
			messageID,
		)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(body)),
		}
	}

	var alternateBody map[string]any
	var followBody map[string]any
	calls := 0

	client, err := NewForward(ForwardConfig{
		Endpoint: testCanonicalForwardResponsesEndpoint,
		HTTPClient: &http.Client{
			Transport: forwardRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++

				switch calls {
				case 1:
					if got := req.Header.Get("Authorization"); got != "Bearer account-a" {
						t.Fatalf("seed authorization=%q", got)
					}
					return sse("resp_a"), nil

				case 2:
					if got := req.Header.Get("Authorization"); got != "Bearer account-a" {
						t.Fatalf("first retry authorization=%q", got)
					}
					return &http.Response{
						StatusCode: http.StatusTooManyRequests,
						Header:     make(http.Header),
						Body:       io.NopCloser(strings.NewReader(`{"error":"quota"}`)),
					}, nil

				case 3:
					if got := req.Header.Get("Authorization"); got != "Bearer account-b" {
						t.Fatalf("alternate authorization=%q", got)
					}
					if err := json.NewDecoder(req.Body).Decode(&alternateBody); err != nil {
						t.Fatal(err)
					}
					return sse("resp_b"), nil

				case 4:
					if got := req.Header.Get("Authorization"); got != "Bearer account-b" {
						t.Fatalf("follow authorization=%q", got)
					}
					if err := json.NewDecoder(req.Body).Decode(&followBody); err != nil {
						t.Fatal(err)
					}
					return sse("resp_c"), nil

				default:
					t.Fatalf("unexpected upstream call %d", calls)
					return nil, nil
				}
			}),
		},
		CredentialAuthority: credentialAuthority,
		Continuation:        continuationAuthority,
	})
	if err != nil {
		t.Fatal(err)
	}

	budget := resourcebudget.NewManager(resourcebudget.Limits{
		MaxActiveTurns: 1,
		MaxTurnBytes:    1 << 20,
		MaxProcessBytes: 1 << 20,
		ClassBytes: map[resourcebudget.Class]int64{
			resourcebudget.ClassContinuation: 1 << 20,
		},
	})

	run := func(id string, dispatch providers.DispatchRequest) {
		t.Helper()

		turn, err := budget.AcquireTurn(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		dispatch.Turn = turn

		stream, err := client.Open(context.Background(), dispatch)
		if err != nil {
			turn.Close()
			t.Fatal(err)
		}

		drainStream(t, stream)
		turn.Close()
	}

	run(
		"seed-a",
		canonicalRequest(
			t,
			`{"model":"openai/gpt-5.6","input":"seed"}`,
			"gpt-5.6",
		),
	)

	run(
		"rotate-a-to-b",
		canonicalRequest(
			t,
			`{"model":"openai/gpt-5.6","previous_response_id":"resp_a","input":"next"}`,
			"gpt-5.6",
		),
	)

	run(
		"follow-b",
		canonicalRequest(
			t,
			`{"model":"openai/gpt-5.6","previous_response_id":"resp_b","input":"continue"}`,
			"gpt-5.6",
		),
	)

	if previous, exists := alternateBody["previous_response_id"]; exists {
		t.Errorf(
			"alternate credential retained first account continuation: previous_response_id=%v",
			previous,
		)
	}

	if got := followBody["previous_response_id"]; got != "resp_b" {
		t.Errorf(
			"alternate response was not persisted under alternate continuation owner: previous_response_id=%v",
			got,
		)
	}
}
