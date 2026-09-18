package modelpreset

import (
	"slices"
	"testing"
)

func TestSeedUnionsCustomModelsSoTheyDoNotDisappear(t *testing.T) {
	spec, _ := Lookup("openrouter")
	got := Seed([]string{"openai/gpt-5.6-sol", "openai/gpt-4o"}, []string{"acme/private-coder", "openai/gpt-5.6-sol"}, spec)
	want := []string{"acme/private-coder", "openai/gpt-5.6-sol"}
	if !slices.Equal(got, want) {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

func TestApplyPresetZeroMatchKeepsPreviousSelection(t *testing.T) {
	spec, _ := Lookup("openrouter")
	got := Apply(Decision{
		Mode:     ModePreset,
		Catalog:  []string{"openai/gpt-4o", "meta-llama/llama-3.1-405b"},
		Custom:   nil,
		Previous: []string{"openai/gpt-4o"},
		Marker:   Marker{Mode: ModeAll},
		Spec:     spec,
	})
	if got.Changed {
		t.Fatal("zero-match rewrote the selection")
	}
	if !slices.Equal(got.Selected, []string{"openai/gpt-4o"}) {
		t.Fatalf("selected=%q", got.Selected)
	}
	if got.Marker.Mode != ModeAll {
		t.Fatalf("mode=%s", got.Marker.Mode)
	}
	if got.Warning == "" {
		t.Fatal("missing zero-match warning")
	}
}

func TestApplyPresetSeedsAllowlistAndMarksPreset(t *testing.T) {
	spec, _ := Lookup("openrouter")
	got := Apply(Decision{
		Mode:     ModePreset,
		Catalog:  []string{"openai/gpt-5.6-sol", "openai/gpt-4o"},
		Custom:   []string{"lab/fixture"},
		Previous: nil,
		Spec:     spec,
	})
	if !got.Changed {
		t.Fatal("successful preset reported no change")
	}
	if got.Marker.Mode != ModePreset || got.Marker.AppliedVersion != 1 {
		t.Fatalf("marker=%#v", got.Marker)
	}
	if !slices.Equal(got.Selected, []string{"lab/fixture", "openai/gpt-5.6-sol"}) {
		t.Fatalf("selected=%q", got.Selected)
	}
	if got.Warning != "" {
		t.Fatalf("warning=%q", got.Warning)
	}
}

func TestApplyAllClearsAllowlist(t *testing.T) {
	got := Apply(Decision{
		Mode:     ModeAll,
		Previous: []string{"openai/gpt-5.6-sol"},
		Marker:   Marker{Mode: ModePreset, AppliedVersion: 1},
	})
	if !got.Changed {
		t.Fatal("all reported no change")
	}
	if len(got.Selected) != 0 {
		t.Fatalf("selected=%q", got.Selected)
	}
	if got.Marker.Mode != ModeAll {
		t.Fatalf("mode=%s", got.Marker.Mode)
	}
}

func TestUserSelectedWriteWhilePresetMarksCustom(t *testing.T) {
	if !MarkCustom(ModePreset) {
		t.Fatal("preset write should diverge")
	}
	if MarkCustom(ModeAll) || MarkCustom(ModeCustom) || MarkCustom("") {
		t.Fatal("non-preset writes should not force custom")
	}
}
