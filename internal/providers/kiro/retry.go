package kiro

import "net/http"

const maxTransientAttempts = 3

func retryableStatus(status int) bool {
	switch status {
	case http.StatusTooManyRequests, http.StatusServiceUnavailable, http.StatusBadGateway, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func terminalAuthStatus(status int) bool {
	return status == http.StatusUnauthorized || status == http.StatusForbidden
}
