package google

import "testing"

func TestNormalizeUsagePrefersProviderAndDoesNotInventCacheRead(t *testing.T) {
	est := NormalizeUsage(99, 0, 4, 0, 20)
	if !est.Estimated || est.InputTokens != 20 || est.TotalTokens != 24 || est.CacheReadInputTokens != 0 {
		t.Fatalf("est=%#v", est)
	}
	real := NormalizeUsage(3, 40, 5, 7, 10)
	if real.Estimated || real.InputTokens != 10 || real.TotalTokens != 15 || real.CachedInputTokens != 7 || real.CacheReadInputTokens != 0 {
		t.Fatalf("real=%#v", real)
	}
}
