package catalog

import "testing"

func TestSynthesizeComboIntersectsRuntimeCapabilities(t *testing.T) {
	a := Model{
		ID: "a", Context: ContextWindow{Tokens: 128000, Source: ContextOperator},
		APITypes: []string{"chat_completions", "responses", "anthropic_messages"},
		ToolUse: CapabilityTrue, Streaming: CapabilityTrue, Reasoning: CapabilityTrue, Vision: CapabilityTrue,
		ReasoningEfforts: []string{"low", "medium", "high"},
		Availability: Availability{Selectable: true},
	}
	b := Model{
		ID: "b", Context: ContextWindow{Tokens: 64000, Source: ContextCap},
		APITypes: []string{"chat_completions", "responses"},
		ToolUse: CapabilityFalse, Streaming: CapabilityTrue, Reasoning: CapabilityUnknown, Vision: CapabilityFalse,
		ReasoningEfforts: []string{"medium", "high"},
		Availability: Availability{Selectable: true},
	}

	got, err := SynthesizeCombo("combo/test", []Model{a, b})
	if err != nil {
		t.Fatal(err)
	}
	if got.Context.Tokens != 64000 || got.Context.Source != ContextCap {
		t.Fatalf("context=%+v", got.Context)
	}
	if len(got.APITypes) != 2 || got.APITypes[0] != "chat_completions" || got.APITypes[1] != "responses" {
		t.Fatalf("api types=%v", got.APITypes)
	}
	if got.ToolUse != CapabilityFalse || got.Streaming != CapabilityTrue || got.Reasoning != CapabilityUnknown || got.Vision != CapabilityFalse {
		t.Fatalf("capabilities tool=%q streaming=%q reasoning=%q vision=%q", got.ToolUse, got.Streaming, got.Reasoning, got.Vision)
	}
	if len(got.ReasoningEfforts) != 2 || got.ReasoningEfforts[0] != "medium" || got.ReasoningEfforts[1] != "high" {
		t.Fatalf("reasoning efforts=%v", got.ReasoningEfforts)
	}
}

func TestProjectAliasPreservesRuntimeCapabilities(t *testing.T) {
	base := Model{
		ID: "provider/model", Context: ContextWindow{Tokens: 128000, Source: ContextDiscovered},
		APITypes: []string{"responses"}, ToolUse: CapabilityTrue, Streaming: CapabilityTrue,
		Reasoning: CapabilityTrue, Vision: CapabilityTrue, ReasoningEfforts: []string{"high"},
	}
	alias := ProjectAlias(base, "alias", 64000)
	if alias.Context.Tokens != 64000 || alias.Context.Source != ContextCap {
		t.Fatalf("context=%+v", alias.Context)
	}
	if len(alias.APITypes) != 1 || alias.APITypes[0] != "responses" || alias.ToolUse != CapabilityTrue || alias.Streaming != CapabilityTrue || alias.Reasoning != CapabilityTrue || alias.Vision != CapabilityTrue {
		t.Fatalf("alias=%+v", alias)
	}
	alias.APITypes[0] = "chat_completions"
	if base.APITypes[0] != "responses" {
		t.Fatal("alias APITypes shares backing storage with base")
	}
}

func TestProjectConfiguredVisionCanNarrowDiscoveredVision(t *testing.T) {
	models, err := Project(ProjectionInput{
		ProviderID: "p",
		Discovered: []DiscoveredModel{{
			ID: "m", ContextWindow: 128000, Vision: CapabilityTrue,
		}},
		Configured: []ConfiguredModel{{
			ID: "m", Vision: CapabilityFalse,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 {
		t.Fatalf("models=%#v", models)
	}
	if models[0].Vision != CapabilityFalse {
		t.Fatalf("vision=%q; explicit configured text-only capability must narrow discovered image support", models[0].Vision)
	}
}

func TestProjectConfiguredVisionDoesNotWidenDiscoveredFalse(t *testing.T) {
	models, err := Project(ProjectionInput{
		ProviderID: "p",
		Discovered: []DiscoveredModel{{
			ID: "m", ContextWindow: 128000, Vision: CapabilityFalse,
		}},
		Configured: []ConfiguredModel{{
			ID: "m", Vision: CapabilityTrue,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].Vision != CapabilityFalse {
		t.Fatalf("models=%#v; configured metadata must not optimistically widen a discovered false", models)
	}
}

func TestProjectExplicitEmptyReasoningCanNarrowDiscoveredReasoning(t *testing.T) {
	models, err := Project(ProjectionInput{
		ProviderID: "p",
		Discovered: []DiscoveredModel{{
			ID: "m", ContextWindow: 128000, ReasoningEfforts: []string{"high"},
		}},
		Configured: []ConfiguredModel{{
			ID: "m", ReasoningEfforts: []string{},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 {
		t.Fatalf("models=%#v", models)
	}
	if models[0].Reasoning != CapabilityFalse || len(models[0].ReasoningEfforts) != 0 {
		t.Fatalf("reasoning=%q efforts=%v; explicit empty reasoning metadata must narrow discovered reasoning support", models[0].Reasoning, models[0].ReasoningEfforts)
	}
}
