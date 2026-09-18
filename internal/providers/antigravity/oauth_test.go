package antigravity

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestExtractProjectIDReadsKnownShapes(t *testing.T) {
	if got := ExtractProjectID([]byte(`{"cloudaicompanionProject":"p1"}`)); got != "p1" {
		t.Fatalf("direct=%q", got)
	}
	if got := ExtractProjectID([]byte(`{"project":{"id":"p2"}}`)); got != "p2" {
		t.Fatalf("nested=%q", got)
	}
	if got := ExtractProjectID([]byte(`{"done":true,"response":{"projectId":"p3"}}`)); got != "p3" {
		t.Fatalf("onboard=%q", got)
	}
}

func TestDiscoverProjectUsesValidatedHTTPSBeforeBearer(t *testing.T) {
	var sawAuth, sawPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		sawPath = r.URL.Path
		if r.URL.Scheme == "http" && r.Host != "" {
			t.Errorf("cleartext destination %s", r.Host)
		}
		io.WriteString(w, `{"cloudaicompanionProject":"discovered"}`)
	}))
	defer upstream.Close()
	client := &http.Client{Transport: rewriteHost{base: upstream.URL, next: http.DefaultTransport}}
	got, err := DiscoverProject(context.Background(), client, "http://daily-cloudcode-pa.googleapis.com", "tok")
	if err != nil || got != "discovered" {
		t.Fatalf("discover=%q err=%v", got, err)
	}
	if sawAuth != "Bearer tok" || !strings.Contains(sawPath, "loadCodeAssist") {
		t.Fatalf("auth=%q path=%q", sawAuth, sawPath)
	}
	if _, err := DiscoverProject(context.Background(), client, "http://evil.example", "tok"); err == nil {
		t.Fatal("foreign destination")
	}
}

func TestRefreshAccessTokenReadsGrant(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		values, _ := url.ParseQuery(string(body))
		if values.Get("grant_type") != "refresh_token" || values.Get("refresh_token") != "rt" {
			t.Fatalf("form=%v", values)
		}
		if values.Get("client_id") != oauthClientID {
			t.Fatalf("client_id=%q", values.Get("client_id"))
		}
		if _, ok := values["client_secret"]; ok {
			t.Fatalf("refresh request must not send client_secret: %v", values)
		}
		io.WriteString(w, `{"access_token":"new-access","expires_in":3600}`)
	}))
	defer upstream.Close()
	client := &http.Client{Transport: rewriteHost{base: upstream.URL, next: http.DefaultTransport}}
	got, err := RefreshAccessToken(context.Background(), client, "rt")
	if err != nil || got != "new-access" {
		t.Fatalf("refresh=%q err=%v", got, err)
	}
}
