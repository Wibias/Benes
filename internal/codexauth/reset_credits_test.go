package codexauth

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type resetRT func(*http.Request) (*http.Response, error)

func (f resetRT) Do(req *http.Request) (*http.Response, error) { return f(req) }

const testRedeemRequestID = "550e8400-e29b-41d4-a716-446655440000"

func TestValidRedeemRequestIDAcceptsCanonicalUUIDv4(t *testing.T) {
	if !ValidRedeemRequestID(testRedeemRequestID) {
		t.Fatal("canonical v4 id rejected")
	}
	generated, err := NewRedeemRequestID()
	if err != nil || !ValidRedeemRequestID(generated) {
		t.Fatalf("generated=%q err=%v", generated, err)
	}
	for _, id := range []string{"", "abcd1234", "550E8400-E29B-41D4-A716-446655440000", "550e8400-e29b-11d4-a716-446655440000"} {
		if ValidRedeemRequestID(id) {
			t.Fatalf("accepted %q", id)
		}
	}
}

func TestConsumeResetCreditRefusesInvalidRedeemRequestID(t *testing.T) {
	prev := ResetCreditHTTP
	t.Cleanup(func() { ResetCreditHTTP = prev })
	ResetCreditHTTP = resetRT(func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid redeemRequestId must not reach WHAM")
		return nil, nil
	})
	_, status, err := ConsumeResetCredit(context.Background(), ManagedToken{AccessToken: "access", ChatGPTAccountID: "chat"}, "abcd1234")
	if err == nil || status != 0 {
		t.Fatalf("status=%d err=%v", status, err)
	}
}

func TestFetchResetCreditsNeverFollowsRedirectsAndStaysOnCanonicalHost(t *testing.T) {
	prev := ResetCreditHTTP
	t.Cleanup(func() { ResetCreditHTTP = prev })
	ResetCreditHTTP = resetRT(func(req *http.Request) (*http.Response, error) {
		if req.URL.Hostname() != "chatgpt.com" {
			t.Fatalf("host=%s", req.URL.Host)
		}
		if req.Header.Get("Authorization") != "Bearer access" {
			t.Fatalf("auth=%s", req.Header.Get("Authorization"))
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"credits":[{"granted_at":"a","expires_at":"b"}],"available_count":2}`)), Header: make(http.Header)}, nil
	})
	dto, status, err := FetchResetCredits(context.Background(), ManagedToken{AccessToken: "access", ChatGPTAccountID: "chat"})
	if err != nil || status != 200 || dto.AvailableCount == nil || *dto.AvailableCount != 2 || len(dto.Credits) != 1 {
		t.Fatalf("dto=%+v status=%d err=%v", dto, status, err)
	}
}
