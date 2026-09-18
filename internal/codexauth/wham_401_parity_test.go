package codexauth

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func whamJWTWithExp(exp int64) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"exp":%d}`, exp)))
	return header + "." + payload + ".sig"
}

func TestWHAMMain401RequiresVerifiablyLiveTokenBeforeTreatingBareResponseAsTransient(t *testing.T) {
	const farFutureExp = int64(4_102_444_800)
	for _, tc := range []struct {
		name  string
		token string
		body  string
		want  bool
	}{
		{name: "bare 401 with live token is transient", token: whamJWTWithExp(farFutureExp), want: false},
		{name: "terminal body still wins for live token", token: whamJWTWithExp(farFutureExp), body: `{"detail":{"code":"invalid_refresh_token"}}`, want: true},
		{name: "expired token makes bare 401 terminal", token: whamJWTWithExp(1), want: true},
		{name: "undecodable token makes bare 401 terminal", token: "not-a-jwt", want: true},
		{name: "missing exp makes bare 401 terminal", token: "eyJhbGciOiJSUzI1NiJ9.e30.sig", want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, err := NewWHAMClient(WHAMClientConfig{HTTPClient: &http.Client{Transport: mainQuotaRoundTripperFunc(func(request *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusUnauthorized, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(tc.body)), Request: request}, nil
			})}})
			if err != nil {
				t.Fatal(err)
			}
			result, err := client.FetchMain(context.Background(), ManagedToken{AccessToken: tc.token, ChatGPTAccountID: "acct"}, "plus")
			if err != nil {
				t.Fatal(err)
			}
			if result.TerminalMainAuth != tc.want {
				t.Fatalf("terminal=%v want=%v", result.TerminalMainAuth, tc.want)
			}
		})
	}
}
