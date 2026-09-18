package codexauth

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/transport"
)

type quotaRoundTripperFunc func(*http.Request) (*http.Response, error)

func (f quotaRoundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func TestWHAMClientUsesCanonicalGETAndCredentialHeaders(t *testing.T) {
	var got *http.Request
	client, err := NewWHAMClient(WHAMClientConfig{HTTPClient: &http.Client{Transport: quotaRoundTripperFunc(func(request *http.Request) (*http.Response, error) {
		got = request.Clone(context.Background())
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body: io.NopCloser(strings.NewReader(`{"plan_type":"plus","rate_limit":{"primary_window":{"used_percent":23}}}`)),
			Request: request,
		}, nil
	})}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Fetch(context.Background(), ManagedToken{AccessToken: "access", ChatGPTAccountID: "chat"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Method != http.MethodGet || got.URL.String() != canonicalWHAMUsageEndpoint {
		t.Fatalf("request=%#v", got)
	}
	if got.Header.Get("Authorization") != "Bearer access" || got.Header.Get("ChatGPT-Account-Id") != "chat" {
		t.Fatalf("headers=%#v", got.Header)
	}
	if len(got.Header) != 2 {
		t.Fatalf("unexpected headers=%#v", got.Header)
	}
	if result.StatusCode != http.StatusOK || result.Quota.Quota == nil || result.Quota.Quota.WeeklyPercent == nil || *result.Quota.Quota.WeeklyPercent != 23 {
		t.Fatalf("result=%#v", result)
	}
}

func TestWHAMClientDoesNotFollowRedirects(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	client, err := NewWHAMClient(WHAMClientConfig{HTTPClient: &http.Client{Transport: quotaRoundTripperFunc(func(request *http.Request) (*http.Response, error) {
		mu.Lock()
		calls++
		current := calls
		mu.Unlock()
		if current == 1 {
			return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": []string{"https://example.com/steal"}}, Body: io.NopCloser(strings.NewReader("redirect")), Request: request}, nil
		}
		return nil, errors.New("redirect followed")
	})}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Fetch(context.Background(), ManagedToken{AccessToken: "a", ChatGPTAccountID: "c"}, "plus")
	if err != nil {
		t.Fatal(err)
	}
	if result.StatusCode != http.StatusFound {
		t.Fatalf("result=%#v", result)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Fatalf("calls=%d", calls)
	}
}

func TestWHAMClientBoundsSuccessBodyAndClosesIt(t *testing.T) {
	closed := false
	body := &quotaCloseTrackingBody{Reader: bytes.NewReader(bytes.Repeat([]byte("x"), defaultWHAMMaxBodyBytes+1)), closed: &closed}
	client, err := NewWHAMClient(WHAMClientConfig{HTTPClient: &http.Client{Transport: quotaRoundTripperFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: body, Request: request}, nil
	})}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Fetch(context.Background(), ManagedToken{AccessToken: "a", ChatGPTAccountID: "c"}, "plus")
	var streamErr *transport.StreamError
	if !errors.As(err, &streamErr) || streamErr.Kind != transport.StreamErrorByteLimit {
		t.Fatalf("err=%T %v", err, err)
	}
	if !closed {
		t.Fatal("body not closed")
	}
}

func TestWHAMClientAppliesTotalTimeoutAndCallerCancellation(t *testing.T) {
	client, err := NewWHAMClient(WHAMClientConfig{
		Timeout: 20 * time.Millisecond,
		HTTPClient: &http.Client{Transport: quotaRoundTripperFunc(func(request *http.Request) (*http.Response, error) {
			<-request.Context().Done()
			return nil, request.Context().Err()
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Fetch(context.Background(), ManagedToken{AccessToken: "a", ChatGPTAccountID: "c"}, "plus")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout err=%v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.Fetch(ctx, ManagedToken{AccessToken: "a", ChatGPTAccountID: "c"}, "plus")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel err=%v", err)
	}
}

func TestNewWHAMClientRejectsMissingHTTPClient(t *testing.T) {
	if _, err := NewWHAMClient(WHAMClientConfig{}); err == nil {
		t.Fatal("expected hardened HTTP client requirement")
	}
}

type quotaCloseTrackingBody struct {
	*bytes.Reader
	closed *bool
}

func (b *quotaCloseTrackingBody) Close() error {
	*b.closed = true
	return nil
}
