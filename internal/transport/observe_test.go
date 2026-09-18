package transport

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/timeline"
)

func TestObserveMarksHeadersAndFirstByte(t *testing.T) {
	tr := timeline.New("req-obs", 8)
	client := &http.Client{Transport: Observe(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("hello")),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	}))}
	req, err := http.NewRequestWithContext(timeline.WithTrace(context.Background(), tr), http.MethodGet, "http://provider.test/v1", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil || string(body) != "hello" {
		t.Fatalf("body=%q err=%v", body, err)
	}
	var sawHeaders, sawFirst bool
	for _, ev := range tr.Events() {
		if ev.Milestone == timeline.MilestoneHeaders && ev.OK {
			sawHeaders = true
		}
		if ev.Milestone == timeline.MilestoneFirstByte && ev.OK {
			sawFirst = true
		}
	}
	if !sawHeaders || !sawFirst {
		t.Fatalf("events=%#v", tr.Events())
	}
}

func TestObserveMarksHeaderFailureWithoutFirstByte(t *testing.T) {
	tr := timeline.New("req-fail", 8)
	client := &http.Client{Transport: Observe(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("dial timeout")
	}))}
	req, err := http.NewRequestWithContext(timeline.WithTrace(context.Background(), tr), http.MethodGet, "http://127.0.0.1:1/", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err == nil || resp != nil {
		t.Fatalf("resp=%v err=%v", resp, err)
	}
	var sawFail, sawFirst bool
	for _, ev := range tr.Events() {
		if ev.Milestone == timeline.MilestoneHeaders && !ev.OK && ev.Cause == "headers" {
			sawFail = true
		}
		if ev.Milestone == timeline.MilestoneFirstByte {
			sawFirst = true
		}
	}
	if !sawFail || sawFirst {
		t.Fatalf("events=%#v", tr.Events())
	}
}

func TestObserveMarksUpstreamEndOnBodyEOF(t *testing.T) {
	tr := timeline.New("req-end", 8)
	client := &http.Client{Transport: Observe(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("hello")),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	}))}
	req, err := http.NewRequestWithContext(timeline.WithTrace(context.Background(), tr), http.MethodGet, "http://provider.test/v1", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	var saw bool
	for _, ev := range tr.Events() {
		if ev.Milestone == timeline.MilestoneUpstreamEnd && ev.OK {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("events=%#v", tr.Events())
	}
}

func TestObserveDoesNotMarkWithoutTrace(t *testing.T) {
	client := &http.Client{Transport: Observe(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("ok")),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	}))}
	resp, err := client.Get("http://provider.test/v1")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
