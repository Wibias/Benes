package server

import (
	"testing"

	"github.com/Wibias/Benes/internal/catalog"
)

func TestEvaluatePolicySelectionRanksByQuota(t *testing.T) {
	record := routingProfileRecord{
		Candidates: []routingProfileCandidate{
			{Provider: "slow", Model: "a"},
			{Provider: "fresh", Model: "b"},
		},
		Optimize: map[string]any{"latency": 0.0, "health": 0.0, "cost": 0.0, "quota": 1.0},
	}
	views := []policyCandidateView{
		{Provider: "slow", Model: "a", Adapter: "openai-chat"},
		{Provider: "fresh", Model: "b", Adapter: "openai-chat"},
	}
	eligible, rows, selected := evaluatePolicySelection(policyEvalInput{
		Record:  record,
		Views:   views,
		Signals: policySignals{QuotaRemaining: map[string]float64{"slow": 0.1, "fresh": 0.9}},
	})
	if len(eligible) != 2 || eligible[0].Provider != "fresh" || eligible[1].Provider != "slow" {
		t.Fatalf("eligible=%v", eligible)
	}
	if selected != 1 {
		t.Fatalf("selected=%v rows=%v", selected, rows)
	}
	freshScore, _ := rows[1]["score"].(map[string]any)
	slowScore, _ := rows[0]["score"].(map[string]any)
	if freshScore == nil || slowScore == nil {
		t.Fatalf("missing scores rows=%v", rows)
	}
	if asFloat(freshScore["total"]) <= asFloat(slowScore["total"]) {
		t.Fatalf("fresh total=%v slow total=%v", freshScore["total"], slowScore["total"])
	}
}

func TestEvaluatePolicySelectionKeepsOrderWhenOptimizeOmittedAndSignalsEqual(t *testing.T) {
	record := routingProfileRecord{
		Candidates: []routingProfileCandidate{
			{Provider: "openai-apikey", Model: "gpt-5.5"},
			{Provider: "xai", Model: "grok-4"},
		},
	}
	views := []policyCandidateView{
		{Provider: "openai-apikey", Model: "gpt-5.5", Adapter: "openai-chat"},
		{Provider: "xai", Model: "grok-4", Adapter: "xai"},
	}
	eligible, _, selected := evaluatePolicySelection(policyEvalInput{Record: record, Views: views})
	if len(eligible) != 2 || eligible[0].Provider != "openai-apikey" || selected != 0 {
		t.Fatalf("eligible=%v selected=%v", eligible, selected)
	}
}

func TestEvaluatePolicySelectionKeepsExplicitZeroWeightsInListOrder(t *testing.T) {
	record := routingProfileRecord{
		Candidates: []routingProfileCandidate{
			{Provider: "slow", Model: "a"},
			{Provider: "fresh", Model: "b"},
		},
		Optimize: map[string]any{"latency": 0.0, "health": 0.0, "cost": 0.0, "quota": 0.0},
	}
	views := []policyCandidateView{
		{Provider: "slow", Model: "a", Adapter: "openai-chat"},
		{Provider: "fresh", Model: "b", Adapter: "openai-chat"},
	}
	eligible, _, selected := evaluatePolicySelection(policyEvalInput{
		Record:  record,
		Views:   views,
		Signals: policySignals{QuotaRemaining: map[string]float64{"slow": 0.1, "fresh": 0.9}},
	})
	if len(eligible) != 2 || eligible[0].Provider != "slow" || selected != 0 {
		t.Fatalf("eligible=%v selected=%v", eligible, selected)
	}
}

func TestEvaluatePolicySelectionExcludesUnknownQuotaWhenConfigured(t *testing.T) {
	record := routingProfileRecord{
		Candidates: []routingProfileCandidate{
			{Provider: "metered", Model: "a"},
			{Provider: "unknown", Model: "b"},
		},
		Optimize:        map[string]any{"quota": 1.0},
		UnknownEvidence: map[string]any{"quota": "exclude"},
	}
	views := []policyCandidateView{
		{Provider: "metered", Model: "a", Adapter: "openai-chat"},
		{Provider: "unknown", Model: "b", Adapter: "openai-chat"},
	}
	eligible, rows, selected := evaluatePolicySelection(policyEvalInput{
		Record:  record,
		Views:   views,
		Signals: policySignals{QuotaRemaining: map[string]float64{"metered": 0.8}},
	})
	if len(eligible) != 1 || eligible[0].Provider != "metered" || selected != 0 {
		t.Fatalf("eligible=%v selected=%v rows=%v", eligible, selected, rows)
	}
	exclusions, _ := rows[1]["exclusions"].([]map[string]any)
	if len(exclusions) == 0 || exclusions[0]["code"] != "unknown-quota" {
		t.Fatalf("exclusions=%v", rows[1]["exclusions"])
	}
}

func TestEvaluatePolicySelectionExcludesBelowQuotaHeadroom(t *testing.T) {
	record := routingProfileRecord{
		Candidates: []routingProfileCandidate{
			{Provider: "empty", Model: "a"},
			{Provider: "fresh", Model: "b"},
		},
		Require:  routingProfileRequire{MinQuotaHeadroom: floatPtr(0.2)},
		Optimize: map[string]any{"quota": 1.0},
	}
	views := []policyCandidateView{
		{Provider: "empty", Model: "a", Adapter: "openai-chat"},
		{Provider: "fresh", Model: "b", Adapter: "openai-chat"},
	}
	eligible, rows, selected := evaluatePolicySelection(policyEvalInput{
		Record:  record,
		Views:   views,
		Signals: policySignals{QuotaRemaining: map[string]float64{"empty": 0.1, "fresh": 0.9}},
	})
	if len(eligible) != 1 || eligible[0].Provider != "fresh" || selected != 1 {
		t.Fatalf("eligible=%v selected=%v rows=%v", eligible, selected, rows)
	}
	exclusions, _ := rows[0]["exclusions"].([]map[string]any)
	if len(exclusions) == 0 || exclusions[0]["code"] != "quota-headroom" {
		t.Fatalf("exclusions=%v", rows[0]["exclusions"])
	}
}

func TestEvaluatePolicySelectionDoesNotTreatMissingQuotaAsZero(t *testing.T) {
	record := routingProfileRecord{
		Candidates: []routingProfileCandidate{
			{Provider: "unknown", Model: "a"},
			{Provider: "metered", Model: "b"},
		},
		Optimize:        map[string]any{"quota": 1.0},
		UnknownEvidence: map[string]any{"quota": "penalize"},
	}
	views := []policyCandidateView{
		{Provider: "unknown", Model: "a", Adapter: "openai-chat"},
		{Provider: "metered", Model: "b", Adapter: "openai-chat"},
	}
	eligible, _, selected := evaluatePolicySelection(policyEvalInput{
		Record:  record,
		Views:   views,
		Signals: policySignals{QuotaRemaining: map[string]float64{"metered": 0.4}},
	})
	if len(eligible) != 2 || eligible[0].Provider != "metered" || selected != 1 {
		t.Fatalf("eligible=%v selected=%v", eligible, selected)
	}
}

func TestEvaluatePolicySelectionPinsStickyMemberFirst(t *testing.T) {
	record := routingProfileRecord{
		Candidates: []routingProfileCandidate{
			{Provider: "slow", Model: "a"},
			{Provider: "fresh", Model: "b"},
		},
		Optimize: map[string]any{"quota": 1.0},
	}
	views := []policyCandidateView{
		{Provider: "slow", Model: "a", Adapter: "openai-chat"},
		{Provider: "fresh", Model: "b", Adapter: "openai-chat"},
	}
	eligible, _, selected := evaluatePolicySelection(policyEvalInput{
		Record:       record,
		Views:        views,
		Signals:      policySignals{QuotaRemaining: map[string]float64{"slow": 0.1, "fresh": 0.9}},
		StickyMember: "slow/a",
	})
	if len(eligible) != 2 || eligible[0].Provider != "slow" || eligible[1].Provider != "fresh" || selected != 0 {
		t.Fatalf("eligible=%v selected=%v", eligible, selected)
	}
}

func TestEvaluatePolicySelectionIgnoresStickyWhenFilteredOut(t *testing.T) {
	image := true
	record := routingProfileRecord{
		Require: routingProfileRequire{ImageInput: &image},
		Candidates: []routingProfileCandidate{
			{Provider: "google", Model: "gemini-flash"},
			{Provider: "openai-apikey", Model: "gpt-5.4"},
		},
		Optimize: map[string]any{"quota": 1.0},
	}
	views := []policyCandidateView{
		{Provider: "google", Model: "gemini-flash", Vision: catalog.CapabilityFalse, Adapter: "google"},
		{Provider: "openai-apikey", Model: "gpt-5.4", Vision: catalog.CapabilityTrue, Adapter: "openai-responses"},
	}
	eligible, _, selected := evaluatePolicySelection(policyEvalInput{
		Record:       record,
		Views:        views,
		StickyMember: "google/gemini-flash",
	})
	if len(eligible) != 1 || eligible[0].Provider != "openai-apikey" || selected != 1 {
		t.Fatalf("eligible=%v selected=%v", eligible, selected)
	}
}

func TestEvaluatePolicySelectionRanksByLatencyWhenKnown(t *testing.T) {
	record := routingProfileRecord{
		Candidates: []routingProfileCandidate{
			{Provider: "slow", Model: "a"},
			{Provider: "fast", Model: "b"},
		},
		Optimize: map[string]any{"latency": 1.0, "health": 0.0, "cost": 0.0, "quota": 0.0},
	}
	views := []policyCandidateView{
		{Provider: "slow", Model: "a", Adapter: "openai-chat"},
		{Provider: "fast", Model: "b", Adapter: "openai-chat"},
	}
	eligible, _, selected := evaluatePolicySelection(policyEvalInput{
		Record:  record,
		Views:   views,
		Signals: policySignals{LatencyMs: map[string]float64{"slow/a": 800, "fast/b": 80}},
	})
	if len(eligible) != 2 || eligible[0].Provider != "fast" || selected != 1 {
		t.Fatalf("eligible=%v selected=%v", eligible, selected)
	}
}

func TestEvaluatePolicySelectionAppliesCostCapWhenKnown(t *testing.T) {
	record := routingProfileRecord{
		Candidates: []routingProfileCandidate{
			{Provider: "pricey", Model: "a"},
			{Provider: "cheap", Model: "b"},
		},
		Optimize: map[string]any{"cost": 1.0},
		Limits:   map[string]any{"maxEstimatedCostUsd": 0.05},
	}
	views := []policyCandidateView{
		{Provider: "pricey", Model: "a", Adapter: "openai-chat"},
		{Provider: "cheap", Model: "b", Adapter: "openai-chat"},
	}
	eligible, rows, selected := evaluatePolicySelection(policyEvalInput{
		Record:  record,
		Views:   views,
		Signals: policySignals{CostUsd: map[string]float64{"pricey/a": 1.2, "cheap/b": 0.01}},
	})
	if len(eligible) != 1 || eligible[0].Provider != "cheap" || selected != 1 {
		t.Fatalf("eligible=%v selected=%v rows=%v", eligible, selected, rows)
	}
	cost, _ := rows[0]["cost"].(map[string]any)
	if cost["capOutcome"] != "exceeded" {
		t.Fatalf("cost=%v", cost)
	}
}

func asFloat(value any) float64 {
	n, _ := value.(float64)
	return n
}

func floatPtr(v float64) *float64 { return &v }
