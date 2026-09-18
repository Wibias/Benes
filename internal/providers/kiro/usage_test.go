package kiro

import "testing"

func TestNormalizeKiroUsageCapsEstimateAndPrefersProvider(t *testing.T) {
	est := NormalizeKiroUsage(99, 0, 4, 20)
	if !est.Estimated || est.InputTokens != 20 || est.TotalTokens != 24 || est.CacheReadInputTokens != 0 {
		t.Fatalf("estimate=%#v", est)
	}
	real := NormalizeKiroUsage(3, 40, 5, 10)
	if real.Estimated || real.InputTokens != 10 || real.TotalTokens != 15 {
		t.Fatalf("provider=%#v", real)
	}
	unknown := NormalizeKiroUsage(99, 0, 2, 0)
	if !unknown.Estimated || unknown.InputTokens != 99 || unknown.TotalTokens != 101 {
		t.Fatalf("unknown window must not invent a cap: %#v", unknown)
	}
}
