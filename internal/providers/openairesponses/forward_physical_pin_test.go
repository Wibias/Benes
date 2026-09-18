package openairesponses

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Wibias/Benes/internal/codexauth"
	"github.com/Wibias/Benes/internal/providers"
)

// TestForwardClient_StreamReportsPhysicalPinFromTrustedAccount proves the
// Codex/openai-forward hole: Account A serves resp_A but the EventStream exposes
// no PhysicalOwnership, so Fabric PreferCommitted continuation cannot pin and the
// pool may select Account B.
func TestForwardClient_StreamReportsPhysicalPinFromTrustedAccount(t *testing.T) {
	var accounts []string
	roundTrip := forwardRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		accounts = append(accounts, r.Header.Get("ChatGPT-Account-Id"))
		body := "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_A\",\"status\":\"completed\",\"output\":[]}}\n\n"
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})

	var picks atomic.Int32
	authority := forwardAttemptAuthorityFunc(func(_ context.Context, dispatch providers.DispatchRequest) (ForwardAttempt, error) {
		n := picks.Add(1)
		if pin := dispatch.PhysicalPin; pin != nil && strings.TrimSpace(pin.CredentialRef) != "" {
			ref := strings.TrimSpace(pin.CredentialRef)
			if ref != "account-a" {
				t.Fatalf("pinned open used unexpected ref=%q dispatch=%#v", ref, dispatch)
			}
			return ForwardAttempt{
				Credential: ForwardCredential{
					Authorization:    "Bearer tok-a",
					ChatGPTAccountID: "chatgpt-a",
					TrustedAccountID: "account-a",
				},
			}, nil
		}
		if n == 1 {
			return ForwardAttempt{
				Credential: ForwardCredential{
					Authorization:    "Bearer tok-a",
					ChatGPTAccountID: "chatgpt-a",
					TrustedAccountID: "account-a",
				},
			}, nil
		}
		return ForwardAttempt{
			Credential: ForwardCredential{
				Authorization:    "Bearer tok-b",
				ChatGPTAccountID: "chatgpt-b",
				TrustedAccountID: "account-b",
			},
		}, nil
	})

	client, err := NewForward(ForwardConfig{
		Endpoint:            testCanonicalForwardResponsesEndpoint,
		HTTPClient:          &http.Client{Transport: roundTrip},
		CredentialAuthority: authority,
	})
	if err != nil {
		t.Fatal(err)
	}

	stream, err := client.Open(context.Background(), observedForwardDispatch(t))
	if err != nil {
		t.Fatal(err)
	}
	for {
		_, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	reporter, ok := stream.(interface{ PhysicalOwnership() *providers.PhysicalPin })
	if !ok {
		t.Fatalf("forward stream %T hides PhysicalOwnership", stream)
	}
	pin := reporter.PhysicalOwnership()
	_ = stream.Close()
	if pin == nil || pin.CredentialRef != "account-a" || pin.AuthClass != "forward" {
		t.Fatalf("PhysicalOwnership=%#v want CredentialRef=account-a AuthClass=forward (Fabric cannot PreferCommitted-pin Account A)", pin)
	}
	if len(accounts) != 1 || accounts[0] != "chatgpt-a" {
		t.Fatalf("primary accounts=%#v", accounts)
	}

	// Unpinned PreferCommitted would let the mock pick Account B — prove pin forces A.
	stream2, err := client.Open(context.Background(), providers.DispatchRequest{
		Parsed:          observedForwardDispatch(t).Parsed,
		ForwardHeaders:  providers.NewForwardHeaders(nil),
		PreferCommitted: true,
		PhysicalPin:     pin,
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = stream2.Close()
	if len(accounts) < 2 || accounts[1] != "chatgpt-a" {
		t.Fatalf("PreferCommitted+PhysicalPin must stay on Account A; accounts=%#v", accounts)
	}
}

// TestCodexPoolAuthority_PhysicalPinForcesFixedAccount proves pool selection
// treats PhysicalPin.CredentialRef like an exact Codex account fix so PreferCommitted
// reopen cannot hop A→B.
func TestCodexPoolAuthority_PhysicalPinForcesFixedAccount(t *testing.T) {
	snapshot := &fakePoolSnapshotSource{}
	resolver := &recordingFixedPoolResolver{}
	authority, err := NewCodexPoolAuthority(snapshot, resolver)
	if err != nil {
		t.Fatal(err)
	}
	dispatch := observedForwardDispatch(t)
	dispatch.PreferCommitted = true
	dispatch.PhysicalPin = &providers.PhysicalPin{
		Destination:   testCanonicalForwardResponsesEndpoint,
		CredentialRef: "account-a",
		AuthClass:     "forward",
	}
	cred, err := authority.Resolve(context.Background(), dispatch)
	if err != nil {
		t.Fatal(err)
	}
	if cred.TrustedAccountID != "account-a" {
		t.Fatalf("TrustedAccountID=%q want account-a", cred.TrustedAccountID)
	}
	if resolver.last.FixedAccountID != "account-a" {
		t.Fatalf("pool request FixedAccountID=%q want account-a (PhysicalPin ignored)", resolver.last.FixedAccountID)
	}
}

type recordingFixedPoolResolver struct {
	last codexauth.PoolCredentialRequest
}

func (r *recordingFixedPoolResolver) Resolve(_ context.Context, req codexauth.PoolCredentialRequest) (codexauth.PoolCredential, error) {
	r.last = req
	fixed := strings.TrimSpace(req.FixedAccountID)
	if fixed == "" {
		fixed = "account-b"
	}
	return codexauth.PoolCredential{
		AccountID:        fixed,
		AccessToken:      "tok-" + fixed,
		ChatGPTAccountID: "chatgpt-" + fixed,
		FixedAccount:     fixed != "",
	}, nil
}
