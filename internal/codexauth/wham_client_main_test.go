package codexauth

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type mainQuotaRoundTripperFunc func(*http.Request) (*http.Response, error)

func (f mainQuotaRoundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestWHAMClientFetchMainClassifiesOnlyRetainedTerminalAuthResponses(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{name: "401", status: http.StatusUnauthorized, want: true},
		{name: "detail workspace 403", status: http.StatusForbidden, body: "{\"detail\":{\"code\":\"invalid_workspace_selected\"}}", want: false},

		{name: "error refresh 403", status: http.StatusForbidden, body: "{\"error\":{\"code\":\"invalid_refresh_token\"}}", want: true},
		{name: "root refresh 403", status: http.StatusForbidden, body: "{\"code\":\"invalid_refresh_token\"}", want: true},
		{name: "unknown 403", status: http.StatusForbidden, body: "{\"code\":\"permission_denied\"}", want: false},
		{name: "detail precedence masks root", status: http.StatusForbidden, body: "{\"detail\":{\"code\":\"permission_denied\"},\"code\":\"invalid_refresh_token\"}", want: false},
		{name: "malformed 403", status: http.StatusForbidden, body: "{", want: false},
		{name: "500", status: http.StatusInternalServerError, body: "{\"code\":\"invalid_refresh_token\"}", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, err := NewWHAMClient(WHAMClientConfig{HTTPClient: &http.Client{Transport: mainQuotaRoundTripperFunc(func(request *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tc.status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(tc.body)), Request: request}, nil
			})}})
			if err != nil {
				t.Fatal(err)
			}
			result, err := client.FetchMain(context.Background(), ManagedToken{AccessToken: "access", ChatGPTAccountID: "chat"}, "plus")
			if err != nil {
				t.Fatal(err)
			}
			if result.StatusCode != tc.status || result.TerminalMainAuth != tc.want {
				t.Fatalf("result=%#v want terminal=%v", result, tc.want)
			}
		})
	}
}

func TestWHAMClientFetchMainBounds403EvidenceBody(t *testing.T) {
	client, err := NewWHAMClient(WHAMClientConfig{HTTPClient: &http.Client{Transport: mainQuotaRoundTripperFunc(func(request *http.Request) (*http.Response, error) {
		body := strings.Repeat("x", defaultWHAMMaxBodyBytes+1)
		return &http.Response{StatusCode: http.StatusForbidden, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
	})}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.FetchMain(context.Background(), ManagedToken{AccessToken: "access", ChatGPTAccountID: "chat"}, "plus")
	if err != nil {
		t.Fatal(err)
	}
	if result.TerminalMainAuth {
		t.Fatalf("oversized 403 body produced terminal auth evidence=%#v", result)
	}
}

func TestWHAMClientManagedFetchDoesNotInspectMainAuthEvidence(t *testing.T) {
	closed := false
	body := &mainQuotaCloseTrackingBody{Reader: strings.NewReader("{\"code\":\"invalid_refresh_token\"}"), closed: &closed}
	client, err := NewWHAMClient(WHAMClientConfig{HTTPClient: &http.Client{Transport: mainQuotaRoundTripperFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusForbidden, Header: make(http.Header), Body: body, Request: request}, nil
	})}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Fetch(context.Background(), ManagedToken{AccessToken: "access", ChatGPTAccountID: "chat"}, "plus")
	if err != nil {
		t.Fatal(err)
	}
	if result.StatusCode != http.StatusForbidden || result.TerminalMainAuth {
		t.Fatalf("managed result=%#v", result)
	}
	if !closed {
		t.Fatal("managed non-2xx body was not closed")
	}
}

type mainQuotaCloseTrackingBody struct {
	*strings.Reader
	closed *bool
}

func (b *mainQuotaCloseTrackingBody) Close() error {
	*b.closed = true
	return nil
}
