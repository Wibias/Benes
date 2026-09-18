package server

import (
	"testing"

	"github.com/Wibias/Benes/internal/catalog"
	"github.com/Wibias/Benes/internal/compat"
)

func TestEvaluatePolicyCandidatesSkipsNoVisionWhenImageRequired(t *testing.T) {
	image := true
	record := routingProfileRecord{
		Require: routingProfileRequire{ImageInput: &image},
		Candidates: []routingProfileCandidate{
			{Provider: "google", Model: "gemini-flash"},
			{Provider: "openai-apikey", Model: "gpt-5.4"},
		},
	}
	views := []policyCandidateView{
		{Provider: "google", Model: "gemini-flash", Vision: catalog.CapabilityFalse, Adapter: "google"},
		{Provider: "openai-apikey", Model: "gpt-5.4", Vision: catalog.CapabilityTrue, Adapter: "openai-responses"},
	}
	eligible, rows, selected := evaluatePolicyCandidates(record, views, policyRequestEvidence{})
	if len(eligible) != 1 || eligible[0].Provider != "openai-apikey" {
		t.Fatalf("eligible=%v", eligible)
	}
	if selected != 1 {
		t.Fatalf("selected=%v rows=%v", selected, rows)
	}
}

func TestEvaluatePolicyCandidatesSkipsDegradedWhenVerifiedRequired(t *testing.T) {
	record := routingProfileRecord{
		Candidates: []routingProfileCandidate{
			{Provider: "google", Model: "gemini-flash"},
			{Provider: "openai-apikey", Model: "gpt-5.4"},
		},
		Compatibility: map[string]any{
			"minStatus": "VERIFIED",
			"requiredSuites": []any{
				map[string]any{"suiteId": "codex", "evidenceLayer": "live_route_compatibility"},
			},
		},
	}
	views := []policyCandidateView{
		{Provider: "google", Model: "gemini-flash", Adapter: "google", Vision: catalog.CapabilityFalse},
		{Provider: "openai-apikey", Model: "gpt-5.4", Adapter: "openai-responses", Vision: catalog.CapabilityTrue},
	}
	if compat.Classify("codex", "google", catalog.CapabilityFalse) != compat.VerdictDegraded {
		t.Fatal("fixture: google should be degraded on codex")
	}
	eligible, _, selected := evaluatePolicyCandidates(record, views, policyRequestEvidence{})
	if len(eligible) != 1 || eligible[0].Provider != "openai-apikey" || selected != 1 {
		t.Fatalf("eligible=%v selected=%v", eligible, selected)
	}
}

func TestEvaluatePolicyCandidatesHonorsRequestImageEvidence(t *testing.T) {
	record := routingProfileRecord{
		Candidates: []routingProfileCandidate{
			{Provider: "google", Model: "gemini-flash"},
			{Provider: "openai-apikey", Model: "gpt-5.4"},
		},
	}
	views := []policyCandidateView{
		{Provider: "google", Model: "gemini-flash", Vision: catalog.CapabilityFalse, Adapter: "google"},
		{Provider: "openai-apikey", Model: "gpt-5.4", Vision: catalog.CapabilityTrue, Adapter: "openai-responses"},
	}
	eligible, _, _ := evaluatePolicyCandidates(record, views, policyRequestEvidence{Image: true})
	if len(eligible) != 1 || eligible[0].Provider != "openai-apikey" {
		t.Fatalf("eligible=%v", eligible)
	}
}

func TestEvaluatePolicyCandidatesSkipsNonForwardWhenEncryptedCodexRequired(t *testing.T) {
	need := true
	record := routingProfileRecord{
		Require: routingProfileRequire{EncryptedCodexTasks: &need},
		Candidates: []routingProfileCandidate{
			{Provider: "openai-apikey", Model: "gpt-5.4"},
			{Provider: "openai", Model: "gpt-5.4"},
		},
	}
	views := []policyCandidateView{
		{Provider: "openai-apikey", Model: "gpt-5.4", Adapter: "openai-responses", AuthMode: "key"},
		{Provider: "openai", Model: "gpt-5.4", Adapter: "openai-responses", AuthMode: "forward"},
	}
	eligible, _, selected := evaluatePolicyCandidates(record, views, policyRequestEvidence{})
	if len(eligible) != 1 || eligible[0].Provider != "openai" || selected != 1 {
		t.Fatalf("eligible=%v selected=%v", eligible, selected)
	}
}
