package transport

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDoTransient5xxRetries503ThenSucceeds(t *testing.T) {
	hits := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		body, _ := io.ReadAll(r.Body)
		if string(body) != "ping" {
			t.Errorf("body=%q", body)
		}
		if hits == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)
	var slept time.Duration
	payload := []byte("ping")
	req, err := http.NewRequest(http.MethodPost, upstream.URL, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(payload)), nil
	}
	resp, err := DoTransient5xx(context.Background(), upstream.Client(), req, Transient5xxPolicy{
		Enabled:  true,
		Attempts: 2,
		Sleep: func(_ context.Context, d time.Duration) error {
			slept = d
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if hits != 2 || resp.StatusCode != http.StatusOK {
		t.Fatalf("hits=%d status=%d", hits, resp.StatusCode)
	}
	if slept != time.Second {
		t.Fatalf("slept=%s", slept)
	}
}

func TestDoTransient5xxDisabledSendsOnce(t *testing.T) {
	hits := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(upstream.Close)
	req, err := http.NewRequest(http.MethodPost, upstream.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := DoTransient5xx(context.Background(), upstream.Client(), req, Transient5xxPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if hits != 1 || resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("hits=%d status=%d", hits, resp.StatusCode)
	}
}
