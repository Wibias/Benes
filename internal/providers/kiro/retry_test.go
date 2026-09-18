package kiro

import (
	"net/http"
	"testing"
)

func TestRetryableStatusIsBoundedAndSkipsAuth(t *testing.T) {
	if !retryableStatus(http.StatusTooManyRequests) || !retryableStatus(http.StatusServiceUnavailable) {
		t.Fatal("throttle")
	}
	if retryableStatus(http.StatusUnauthorized) || retryableStatus(http.StatusBadRequest) {
		t.Fatal("auth/validation must not retry")
	}
	if !terminalAuthStatus(http.StatusForbidden) {
		t.Fatal("forbidden")
	}
}
