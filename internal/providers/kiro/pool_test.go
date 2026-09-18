package kiro

import (
	"testing"
	"time"
)

func TestClampRetryAtCapsFarResetAndKeepsNearFuture(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	if got := clampRetryAt(now, time.Time{}); !got.Equal(now.Add(maxKiroRetryCooldown)) {
		t.Fatalf("zero reset=%v", got)
	}
	if got := clampRetryAt(now, now.Add(24*time.Hour)); !got.Equal(now.Add(maxKiroRetryCooldown)) {
		t.Fatalf("far reset=%v", got)
	}
	near := now.Add(10 * time.Minute)
	if got := clampRetryAt(now, near); !got.Equal(near) {
		t.Fatalf("near reset=%v", got)
	}
}
