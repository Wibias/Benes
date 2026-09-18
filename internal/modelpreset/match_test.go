package modelpreset

import (
	"slices"
	"testing"
)

func TestOpenRouterPresetMatchesCurrentCoreAndSkipsHistoricalTail(t *testing.T) {
	spec, ok := Lookup("openrouter")
	if !ok {
		t.Fatal("openrouter has no preset")
	}
	if spec.Version != 1 {
		t.Fatalf("version=%d", spec.Version)
	}
	catalog := []string{
		"anthropic/claude-opus-5",
		"anthropic/claude-sonnet-5",
		"anthropic/claude-sonnet-4-6",
		"anthropic/claude-3-opus",
		"anthropic/claude-opus-5:free",
		"openai/gpt-5.6-sol",
		"openai/gpt-5.6-terra",
		"openai/gpt-5.6-luna",
		"openai/gpt-5.5",
		"openai/gpt-4o",
		"google/gemini-3.5-flash",
		"google/gemini-3.7-flash",
		"google/gemini-2.5-flash",
		"x-ai/grok-4.6",
		"x-ai/grok-3",
		"deepseek/deepseek-v4-flash",
		"meta-llama/llama-3.1-405b",
	}
	got := Match(catalog, spec)
	want := []string{
		"anthropic/claude-opus-5",
		"anthropic/claude-sonnet-5",
		"deepseek/deepseek-v4-flash",
		"google/gemini-3.5-flash",
		"google/gemini-3.7-flash",
		"openai/gpt-5.6-luna",
		"openai/gpt-5.6-sol",
		"openai/gpt-5.6-terra",
		"x-ai/grok-4.6",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

func TestLookupUnknownProviderHasNoPreset(t *testing.T) {
	if _, ok := Lookup("deepseek"); ok {
		t.Fatal("deepseek should not ship a preset without evidence")
	}
}
