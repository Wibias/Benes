package google

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/timeline"
)

func TestAppliesTransientRetryIsGoogleScoped(t *testing.T) {
	if !AppliesTransientRetry("google") || !AppliesTransientRetry("google-vertex") {
		t.Fatal("google")
	}
	if AppliesTransientRetry("openai-chat") || AppliesTransientRetry("cursor") || AppliesTransientRetry("kiro") {
		t.Fatal("non-google combo targets must remain reset-only")
	}
}

func TestAIStudioRetriesTransientThenSucceeds(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`ok`))
	}))
	t.Cleanup(srv.Close)
	req, err := http.NewRequest(http.MethodPost, srv.URL, strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("{}")), nil }
	resp, err := Do(context.Background(), srv.Client(), req, RetryPolicy{Sleep: func(context.Context, time.Duration) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if calls != 3 || resp.StatusCode != 200 {
		t.Fatalf("calls=%d status=%d", calls, resp.StatusCode)
	}
}

func TestDoStampsIncrementingAttemptsOnTheRequestTrace(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`ok`))
	}))
	t.Cleanup(srv.Close)
	tr := timeline.New("req-retry", 8)
	ctx := timeline.WithTrace(context.Background(), tr)
	req, err := http.NewRequest(http.MethodPost, srv.URL, strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("{}")), nil }
	resp, err := Do(ctx, srv.Client(), req, RetryPolicy{Sleep: func(context.Context, time.Duration) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if tr.Attempt() != 3 {
		t.Fatalf("attempt=%d events=%#v", tr.Attempt(), tr.Events())
	}
}

func TestAIStudioQuota429AndInvalid400AreSingleShot(t *testing.T) {
	quotaCalls := 0
	quota := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		quotaCalls++
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"status":"RESOURCE_EXHAUSTED","message":"quota exceeded"}}`))
	}))
	t.Cleanup(quota.Close)
	req, _ := http.NewRequest(http.MethodPost, quota.URL, strings.NewReader("{}"))
	req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("{}")), nil }
	resp, err := Do(context.Background(), quota.Client(), req, RetryPolicy{Sleep: func(context.Context, time.Duration) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if quotaCalls != 1 || resp.StatusCode != 429 {
		t.Fatalf("quota calls=%d status=%d", quotaCalls, resp.StatusCode)
	}

	badCalls := 0
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		badCalls++
		w.WriteHeader(http.StatusBadRequest)
	}))
	t.Cleanup(bad.Close)
	req, _ = http.NewRequest(http.MethodPost, bad.URL, strings.NewReader("{}"))
	req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("{}")), nil }
	resp, err = Do(context.Background(), bad.Client(), req, RetryPolicy{Sleep: func(context.Context, time.Duration) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if badCalls != 1 || resp.StatusCode != 400 {
		t.Fatalf("400 calls=%d status=%d", badCalls, resp.StatusCode)
	}
}

func TestAIStudioRetryHonorsCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1:1/", strings.NewReader("{}"))
	if _, err := Do(ctx, http.DefaultClient, req, RetryPolicy{}); !strings.Contains(err.Error(), "canceled") && err != context.Canceled {
		t.Fatalf("err=%v", err)
	}
}
