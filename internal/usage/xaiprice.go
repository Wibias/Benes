package usage

import "strings"

type Cost4 struct {
	Input      float64
	Output     float64
	CacheRead  float64
	CacheWrite float64
}

type Estimate struct {
	Cost4
	PriorityApplied bool
	LowerBound      bool
}

const grok46LongContextTokens = 200_000

// Grok46VerifiedBase is the published grok-4.6 Cost4 (USD / 1M tokens).
// Cached input is $0.20, not the stale bundled $0.50 that copied another model's rate.
var Grok46VerifiedBase = Cost4{Input: 2, Output: 6, CacheRead: 0.2, CacheWrite: 0}

// Grok46PriorityMultiplier is the provider-declared Priority Processing multiplier.
const Grok46PriorityMultiplier = 2

func EstimateXAI(provider, model, confirmedTier, requestedTier string, inputTokens int64, base Cost4, priorityMultiplier float64) Estimate {
	if !strings.EqualFold(strings.TrimSpace(provider), "xai") || strings.TrimSpace(model) != "grok-4.6" {
		return Estimate{Cost4: base}
	}
	base = CorrectGrok46Base(base)
	if priorityMultiplier <= 1 {
		priorityMultiplier = Grok46PriorityMultiplier
	}
	longContext := inputTokens >= grok46LongContextTokens
	confirmedPriority := strings.EqualFold(strings.TrimSpace(confirmedTier), "priority")
	if longContext && (confirmedPriority || strings.EqualFold(strings.TrimSpace(requestedTier), "priority")) {
		return Estimate{Cost4: base, LowerBound: true}
	}
	if !confirmedPriority {
		return Estimate{Cost4: base}
	}
	return Estimate{
		Cost4: Cost4{
			Input:      base.Input * priorityMultiplier,
			Output:     base.Output * priorityMultiplier,
			CacheRead:  base.CacheRead * priorityMultiplier,
			CacheWrite: base.CacheWrite * priorityMultiplier,
		},
		PriorityApplied: true,
	}
}

func CorrectGrok46Base(base Cost4) Cost4 {
	if base == (Cost4{}) {
		return Grok46VerifiedBase
	}
	// Stale bundled grok-4.6 rows used cacheRead 0.50 (or copied input). Prefer the verified 0.20.
	if base.Input == Grok46VerifiedBase.Input && base.Output == Grok46VerifiedBase.Output &&
		base.CacheRead != Grok46VerifiedBase.CacheRead {
		base.CacheRead = Grok46VerifiedBase.CacheRead
		base.CacheWrite = Grok46VerifiedBase.CacheWrite
	}
	return base
}

func ForRoutedModel(routedModel, confirmedTier, requestedTier string, inputTokens int64, base Cost4, priorityMultiplier float64) (Estimate, bool) {
	provider, model, ok := splitRoutedModel(routedModel)
	if !ok || !strings.EqualFold(provider, "xai") || model != "grok-4.6" {
		return Estimate{Cost4: base}, false
	}
	return EstimateXAI(provider, model, confirmedTier, requestedTier, inputTokens, base, priorityMultiplier), true
}

func splitRoutedModel(routedModel string) (string, string, bool) {
	routedModel = strings.TrimSpace(routedModel)
	slash := strings.Index(routedModel, "/")
	if slash <= 0 || slash == len(routedModel)-1 {
		return "", "", false
	}
	return routedModel[:slash], routedModel[slash+1:], true
}
