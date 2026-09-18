package server

import (
	"math"
	"sort"
)

type policyEvalInput struct {
	Record       routingProfileRecord
	Views        []policyCandidateView
	Evidence     policyRequestEvidence
	Signals      policySignals
	StickyMember string
}

type policySignals struct {
	QuotaRemaining map[string]float64
	LatencyMs      map[string]float64
	CostUsd        map[string]float64
	Health         map[string]float64
}

type policyWeights struct {
	Latency float64
	Health  float64
	Cost    float64
	Quota   float64
}

func evaluatePolicyCandidates(record routingProfileRecord, views []policyCandidateView, evidence policyRequestEvidence) ([]routingProfileCandidate, []map[string]any, any) {
	return evaluatePolicySelection(policyEvalInput{Record: record, Views: views, Evidence: evidence})
}

func evaluatePolicySelection(in policyEvalInput) ([]routingProfileCandidate, []map[string]any, any) {
	eligible, rows, _ := filterPolicyCandidates(in.Record, in.Views, in.Evidence)
	eligible, rows = applyPolicyRanking(in.Record, in.Views, rows, eligible, in.Signals)
	return pinPolicySticky(in.Views, eligible, rows, in.StickyMember)
}

func applyPolicyRanking(record routingProfileRecord, views []policyCandidateView, rows []map[string]any, eligible []routingProfileCandidate, signals policySignals) ([]routingProfileCandidate, []map[string]any) {
	weights, useWeights := parsePolicyWeights(record.Optimize)
	quotaMode := evidenceMode(record.UnknownEvidence, "quota", "penalize")
	healthMode := evidenceMode(record.UnknownEvidence, "health", "penalize")
	costMode := evidenceMode(record.UnknownEvidence, "cost", "penalize")
	maxCost, hasMaxCost := jsonFloat(record.Limits["maxEstimatedCostUsd"])
	onUnknownCost := evidenceMode(record.Limits, "onUnknownCost", "allow")

	type scored struct {
		candidate  routingProfileCandidate
		index      int
		total      float64
		cost       map[string]any
		components map[string]float64
	}

	still := make([]scored, 0, len(eligible))
	for _, candidate := range eligible {
		index := policyViewIndex(views, candidate)
		member := policyMemberID(candidate.Provider, candidate.Model)
		exclusions := make([]map[string]any, 0)
		costInfo := map[string]any{}

		quota, quotaKnown := signals.QuotaRemaining[candidate.Provider]
		if floatPtrValue(record.Require.MinQuotaHeadroom) > 0 && quotaKnown && quota < floatPtrValue(record.Require.MinQuotaHeadroom) {
			exclusions = append(exclusions, map[string]any{"code": "quota-headroom"})
		}
		if !quotaKnown && quotaMode == "exclude" {
			exclusions = append(exclusions, map[string]any{"code": "unknown-quota"})
		}

		_, healthKnown := lookupPolicyHealth(signals, candidate.Provider, member)
		if !healthKnown && healthMode == "exclude" {
			exclusions = append(exclusions, map[string]any{"code": "unknown-health"})
		}

		cost, costKnown := signals.CostUsd[member]
		if hasMaxCost {
			if costKnown {
				costInfo["estimatedUsd"] = cost
				costInfo["limitUsd"] = maxCost
				if cost > maxCost {
					exclusions = append(exclusions, map[string]any{"code": "cost-limit"})
					costInfo["capOutcome"] = "exceeded"
				} else {
					costInfo["capOutcome"] = "satisfied"
				}
			} else {
				costInfo["incomplete"] = true
				if onUnknownCost == "exclude" {
					exclusions = append(exclusions, map[string]any{"code": "cost-limit-unknown"})
					costInfo["capOutcome"] = "unknown-excluded"
				} else {
					costInfo["capOutcome"] = "unknown-allowed"
				}
			}
		}
		if !costKnown && costMode == "exclude" {
			exclusions = append(exclusions, map[string]any{"code": "unknown-price"})
		}

		if len(exclusions) > 0 {
			if index >= 0 && index < len(rows) {
				merged := append(rowExclusions(rows[index]), exclusions...)
				rows[index]["exclusions"] = merged
				rows[index]["eligible"] = false
				if len(costInfo) > 0 {
					rows[index]["cost"] = costInfo
				}
			}
			continue
		}

		still = append(still, scored{
			candidate: candidate,
			index:     index,
			cost:      costInfo,
		})
	}

	if useWeights && len(still) > 0 {
		minLatency := math.Inf(1)
		minCost := math.Inf(1)
		for _, row := range still {
			if ms, ok := signals.LatencyMs[policyMemberID(row.candidate.Provider, row.candidate.Model)]; ok && ms > 0 && ms < minLatency {
				minLatency = ms
			}
			if usd, ok := signals.CostUsd[policyMemberID(row.candidate.Provider, row.candidate.Model)]; ok && usd > 0 && usd < minCost {
				minCost = usd
			}
		}
		for i := range still {
			member := policyMemberID(still[i].candidate.Provider, still[i].candidate.Model)
			latencyScore, latencyKnown := relativeInverse(signals.LatencyMs, member, minLatency)
			if !latencyKnown {
				latencyScore = unknownComponentScore("allow")
			}
			healthScore, healthKnown := lookupPolicyHealth(signals, still[i].candidate.Provider, member)
			if !healthKnown {
				healthScore = unknownComponentScore(healthMode)
			}
			costScore, costKnown := relativeInverse(signals.CostUsd, member, minCost)
			if !costKnown {
				costScore = unknownComponentScore(costMode)
			}
			quotaScore, quotaKnown := signals.QuotaRemaining[still[i].candidate.Provider]
			if !quotaKnown {
				quotaScore = unknownComponentScore(quotaMode)
			}
			total := weights.Latency*latencyScore + weights.Health*healthScore + weights.Cost*costScore + weights.Quota*quotaScore
			denom := weights.Latency + weights.Health + weights.Cost + weights.Quota
			if denom > 0 {
				total /= denom
			}
			still[i].total = total
			still[i].components = map[string]float64{
				"latency": latencyScore,
				"health":  healthScore,
				"cost":    costScore,
				"quota":   quotaScore,
			}
		}
		sort.SliceStable(still, func(i, j int) bool {
			if still[i].total == still[j].total {
				return still[i].index < still[j].index
			}
			return still[i].total > still[j].total
		})
	}

	out := make([]routingProfileCandidate, 0, len(still))
	for _, row := range still {
		out = append(out, row.candidate)
		if row.index >= 0 && row.index < len(rows) {
			rows[row.index]["score"] = map[string]any{
				"total":      row.total,
				"components": row.components,
			}
			if len(row.cost) > 0 {
				rows[row.index]["cost"] = row.cost
			}
			rows[row.index]["eligible"] = true
		}
	}
	return out, rows
}

func pinPolicySticky(views []policyCandidateView, eligible []routingProfileCandidate, rows []map[string]any, sticky string) ([]routingProfileCandidate, []map[string]any, any) {
	if sticky != "" {
		for i, candidate := range eligible {
			if policyMemberID(candidate.Provider, candidate.Model) != sticky {
				continue
			}
			if i > 0 {
				pinned := candidate
				copy(eligible[1:i+1], eligible[:i])
				eligible[0] = pinned
			}
			break
		}
	}
	var selected any
	if len(eligible) > 0 {
		if idx := policyViewIndex(views, eligible[0]); idx >= 0 {
			selected = idx
		}
	}
	return eligible, rows, selected
}

func parsePolicyWeights(optimize map[string]any) (policyWeights, bool) {
	if len(optimize) == 0 {
		return policyWeights{Latency: 0.55, Health: 0.25, Cost: 0.1, Quota: 0.1}, true
	}
	weights := policyWeights{
		Latency: jsonFloatOr(optimize["latency"], 0),
		Health:  jsonFloatOr(optimize["health"], 0),
		Cost:    jsonFloatOr(optimize["cost"], 0),
		Quota:   jsonFloatOr(optimize["quota"], 0),
	}
	if weights.Latency == 0 && weights.Health == 0 && weights.Cost == 0 && weights.Quota == 0 {
		return weights, false
	}
	return weights, true
}

func unknownComponentScore(mode string) float64 {
	if mode == "allow" {
		return 0.5
	}
	return 0
}

func relativeInverse(values map[string]float64, key string, min float64) (float64, bool) {
	if values == nil {
		return 0, false
	}
	value, known := values[key]
	if !known {
		return 0, false
	}
	if value <= 0 {
		return 1, true
	}
	if min <= 0 || math.IsInf(min, 1) {
		return 1, true
	}
	score := min / value
	if score > 1 {
		score = 1
	}
	return score, true
}

func lookupPolicyHealth(signals policySignals, provider, member string) (float64, bool) {
	if signals.Health == nil {
		return 0, false
	}
	if value, ok := signals.Health[member]; ok {
		return value, true
	}
	if value, ok := signals.Health[provider]; ok {
		return value, true
	}
	return 0, false
}

func policyMemberID(provider, model string) string {
	return provider + "/" + model
}

func policyViewIndex(views []policyCandidateView, candidate routingProfileCandidate) int {
	for i, view := range views {
		if view.Provider == candidate.Provider && view.Model == candidate.Model {
			return i
		}
	}
	return -1
}

func rowExclusions(row map[string]any) []map[string]any {
	list, _ := row["exclusions"].([]map[string]any)
	return list
}

func jsonFloat(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	default:
		return 0, false
	}
}

func jsonFloatOr(value any, fallback float64) float64 {
	n, ok := jsonFloat(value)
	if !ok {
		return fallback
	}
	return n
}
