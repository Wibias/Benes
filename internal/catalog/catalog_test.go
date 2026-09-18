package catalog

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/credentialpool"
)

func boolp(v bool) *bool { return &v }

func TestEffectiveContextWindowTracksRealVsFallbackAndCap(t *testing.T) {
	tests := []struct {
		name       string
		in         ContextInput
		wantTokens int
		wantSource ContextSource
	}{
		{"discovered below cap", ContextInput{Discovered: 64000, Cap: 128000}, 64000, ContextDiscovered},
		{"discovered above cap", ContextInput{Discovered: 200000, Cap: 128000}, 128000, ContextCap},
		{"operator override beats discovered", ContextInput{Discovered: 64000, Operator: 96000, OperatorMode: WindowOverride, Cap: 128000}, 96000, ContextOperator},
		{"operator fallback fills missing", ContextInput{Operator: 96000, OperatorMode: WindowFallback}, 96000, ContextOperator},
		{"static fills missing", ContextInput{Static: 32768}, 32768, ContextStatic},
		{"cap becomes window when no real value", ContextInput{Cap: 40000}, 40000, ContextCap},
		{"disabled cap leaves conservative fallback", ContextInput{Cap: 40000, CapDisabled: true}, ConservativeContextWindow, ContextConservativeDefault},
		{"no data leaves conservative fallback", ContextInput{}, ConservativeContextWindow, ContextConservativeDefault},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := EffectiveContextWindow(tc.in)
			if got.Tokens != tc.wantTokens || got.Source != tc.wantSource {
				t.Fatalf("got %#v want tokens=%d source=%q", got, tc.wantTokens, tc.wantSource)
			}
		})
	}
}

func TestReasoningExactProviderAndModelOverrides(t *testing.T) {
	configured := []string{"low", "medium", "high"}
	if got := EffectiveReasoningEfforts(configured, false, nil); !equalStrings(got, []string{"low", "medium", "high", "max", "ultra"}) {
		t.Fatalf("default synthesis = %#v", got)
	}
	if got := EffectiveReasoningEfforts(configured, true, nil); !equalStrings(got, configured) {
		t.Fatalf("provider exact = %#v", got)
	}
	if got := EffectiveReasoningEfforts(configured, true, boolp(false)); !equalStrings(got, []string{"low", "medium", "high", "max", "ultra"}) {
		t.Fatalf("model false override = %#v", got)
	}
	if got := EffectiveReasoningEfforts(configured, false, boolp(true)); !equalStrings(got, configured) {
		t.Fatalf("model true override = %#v", got)
	}
}

func TestResolveAutoReviewModelUsesRootScopeOnly(t *testing.T) {
	raw := json.RawMessage(`{"auto_review_model":"reviewer","providers":{"x":{"model":"wrong"}},"table":{"model":"also-wrong"}}`)
	got, ok := ResolveAutoReviewModel(raw)
	if !ok || got != "reviewer" {
		t.Fatalf("got %q %v", got, ok)
	}
	if got, ok := ResolveAutoReviewModel(json.RawMessage(`{"providers":{"x":{"model":"wrong"}}}`)); ok || got != "" {
		t.Fatalf("nested model leaked into review model: %q %v", got, ok)
	}
}

func TestSoftCompactionIsLoweringOnlyAndReclamps(t *testing.T) {
	if got := EffectiveAutoCompactLimit(100000, 80000, 0); got != 80000 {
		t.Fatalf("max input ceiling not applied: %d", got)
	}
	if got := EffectiveAutoCompactLimit(100000, 0, 95000); got != 90000 {
		t.Fatalf("configured limit raised derived bound: %d", got)
	}
	if got := EffectiveAutoCompactLimit(100000, 0, 70000); got != 70000 {
		t.Fatalf("configured lowering not applied: %d", got)
	}
	if got := EffectiveAutoCompactLimit(50000, 0, 70000); got != 45000 {
		t.Fatalf("narrowed context did not reclamp stale configured limit: %d", got)
	}
}

func TestCompleteGeminiTierSetCollapsesBeforeDisplayNames(t *testing.T) {
	rows := []DiscoveredModel{
		{ID: "wire-low", LogicalID: "gemini-3-pro", ReasoningTier: "low", DisplayName: "Gemini Pro (Thinking Low)", ContextWindow: 200000},
		{ID: "wire-medium", LogicalID: "gemini-3-pro", ReasoningTier: "medium", DisplayName: "Marketing Medium Suffix", ContextWindow: 200000},
		{ID: "wire-high", LogicalID: "gemini-3-pro", ReasoningTier: "high", DisplayName: "Something High", ContextWindow: 200000},
	}
	got := NormalizeDiscovered(rows)
	if len(got) != 1 || got[0].ID != "gemini-3-pro" {
		t.Fatalf("collapse = %#v", got)
	}
	if got[0].ReasoningWire["low"] != "wire-low" || got[0].ReasoningWire["medium"] != "wire-medium" || got[0].ReasoningWire["high"] != "wire-high" {
		t.Fatalf("wire map lost: %#v", got[0].ReasoningWire)
	}
	if !equalStrings(got[0].ReasoningEfforts, []string{"low", "medium", "high"}) {
		t.Fatalf("efforts = %#v", got[0].ReasoningEfforts)
	}
}

func TestIncompleteGeminiTierSetIsNotCollapsed(t *testing.T) {
	rows := []DiscoveredModel{
		{ID: "wire-low", LogicalID: "gemini-3-pro", ReasoningTier: "low"},
		{ID: "wire-high", LogicalID: "gemini-3-pro", ReasoningTier: "high"},
	}
	got := NormalizeDiscovered(rows)
	if len(got) != 2 || got[0].ID == "gemini-3-pro" || got[1].ID == "gemini-3-pro" {
		t.Fatalf("incomplete set collapsed: %#v", got)
	}
}

func TestProjectionRetainsConfiguredMissingModelWithoutInventingCapabilities(t *testing.T) {
	policy := Policy{RetainModels: map[string]map[string]bool{"p": {"missing": true}}}
	got, err := Project(ProjectionInput{
		ProviderID: "p", Destination: "https://example.test", Policy: policy,
		Configured: []ConfiguredModel{{ID: "missing", ContextWindow: 32000}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("rows = %#v", got)
	}
	row := got[0]
	if !row.Retained || row.Discovered || row.ConfirmedCallable {
		t.Fatalf("retention truth lost: %#v", row)
	}
	if row.Vision != CapabilityUnknown {
		t.Fatalf("invented vision capability: %q", row.Vision)
	}
	if row.Context.Tokens != 32000 || row.Context.Source != ContextStatic {
		t.Fatalf("configured context lost: %#v", row.Context)
	}
	if row.AdvertisedTokens != 32000 {
		t.Fatalf("advertised window lost: %d", row.AdvertisedTokens)
	}
}

func TestProviderDestinationScopedModalityDoesNotLeakBySlug(t *testing.T) {
	policy := Policy{StaticVision: map[CapabilityKey]CapabilityState{
		{ProviderID: "mimo", Destination: "https://api.xiaomi.example", ModelID: "mimo-v2.5-pro"}: CapabilityFalse,
		{ProviderID: "mimo", Destination: "https://api.xiaomi.example", ModelID: "mimo-v2.5"}:     CapabilityTrue,
	}}
	canonical, err := Project(ProjectionInput{ProviderID: "mimo", Destination: "https://api.xiaomi.example", Policy: policy, Discovered: []DiscoveredModel{{ID: "mimo-v2.5-pro"}}})
	if err != nil {
		t.Fatal(err)
	}
	custom, err := Project(ProjectionInput{ProviderID: "custom", Destination: "https://other.example", Policy: policy, Discovered: []DiscoveredModel{{ID: "mimo-v2.5-pro"}}})
	if err != nil {
		t.Fatal(err)
	}
	if canonical[0].Vision != CapabilityFalse || custom[0].Vision != CapabilityUnknown {
		t.Fatalf("modality leaked: canonical=%q custom=%q", canonical[0].Vision, custom[0].Vision)
	}
}

func TestQuotaAvailabilityUsesSharedCredentialEvidence(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	exhausted := credentialpool.Candidate{Ref: "a", Destination: "d", AuthClass: "oauth", Evidence: credentialpool.Evidence{Auth: credentialpool.AuthUsable, Quota: credentialpool.QuotaKnown, Utilization: 1, ValidUntil: now.Add(time.Minute), Limit: credentialpool.LimitAvailable}}
	healthy := credentialpool.Candidate{Ref: "b", Destination: "d", AuthClass: "oauth", Evidence: credentialpool.Evidence{Auth: credentialpool.AuthUsable, Quota: credentialpool.QuotaKnown, Utilization: .5, ValidUntil: now.Add(time.Minute), Limit: credentialpool.LimitAvailable}}
	unknown := credentialpool.Candidate{Ref: "c", Destination: "d", AuthClass: "oauth", Evidence: credentialpool.Evidence{Auth: credentialpool.AuthUsable, Quota: credentialpool.QuotaUnknown, Limit: credentialpool.LimitAvailable}}
	stale := exhausted
	stale.Ref = "stale"
	stale.Evidence.ValidUntil = now.Add(-time.Second)

	cases := []struct {
		name   string
		c      []credentialpool.Candidate
		active bool
		reason string
	}{
		{"all exhausted", []credentialpool.Candidate{exhausted}, false, "no_credit"},
		{"one healthy", []credentialpool.Candidate{exhausted, healthy}, true, ""},
		{"unknown", []credentialpool.Candidate{exhausted, unknown}, true, ""},
		{"stale exhaustion", []credentialpool.Candidate{stale}, true, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := AvailabilityFromCandidates(tc.c, now)
			if got.Selectable != tc.active || got.Reason != tc.reason {
				t.Fatalf("got %#v", got)
			}
		})
	}
}

func TestComboUsesCapabilityIntersectionAndMinima(t *testing.T) {
	a := Model{ID: "a", Context: ContextWindow{Tokens: 100000, Source: ContextDiscovered}, MaxInput: 80000, AutoCompactTokenLimit: 70000, ReasoningEfforts: []string{"low", "medium", "high"}, Vision: CapabilityTrue}
	b := Model{ID: "b", Context: ContextWindow{Tokens: 64000, Source: ContextOperator}, MaxInput: 60000, AutoCompactTokenLimit: 50000, ReasoningEfforts: []string{"medium", "high"}, Vision: CapabilityFalse}
	got, err := SynthesizeCombo("combo", []Model{a, b})
	if err != nil {
		t.Fatal(err)
	}
	if got.Context.Tokens != 64000 || got.MaxInput != 60000 || got.AutoCompactTokenLimit != 50000 {
		t.Fatalf("minima wrong: %#v", got)
	}
	if !equalStrings(got.ReasoningEfforts, []string{"medium", "high"}) || got.Vision != CapabilityFalse {
		t.Fatalf("intersection wrong: %#v", got)
	}
}

func TestAliasProjectionPreservesMetadataAndReclampsSoftBudget(t *testing.T) {
	base := Model{ID: "base", Context: ContextWindow{Tokens: 100000, Source: ContextDiscovered}, MaxInput: 90000, AutoCompactTokenLimit: 80000, AutoReviewModel: "reviewer", Vision: CapabilityTrue}
	alias := ProjectAlias(base, "account/base", 60000)
	if alias.ID != "account/base" || alias.Context.Tokens != 60000 || alias.AutoCompactTokenLimit != 54000 || alias.AutoReviewModel != "reviewer" || alias.Vision != CapabilityTrue {
		t.Fatalf("alias projection = %#v", alias)
	}
}

func TestProjectionKeepsAdvertisedWindowWhenOperatorOverrides(t *testing.T) {
	got, err := Project(ProjectionInput{
		ProviderID:  "openai",
		Destination: "https://example.test",
		Policy: Policy{
			RetainModels: map[string]map[string]bool{"openai": {"gpt-5.6": true}},
			ModelContextWindows: map[string]map[string]ContextPolicyValue{
				"openai": {"gpt-5.6": {Tokens: 64000, Mode: WindowOverride}},
			},
		},
		Configured: []ConfiguredModel{{ID: "gpt-5.6", ContextWindow: 1_050_000, MaxInput: 272_000}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("rows = %#v", got)
	}
	row := got[0]
	if row.Context.Tokens != 64000 || row.Context.Source != ContextOperator {
		t.Fatalf("effective window = %#v", row.Context)
	}
	if row.AdvertisedTokens != 1_050_000 {
		t.Fatalf("advertised=%d", row.AdvertisedTokens)
	}
	if row.StandardTokens != 272_000 {
		t.Fatalf("standard=%d", row.StandardTokens)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
