package transport

import (
	"fmt"
	"net/http"
)

type RequestBodyTooLargeError struct {
	Limit         int64
	ContentLength int64
}

func (e *RequestBodyTooLargeError) Error() string {
	if e == nil {
		return "request body exceeds configured transport limit"
	}
	return fmt.Sprintf("request body length %d exceeds configured transport limit %d", e.ContentLength, e.Limit)
}

type requestBodyLimitRoundTripper struct {
	maxBytes int64
	path     string
	next     http.RoundTripper
}

func (r requestBodyLimitRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	if request == nil {
		return r.next.RoundTrip(request)
	}
	if err := request.Context().Err(); err != nil {
		if request.Body != nil {
			_ = request.Body.Close()
		}
		return nil, err
	}
	if r.maxBytes > 0 && requestBodyLimitMatches(request, r.path) && request.ContentLength > r.maxBytes {
		if request.Body != nil {
			_ = request.Body.Close()
		}
		return nil, &RequestBodyTooLargeError{Limit: r.maxBytes, ContentLength: request.ContentLength}
	}
	return r.next.RoundTrip(request)
}

func requestBodyLimitMatches(request *http.Request, path string) bool {
	if path == "" {
		return true
	}
	return request != nil && request.URL != nil && request.URL.Path == path
}

func withRequestBodyLimit(next http.RoundTripper, maxBytes int64, path string) http.RoundTripper {
	if next == nil || maxBytes <= 0 {
		return next
	}
	return requestBodyLimitRoundTripper{maxBytes: maxBytes, path: path, next: next}
}
