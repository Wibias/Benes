package openairesponses

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/requestpolicy"
)

const serviceTierWireBaseRequest = `{"model":"openai/gpt-5.6","store":false,"input":"hi"}`

// The forwarded Responses body is the wire contract. The tier Benes admitted for the
// request must be visible in the exact bytes sent upstream, an explicit client tier
// must survive byte-for-byte, and a live settings change after admission must not
// rewrite a request that already carries its admitted tier.
func TestForwardWireCarriesAdmittedServiceTier(t *testing.T) {
	cases := []struct {
		name          string
		admittedWhile string
		changedTo     string
		request       string
		wantForwarded string
	}{
		{
			name:          "admitted tier reaches the wire when the client omits it",
			admittedWhile: "priority",
			request:       serviceTierWireBaseRequest,
			wantForwarded: "priority",
		},
		{
			name:          "a later settings change does not rewrite an admitted request",
			admittedWhile: "priority",
			changedTo:     "flex",
			request:       serviceTierWireBaseRequest,
			wantForwarded: "priority",
		},
		{
			name:          "explicit client tier wins over the admitted tier",
			admittedWhile: "priority",
			changedTo:     "flex",
			request:       `{"model":"openai/gpt-5.6","store":false,"input":"hi","service_tier":"default"}`,
			wantForwarded: "default",
		},
		{
			name:          "no admitted or explicit tier emits no field",
			admittedWhile: "",
			request:       serviceTierWireBaseRequest,
			wantForwarded: "",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			defer restoreRequestPolicy(t)
			admitted := admitServiceTier(t, testCase.admittedWhile)
			if testCase.changedTo != "" {
				publishRequestPolicy(t, testCase.changedTo)
			}

			var wire []byte
			client, err := NewForward(ForwardConfig{
				Endpoint: testCanonicalForwardResponsesEndpoint,
				HTTPClient: forwardSSEClient(t, func(request *http.Request) {
					body, readErr := io.ReadAll(request.Body)
					if readErr != nil {
						t.Errorf("read forwarded body: %v", readErr)
						return
					}
					wire = body
				}),
				MaxStreamBytes:    1 << 20,
				InactivityTimeout: time.Second,
			})
			if err != nil {
				t.Fatal(err)
			}
			dispatch := canonicalRequest(t, testCase.request, "gpt-5.6")
			dispatch.ConfiguredServiceTier = admitted
			dispatch.ForwardHeaders = providers.NewForwardHeaders(map[string]string{"authorization": "Bearer caller-oauth"})
			stream, err := client.Open(context.Background(), dispatch)
			if err != nil {
				t.Fatalf("Open(): %v", err)
			}
			defer stream.Close()
			if _, err := stream.Next(); err != nil {
				t.Fatalf("Next(): %v", err)
			}
			assertWireServiceTier(t, wire, testCase.wantForwarded)
		})
	}
}

// The api-key canonical client applies the admitted tier through the provider
// capability gate before serialization, so it must reach the wire and must not follow
// a settings change that arrived after admission.
func TestCanonicalWireCarriesAdmittedServiceTier(t *testing.T) {
	defer restoreRequestPolicy(t)
	admitted := admitServiceTier(t, "priority")
	publishRequestPolicy(t, "flex")

	var wire []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read upstream body: %v", err)
			return
		}
		wire = body
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{}}\n\n")
	}))
	defer upstream.Close()

	client, err := New(Config{Endpoint: upstream.URL, APIKey: "upstream-key", HTTPClient: upstream.Client(), MaxStreamBytes: 1 << 20, InactivityTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	dispatch := canonicalRequest(t, `{"model":"openai-apikey/gpt-5.6","store":false,"input":"hi"}`, "gpt-5.6")
	dispatch.ConfiguredServiceTier = admitted
	stream, err := client.Open(context.Background(), dispatch)
	if err != nil {
		t.Fatalf("Open(): %v", err)
	}
	defer stream.Close()
	if _, err := stream.Next(); err != nil {
		t.Fatalf("Next(): %v", err)
	}
	assertWireServiceTier(t, wire, "priority")
}

func assertWireServiceTier(t *testing.T, wire []byte, want string) {
	t.Helper()
	if len(wire) == 0 {
		t.Fatal("no outbound Responses body captured")
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(wire, &body); err != nil {
		t.Fatalf("outbound body=%s err=%v", wire, err)
	}
	raw, exists := body["service_tier"]
	if want == "" {
		if exists {
			t.Fatalf("outbound body invented service_tier: %s", wire)
		}
		return
	}
	if !exists {
		t.Fatalf("service_tier missing from outbound Responses body: %s", wire)
	}
	if got := string(raw); got != `"`+want+`"` {
		t.Fatalf("service_tier=%s want %q", got, want)
	}
}

// admitServiceTier publishes a live policy and freezes it the way a request boundary
// does, so the returned value is what an admitted request would carry.
func admitServiceTier(t *testing.T, policy string) string {
	t.Helper()
	publishRequestPolicy(t, policy)
	admitted := requestpolicy.AdmittedServiceTier()
	if admitted != policy {
		t.Fatalf("admitted tier=%q want %q", admitted, policy)
	}
	return admitted
}

func publishRequestPolicy(t *testing.T, tier string) {
	t.Helper()
	if err := requestpolicy.Publish(requestpolicy.Policy{ServiceTier: tier}); err != nil {
		t.Fatal(err)
	}
}

func restoreRequestPolicy(t *testing.T) {
	t.Helper()
	if err := requestpolicy.Publish(requestpolicy.Policy{}); err != nil {
		t.Fatal(err)
	}
}
