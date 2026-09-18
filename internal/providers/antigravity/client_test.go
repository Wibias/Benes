package antigravity

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/transport"
)

const ccaOK = "data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"ok\"}]}}]}}\n\n"

func newTestClient(t *testing.T, upstream *httptest.Server, accounts []Account, image bool) *Client {
	t.Helper()
	client, err := NewHardened(context.Background(), Config{
		Endpoint:          DailyAPI,
		Accounts:          accounts,
		CatalogModels:     []string{"google-antigravity/gemini-3.7-flash", "claude-sonnet-4-6"},
		HTTPClient:        &http.Client{Transport: rewriteHost{base: upstream.URL, next: http.DefaultTransport}},
		DestinationPolicy: transport.DestinationPolicy{AllowPrivateNetwork: true},
		ImageGeneration:   image,
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestClientOpenUsesCatalogAndSameSnapshotProject(t *testing.T) {
	var gotAuth, gotBody, gotPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, ccaOK)
	}))
	defer upstream.Close()
	client := newTestClient(t, upstream, []Account{{ID: "acct", Token: "tok", ProjectID: "proj-1"}}, false)
	stream, err := client.Open(context.Background(), providers.DispatchRequest{
		Parsed: protocol.ParsedRequest{UpstreamModelID: "gemini-3.7-flash"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	ev, err := stream.Next()
	if err != nil || ev.Text != "ok" {
		t.Fatalf("event=%#v err=%v", ev, err)
	}
	if gotAuth != "Bearer tok" || !strings.Contains(gotBody, `"project":"proj-1"`) || !strings.Contains(gotPath, "streamGenerateContent") {
		t.Fatalf("auth=%q path=%q body=%s", gotAuth, gotPath, gotBody)
	}
	if !strings.Contains(gotBody, `"userAgent":"antigravity"`) {
		t.Fatalf("missing CCA envelope: %s", gotBody)
	}
	if _, err := client.Open(context.Background(), providers.DispatchRequest{Parsed: protocol.ParsedRequest{UpstreamModelID: "unknown"}}); err == nil {
		t.Fatal("catalog must reject unknown models")
	}
}

func TestClientRejectsCleartextForeignDestination(t *testing.T) {
	if _, err := NewHardened(context.Background(), Config{Endpoint: "http://example.com"}); !errors.Is(err, ErrInvalidDestination) {
		t.Fatalf("err=%v", err)
	}
}

func TestClientRotatesAccountOnBoundedPreStream429(t *testing.T) {
	var seen []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		seen = append(seen, r.Header.Get("Authorization")+" "+string(raw))
		if strings.Contains(string(raw), `"project":"pa"`) {
			w.Header().Set("Retry-After", "7")
			http.Error(w, `{"error":{"status":"RESOURCE_EXHAUSTED","message":"quota exceeded"}}`, http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, ccaOK)
	}))
	defer upstream.Close()
	client := newTestClient(t, upstream, []Account{
		{ID: "a", Token: "ta", ProjectID: "pa"},
		{ID: "b", Token: "tb", ProjectID: "pb"},
	}, false)
	stream, err := client.Open(context.Background(), providers.DispatchRequest{
		Parsed: protocol.ParsedRequest{UpstreamModelID: "gemini-3.7-flash"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if len(seen) != 2 || !strings.Contains(seen[0], "Bearer ta") || !strings.Contains(seen[1], `"project":"pb"`) {
		t.Fatalf("rotation=%v", seen)
	}
}

func TestClientDoesNotRotateGeoblocked403(t *testing.T) {
	var hits int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		http.Error(w, `{"error":{"status":"FAILED_PRECONDITION","message":"user location is not supported"}}`, http.StatusForbidden)
	}))
	defer upstream.Close()
	client := newTestClient(t, upstream, []Account{
		{ID: "a", Token: "ta", ProjectID: "pa"},
		{ID: "b", Token: "tb", ProjectID: "pb"},
	}, false)
	_, err := client.Open(context.Background(), providers.DispatchRequest{
		Parsed: protocol.ParsedRequest{UpstreamModelID: "gemini-3.7-flash"},
	})
	if err == nil || !strings.Contains(err.Error(), string(FailureGeoblock)) {
		t.Fatalf("err=%v", err)
	}
	if hits != 1 {
		t.Fatalf("geoblock must not enter the 429 carousel: hits=%d", hits)
	}
}

func TestClientPeerFailoverBeforeContentPreservesLaterBytes(t *testing.T) {
	var hits int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&hits, 1)
		if n == 1 {
			http.Error(w, "missing", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, ccaOK)
	}))
	defer upstream.Close()
	client := newTestClient(t, upstream, []Account{{ID: "acct", Token: "tok", ProjectID: "proj-1"}}, false)
	stream, err := client.Open(context.Background(), providers.DispatchRequest{
		Parsed: protocol.ParsedRequest{UpstreamModelID: "gemini-3.7-flash"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	ev, err := stream.Next()
	if err != nil || ev.Text != "ok" {
		t.Fatalf("event=%#v err=%v", ev, err)
	}
	if atomic.LoadInt32(&hits) != 2 {
		t.Fatalf("hits=%d", hits)
	}
}

func TestClientEmptyPreContentStreamFailoversOnce(t *testing.T) {
	var hits int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		if n == 1 {
			return
		}
		io.WriteString(w, ccaOK)
	}))
	defer upstream.Close()
	client := newTestClient(t, upstream, []Account{{ID: "acct", Token: "tok", ProjectID: "proj-1"}}, false)
	stream, err := client.Open(context.Background(), providers.DispatchRequest{
		Parsed: protocol.ParsedRequest{UpstreamModelID: "gemini-3.7-flash"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	ev, err := stream.Next()
	if err != nil || ev.Text != "ok" {
		t.Fatalf("event=%#v err=%v", ev, err)
	}
	if atomic.LoadInt32(&hits) != 2 {
		t.Fatalf("hits=%d", hits)
	}
}

func TestClientDoesNotPeerFailoverPaidImage(t *testing.T) {
	var hits int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		http.Error(w, "missing", http.StatusNotFound)
	}))
	defer upstream.Close()
	client := newTestClient(t, upstream, []Account{{ID: "acct", Token: "tok", ProjectID: "proj-1"}}, true)
	_, err := client.Open(context.Background(), providers.DispatchRequest{
		Parsed: protocol.ParsedRequest{UpstreamModelID: "gemini-3.7-flash"},
	})
	if !errors.Is(err, ErrAmbiguousPaidImage) {
		t.Fatalf("err=%v", err)
	}
	if atomic.LoadInt32(&hits) != 1 {
		t.Fatalf("image failover hits=%d", hits)
	}
}

func TestClientDiscoversProjectOnSelectedSnapshot(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(raw), `"project":"live-proj"`) {
			t.Fatalf("body=%s", raw)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, ccaOK)
	}))
	defer upstream.Close()
	client, err := NewHardened(context.Background(), Config{
		Endpoint:      DailyAPI,
		Accounts:      []Account{{ID: "acct", Token: "tok"}},
		CatalogModels: []string{"gemini-3.7-flash"},
		HTTPClient:    &http.Client{Transport: rewriteHost{base: upstream.URL, next: http.DefaultTransport}},
		Discover: func(ctx context.Context, token string) (string, error) {
			if token != "tok" {
				t.Fatalf("token=%q", token)
			}
			return "live-proj", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := client.Open(context.Background(), providers.DispatchRequest{
		Parsed: protocol.ParsedRequest{UpstreamModelID: "gemini-3.7-flash"},
	})
	if err != nil {
		t.Fatal(err)
	}
	stream.Close()
}

func TestStreamQuotaErrorMarksAccount(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"error\":{\"status\":\"RESOURCE_EXHAUSTED\",\"message\":\"quota exceeded\"}}\n\n")
	}))
	defer upstream.Close()
	client := newTestClient(t, upstream, []Account{{ID: "acct", Token: "tok", ProjectID: "proj-1"}}, false)
	stream, err := client.Open(context.Background(), providers.DispatchRequest{
		Parsed: protocol.ParsedRequest{UpstreamModelID: "gemini-3.7-flash"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	ev, err := stream.Next()
	if err != nil || ev.Type != protocol.EventError || ev.Message != string(FailureQuota) {
		t.Fatalf("event=%#v err=%v", ev, err)
	}
}
