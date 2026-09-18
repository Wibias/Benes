package cursor

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type seqRoundTrip struct {
	calls int
	steps []func(*http.Request) (*http.Response, error)
}

func (s *seqRoundTrip) RoundTrip(req *http.Request) (*http.Response, error) {
	if s.calls >= len(s.steps) {
		return nil, errors.New("unexpected extra discovery request")
	}
	step := s.steps[s.calls]
	s.calls++
	return step(req)
}

func discoveryClient(t *testing.T, transport http.RoundTripper) *Client {
	t.Helper()
	client, err := NewHardened(context.Background(), Config{
		Endpoint:   DefaultAPI,
		APIKey:     "tok",
		HTTPClient: &http.Client{Transport: transport},
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestDiscoverUsableModelsRetriesOnceOnPreHeaderEOF(t *testing.T) {
	ok := EncodeProtoMessage(1, EncodeProtoString(1, "gpt-5.4"))
	trip := &seqRoundTrip{steps: []func(*http.Request) (*http.Response, error){
		func(*http.Request) (*http.Response, error) { return nil, io.EOF },
		func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(ok))), Header: make(http.Header)}, nil
		},
	}}
	models, err := discoveryClient(t, trip).DiscoverUsableModels(context.Background())
	if err != nil || len(models) != 1 || models[0] != "gpt-5.4" {
		t.Fatalf("models=%v err=%v", models, err)
	}
	if trip.calls != 2 {
		t.Fatalf("calls=%d", trip.calls)
	}
}

func TestDiscoverUsableModelsDoesNotRetrySecondTransientFailure(t *testing.T) {
	trip := &seqRoundTrip{steps: []func(*http.Request) (*http.Response, error){
		func(*http.Request) (*http.Response, error) { return nil, io.EOF },
		func(*http.Request) (*http.Response, error) { return nil, io.ErrUnexpectedEOF },
	}}
	_, err := discoveryClient(t, trip).DiscoverUsableModels(context.Background())
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("err=%v", err)
	}
	if trip.calls != 2 {
		t.Fatalf("calls=%d", trip.calls)
	}
}

func TestDiscoverUsableModelsDoesNotRetryHTTPStatus(t *testing.T) {
	trip := &seqRoundTrip{steps: []func(*http.Request) (*http.Response, error){
		func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusUnauthorized, Body: io.NopCloser(strings.NewReader("no")), Header: make(http.Header)}, nil
		},
	}}
	_, err := discoveryClient(t, trip).DiscoverUsableModels(context.Background())
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("err=%v", err)
	}
	if trip.calls != 1 {
		t.Fatalf("calls=%d", trip.calls)
	}
}

func TestDiscoverUsableModelsDoesNotRetryMalformedBody(t *testing.T) {
	trip := &seqRoundTrip{steps: []func(*http.Request) (*http.Response, error){
		func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("not-proto")), Header: make(http.Header)}, nil
		},
	}}
	models, err := discoveryClient(t, trip).DiscoverUsableModels(context.Background())
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if len(models) != 0 {
		t.Fatalf("models=%v", models)
	}
	if trip.calls != 1 {
		t.Fatalf("calls=%d", trip.calls)
	}
}

func TestDiscoverUsableModelsDoesNotRetryCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	trip := &seqRoundTrip{steps: []func(*http.Request) (*http.Response, error){
		func(*http.Request) (*http.Response, error) { return nil, io.EOF },
		func(*http.Request) (*http.Response, error) { return nil, io.EOF },
	}}
	_, err := discoveryClient(t, trip).DiscoverUsableModels(ctx)
	if err == nil {
		t.Fatal("expected discovery error")
	}
	if trip.calls > 1 {
		t.Fatalf("canceled context retried: calls=%d", trip.calls)
	}
}
