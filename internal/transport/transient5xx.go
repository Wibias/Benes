package transport

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	maxTransient5xxAttempts = 4
	transient5xxBase        = 250 * time.Millisecond
	transient5xxMax         = 2 * time.Second
)

type Transient5xxPolicy struct {
	Enabled  bool
	Attempts int
	Sleep    func(context.Context, time.Duration) error
}

func RetryableTransient5xx(status int) bool {
	switch status {
	case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func (p Transient5xxPolicy) BoundAttempts() int {
	if !p.Enabled {
		return 1
	}
	n := p.Attempts
	if n <= 0 {
		n = 2
	}
	if n > maxTransient5xxAttempts {
		n = maxTransient5xxAttempts
	}
	return n
}

func DoTransient5xx(ctx context.Context, client *http.Client, req *http.Request, policy Transient5xxPolicy) (*http.Response, error) {
	if client == nil {
		return nil, fmt.Errorf("HTTP client is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	attempts := policy.BoundAttempts()
	sleep := policy.Sleep
	if sleep == nil {
		sleep = func(ctx context.Context, d time.Duration) error {
			if d <= 0 {
				return nil
			}
			timer := time.NewTimer(d)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				return nil
			}
		}
	}
	for attempt := 1; attempt <= attempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		cloned := req.Clone(ctx)
		if req.GetBody != nil {
			body, err := req.GetBody()
			if err != nil {
				return nil, err
			}
			cloned.Body = body
		} else if req.Body != nil && attempt > 1 {
			return nil, fmt.Errorf("transient 5xx retry requires a rewindable request body")
		}
		resp, err := client.Do(cloned)
		if err != nil {
			return nil, err
		}
		if !RetryableTransient5xx(resp.StatusCode) || attempt == attempts {
			return resp, nil
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		_ = resp.Body.Close()
		if err := sleep(ctx, transientBackoff(attempt-1, resp.Header)); err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("transient 5xx retry exhausted")
}

func transientBackoff(attempt int, headers http.Header) time.Duration {
	if headers != nil {
		if wait := parseRetryAfter(headers.Get("Retry-After")); wait > 0 {
			if wait > transient5xxMax {
				return transient5xxMax
			}
			return wait
		}
	}
	delay := transient5xxBase << attempt
	if delay > transient5xxMax {
		delay = transient5xxMax
	}
	return delay
}

func parseRetryAfter(raw string) time.Duration {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(raw); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(raw); err == nil {
		if wait := time.Until(when); wait > 0 {
			return wait
		}
	}
	return 0
}
