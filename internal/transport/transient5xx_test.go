package transport

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/resourcebudget"
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

func TestDoTransient5xxForTurnStopsAtPhysicalSendBudget(t *testing.T) {
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
	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxPhysicalSends: 2})
	turn, err := budget.AcquireTurn(context.Background(), "transient-budget")
	if err != nil {
		t.Fatal(err)
	}
	defer turn.Close()

	_, err = DoTransient5xxForTurn(context.Background(), upstream.Client(), req, Transient5xxPolicy{
		Enabled:  true,
		Attempts: 4,
		Sleep:    func(context.Context, time.Duration) error { return nil },
	}, turn, "test-provider")
	if !errors.Is(err, resourcebudget.ErrPhysicalSendBudgetExceeded) {
		t.Fatalf("err=%v", err)
	}
	if hits != 2 {
		t.Fatalf("physical hits=%d want=2", hits)
	}
	records := turn.PhysicalSends()
	if len(records) != 2 || records[0].Reason != "test-provider" || records[1].Reason != "test-provider" {
		t.Fatalf("records=%#v", records)
	}
}

func TestDoPhysicalSendCancellationBeforeNetworkRefundsReservation(t *testing.T) {
	hits := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxPhysicalSends: 1})
	turn, err := budget.AcquireTurn(context.Background(), "cancel-before-send")
	if err != nil {
		t.Fatal(err)
	}
	defer turn.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, upstream.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DoPhysicalSend(ctx, upstream.Client(), req, turn, "test_provider"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled err=%v", err)
	}
	if hits != 0 || len(turn.PhysicalSends()) != 0 {
		t.Fatalf("cancelled request consumed send: hits=%d records=%#v", hits, turn.PhysicalSends())
	}

	req2, err := http.NewRequest(http.MethodGet, upstream.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := DoPhysicalSend(context.Background(), upstream.Client(), req2, turn, "test_provider")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if hits != 1 || len(turn.PhysicalSends()) != 1 {
		t.Fatalf("refunded slot not reusable: hits=%d records=%#v", hits, turn.PhysicalSends())
	}
}

func TestDoPhysicalSendNetworkErrorDoesNotRefundCommittedSend(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, context.Canceled
	})}
	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxPhysicalSends: 1})
	turn, err := budget.AcquireTurn(context.Background(), "cancel-after-send")
	if err != nil {
		t.Fatal(err)
	}
	defer turn.Close()

	req, _ := http.NewRequest(http.MethodGet, "https://example.invalid", nil)
	if _, err := DoPhysicalSend(context.Background(), client, req, turn, "test_provider"); !errors.Is(err, context.Canceled) {
		t.Fatalf("network err=%v", err)
	}
	if calls != 1 || len(turn.PhysicalSends()) != 1 {
		t.Fatalf("committed send missing: calls=%d records=%#v", calls, turn.PhysicalSends())
	}
	if _, err := DoPhysicalSend(context.Background(), client, req, turn, "test_provider"); !errors.Is(err, resourcebudget.ErrPhysicalSendBudgetExceeded) {
		t.Fatalf("second send err=%v", err)
	}
	if calls != 1 {
		t.Fatalf("budget exhaustion reached network: calls=%d", calls)
	}
}

