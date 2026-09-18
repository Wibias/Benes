package transport

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type requestBodyLimitRoundTripFunc func(*http.Request) (*http.Response, error)

func (f requestBodyLimitRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type trackedRequestBody struct {
	io.Reader
	closed bool
}

func (b *trackedRequestBody) Close() error {
	b.closed = true
	return nil
}

func TestRequestBodyLimitRejectsKnownExactLengthBeforeNextTransport(t *testing.T) {
	called := false
	next := requestBodyLimitRoundTripFunc(func(*http.Request) (*http.Response, error) {
		called = true
		return nil, errors.New("must not be called")
	})
	body := &trackedRequestBody{Reader: strings.NewReader("12345")}
	req, err := http.NewRequest(http.MethodPost, "https://example.com/v1/responses", body)
	if err != nil {
		t.Fatal(err)
	}
	req.ContentLength = 5

	_, err = withRequestBodyLimit(next, 4, "/v1/responses").RoundTrip(req)
	var tooLarge *RequestBodyTooLargeError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("err=%v", err)
	}
	if tooLarge.Limit != 4 || tooLarge.ContentLength != 5 {
		t.Fatalf("tooLarge=%#v", tooLarge)
	}
	if called {
		t.Fatal("next transport was called")
	}
	if !body.closed {
		t.Fatal("rejected request body was not closed")
	}
}

func TestRequestBodyLimitAllowsExactLimitAndUnknownLength(t *testing.T) {
	for _, contentLength := range []int64{4, -1} {
		t.Run(http.StatusText(int(contentLength+200)), func(t *testing.T) {
			called := false
			next := requestBodyLimitRoundTripFunc(func(*http.Request) (*http.Response, error) {
				called = true
				return &http.Response{StatusCode: http.StatusNoContent, Body: http.NoBody}, nil
			})
			req, err := http.NewRequest(http.MethodPost, "https://example.com/v1/responses", strings.NewReader("1234"))
			if err != nil {
				t.Fatal(err)
			}
			req.ContentLength = contentLength
			response, err := withRequestBodyLimit(next, 4, "/v1/responses").RoundTrip(req)
			if err != nil {
				t.Fatal(err)
			}
			if !called || response == nil || response.StatusCode != http.StatusNoContent {
				t.Fatalf("called=%v response=%#v", called, response)
			}
		})
	}
}

func TestRequestBodyLimitIgnoresOversizedBodyOnDifferentPath(t *testing.T) {
	called := false
	next := requestBodyLimitRoundTripFunc(func(*http.Request) (*http.Response, error) {
		called = true
		return &http.Response{StatusCode: http.StatusNoContent, Body: http.NoBody}, nil
	})
	req, err := http.NewRequest(http.MethodPost, "https://example.com/v1/images/generations", strings.NewReader("123456789"))
	if err != nil {
		t.Fatal(err)
	}
	req.ContentLength = 9
	response, err := withRequestBodyLimit(next, 4, "/v1/responses").RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	if !called || response == nil || response.StatusCode != http.StatusNoContent {
		t.Fatalf("called=%v response=%#v", called, response)
	}
}

func TestRequestBodyLimitPreservesCallerCancellationPrecedence(t *testing.T) {
	called := false
	next := requestBodyLimitRoundTripFunc(func(*http.Request) (*http.Response, error) {
		called = true
		return nil, errors.New("must not be called")
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	body := &trackedRequestBody{Reader: strings.NewReader("12345")}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://example.com/v1/responses", body)
	if err != nil {
		t.Fatal(err)
	}
	req.ContentLength = 5

	_, err = withRequestBodyLimit(next, 4, "/v1/responses").RoundTrip(req)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	if called {
		t.Fatal("next transport was called")
	}
	if !body.closed {
		t.Fatal("cancelled request body was not closed")
	}
}
