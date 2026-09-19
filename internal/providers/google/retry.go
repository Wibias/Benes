package google

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wibias/Benes/internal/resourcebudget"
	"github.com/Wibias/Benes/internal/timeline"
	"github.com/Wibias/Benes/internal/transport"
)

const (
	maxRetryAttempts = 3
	retryBase        = 250 * time.Millisecond
	retryMax         = 2 * time.Second
	maxErrorBody     = 64 << 10
)

type RetryPolicy struct {
	Attempts int
	Sleep    func(context.Context, time.Duration) error
}

func AppliesTransientRetry(protocol string) bool {
	switch protocol {
	case "google", "google-vertex":
		return true
	default:
		return false
	}
}

func RetryableStatus(status int) bool {
	switch status {
	case http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func QuotaExhausted(body []byte) bool {
	var payload struct {
		Error struct {
			Status  string `json:"status"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return false
	}
	if payload.Error.Status != "RESOURCE_EXHAUSTED" {
		return false
	}
	lower := strings.ToLower(payload.Error.Message)
	return strings.Contains(lower, "quotafailure") ||
		strings.Contains(lower, "quota exceeded") ||
		strings.Contains(lower, "exceeded your current quota") ||
		strings.Contains(lower, "billing")
}

func Do(ctx context.Context, client *http.Client, req *http.Request, policy RetryPolicy) (*http.Response, error) {
	return do(ctx, client, req, policy, nil, "")
}

func DoForTurn(ctx context.Context, client *http.Client, req *http.Request, policy RetryPolicy, turn *resourcebudget.Turn, reason string) (*http.Response, error) {
	return do(ctx, client, req, policy, turn, reason)
}

func do(ctx context.Context, client *http.Client, req *http.Request, policy RetryPolicy, turn *resourcebudget.Turn, reason string) (*http.Response, error) {
	if client == nil {
		client = transport.DefaultUnpinnedClient()
	}
	if ctx == nil {
		ctx = context.Background()
	}
	attempts := policy.Attempts
	if attempts <= 0 {
		attempts = maxRetryAttempts
	}
	sleep := policy.Sleep
	if sleep == nil {
		sleep = func(ctx context.Context, d time.Duration) error {
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
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		if tr := timeline.FromContext(ctx); tr != nil {
			tr.SetAttempt(attempt)
		}
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
			return nil, fmt.Errorf("Google retry requires a rewindable request body")
		}
		resp, err := transport.DoPhysicalSend(ctx, client, cloned, turn, reason)
		if err != nil {
			if errors.Is(err, resourcebudget.ErrPhysicalSendBudgetExceeded) {
				return nil, err
			}
			lastErr = err
			if attempt == attempts {
				return nil, err
			}
			if err := sleep(ctx, backoff(attempt-1, nil)); err != nil {
				return nil, err
			}
			continue
		}
		if !RetryableStatus(resp.StatusCode) || attempt == attempts {
			return resp, nil
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests && QuotaExhausted(raw) {
			resp.Body = io.NopCloser(bytes.NewReader(raw))
			return resp, nil
		}
		if err := sleep(ctx, backoff(attempt-1, resp.Header)); err != nil {
			return nil, err
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("Google request failed")
}

func backoff(attempt int, headers http.Header) time.Duration {
	if headers != nil {
		if raw := strings.TrimSpace(headers.Get("Retry-After")); raw != "" {
			if seconds, err := strconv.Atoi(raw); err == nil && seconds >= 0 {
				d := time.Duration(seconds) * time.Second
				if d > retryMax {
					return retryMax
				}
				return d
			}
		}
	}
	delay := retryBase << attempt
	if delay > retryMax {
		delay = retryMax
	}
	jitter := time.Duration(rand.Int63n(int64(delay/2) + 1))
	return delay/2 + jitter
}
