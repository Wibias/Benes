package providerregistry

import (
	"testing"
	"time"
)

func TestPacingIntervalForModelUsesOverrideThenProviderFallback(t *testing.T) {
	models := map[string]time.Duration{"gpt-5": 2 * time.Second}
	if got := pacingIntervalForModel("openai-apikey", "gpt-5", time.Second, models); got != 2*time.Second {
		t.Fatalf("override=%s", got)
	}
	if got := pacingIntervalForModel("openai-apikey", "openai-apikey/gpt-5", time.Second, models); got != 2*time.Second {
		t.Fatalf("qualified override=%s", got)
	}
	if got := pacingIntervalForModel("openai-apikey", "gpt-4o", time.Second, models); got != time.Second {
		t.Fatalf("fallback=%s", got)
	}
}
