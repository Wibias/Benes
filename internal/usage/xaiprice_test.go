package usage

import "testing"

func TestEstimateXAIAppliesPriorityOnlyWhenConfirmedOnCanonicalProvider(t *testing.T) {
	base := Cost4{Input: 2, Output: 6, CacheRead: 0.2, CacheWrite: 0}
	got := EstimateXAI("xai", "grok-4.6", "priority", "priority", 1000, base, 2)
	if !got.PriorityApplied || got.Input != 4 || got.Output != 12 || got.LowerBound {
		t.Fatalf("confirmed=%#v", got)
	}
	unconfirmed := EstimateXAI("xai", "grok-4.6", "", "priority", 1000, base, 2)
	if unconfirmed.PriorityApplied || unconfirmed.Input != 2 {
		t.Fatalf("unconfirmed billed as Priority: %#v", unconfirmed)
	}
	reseller := EstimateXAI("openrouter", "grok-4.6", "priority", "priority", 1000, base, 2)
	if reseller.PriorityApplied || reseller.Input != 2 {
		t.Fatalf("reseller inherited xAI price: %#v", reseller)
	}
}

func TestEstimateXAILongContextPlusPriorityIsLowerBoundNotCombined(t *testing.T) {
	base := Cost4{Input: 2, Output: 6, CacheRead: 0.2, CacheWrite: 0}
	got := EstimateXAI("xai", "grok-4.6", "priority", "priority", 200_000, base, 2)
	if !got.LowerBound || got.PriorityApplied || got.Input != 2 {
		t.Fatalf("combined invented rate: %#v", got)
	}
}

func TestForRoutedModelAppliesOnlyCanonicalXAIGrok46(t *testing.T) {
	base := Cost4{Input: 2, Output: 6, CacheRead: 0.2, CacheWrite: 0}
	got, ok := ForRoutedModel("xai/grok-4.6", "priority", "priority", 1000, base, 2)
	if !ok || !got.PriorityApplied || got.Input != 4 {
		t.Fatalf("canonical=%v %#v", ok, got)
	}
	reseller, resellerOK := ForRoutedModel("openrouter/grok-4.6", "priority", "priority", 1000, base, 2)
	if resellerOK || reseller.PriorityApplied || reseller.Input != 2 {
		t.Fatalf("reseller=%v %#v", resellerOK, reseller)
	}
}

func TestCorrectGrok46BaseReplacesStaleCachedInput(t *testing.T) {
	stale := Cost4{Input: 2, Output: 6, CacheRead: 0.5, CacheWrite: 0}
	got := CorrectGrok46Base(stale)
	if got != Grok46VerifiedBase {
		t.Fatalf("stale=%#v", got)
	}
	if CorrectGrok46Base(Cost4{}) != Grok46VerifiedBase {
		t.Fatal("zero base was not replaced")
	}
	unconfirmed := EstimateXAI("xai", "grok-4.6", "", "", 1000, stale, 1)
	if unconfirmed.CacheRead != 0.2 || unconfirmed.PriorityApplied {
		t.Fatalf("uncorrected stale billed: %#v", unconfirmed)
	}
	confirmed := EstimateXAI("xai", "grok-4.6", "priority", "priority", 1000, stale, 1)
	if !confirmed.PriorityApplied || confirmed.CacheRead != 0.4 || confirmed.Input != 4 {
		t.Fatalf("confirmed did not use corrected base: %#v", confirmed)
	}
}
