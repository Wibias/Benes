package modeldiscovery

import (
	"slices"
	"testing"
)

func TestMissingPolicyLeavesArrivalsEnabled(t *testing.T) {
	got := Apply(Input{
		Discovered: []string{"a", "b", "c"},
		Baseline:   []string{"a", "b"},
	})
	if got.Bootstrap {
		t.Fatal("expected a later fetch, not bootstrap")
	}
	if len(got.NewlyDisabled) != 0 {
		t.Fatalf("disabled=%v", got.NewlyDisabled)
	}
	if !slices.Equal(got.Baseline, []string{"a", "b", "c"}) {
		t.Fatalf("baseline=%v", got.Baseline)
	}
}

func TestEmptyBaselineBootstrapsWithoutHiding(t *testing.T) {
	got := Apply(Input{
		Policy:     PolicyOff,
		Discovered: []string{"a", "b"},
	})
	if !got.Bootstrap {
		t.Fatal("expected bootstrap")
	}
	if len(got.NewlyDisabled) != 0 || len(got.Show) != 0 {
		t.Fatalf("hidden bootstrap newly=%v show=%v", got.NewlyDisabled, got.Show)
	}
	if !slices.Equal(got.Baseline, []string{"a", "b"}) {
		t.Fatalf("baseline=%v", got.Baseline)
	}
}

func TestPolicyOffDisablesALaterArrivalOnce(t *testing.T) {
	first := Apply(Input{
		Policy:     PolicyOff,
		Discovered: []string{"a", "b"},
		Baseline:   []string{"a", "b"},
	})
	if first.Changed || len(first.NewlyDisabled) != 0 {
		t.Fatalf("steady first=%+v", first)
	}

	second := Apply(Input{
		Policy:     PolicyOff,
		Discovered: []string{"a", "b", "c"},
		Baseline:   first.Baseline,
		Disabled:   first.Disabled,
	})
	if !slices.Equal(second.NewlyDisabled, []string{"c"}) {
		t.Fatalf("newly=%v", second.NewlyDisabled)
	}
	if !slices.Equal(second.Show, []string{"c"}) {
		t.Fatalf("show=%v", second.Show)
	}
	if !containsAll(second.Disabled, "c") {
		t.Fatalf("disabled=%v", second.Disabled)
	}

	enabled := Apply(Input{
		Policy:     PolicyOff,
		Discovered: []string{"a", "b", "c"},
		Baseline:   second.Baseline,
		Disabled:   filterString(second.Disabled, "c"),
		Show:       second.Show,
	})
	if containsAll(enabled.NewlyDisabled, "c") || containsAll(enabled.Disabled, "c") {
		t.Fatalf("re-disabled after enable: %+v", enabled)
	}
	if !containsAll(enabled.Show, "c") {
		t.Fatalf("enabled arrival must stay in the catalog overlay: show=%v", enabled.Show)
	}
}

func TestAllowlistAndPresetSkipDisableButAdvanceBaseline(t *testing.T) {
	got := Apply(Input{
		Policy:         PolicyOff,
		Discovered:     []string{"a", "new"},
		Baseline:       []string{"a"},
		SelectedModels: []string{"a"},
	})
	if len(got.NewlyDisabled) != 0 {
		t.Fatalf("allowlist must not double-manage arrivals: %+v", got)
	}
	if !slices.Equal(got.Baseline, []string{"a", "new"}) {
		t.Fatalf("baseline=%v", got.Baseline)
	}
}

func TestCustomModelsAreNotArrivals(t *testing.T) {
	got := Apply(Input{
		Policy:     PolicyOff,
		Discovered: []string{"a", "lab/fixture"},
		Baseline:   []string{"a"},
		CustomIDs:  []string{"lab/fixture"},
	})
	if containsAll(got.NewlyDisabled, "lab/fixture") {
		t.Fatalf("custom disabled: %+v", got)
	}
	if !slices.Equal(got.Baseline, []string{"a", "lab/fixture"}) {
		t.Fatalf("baseline=%v", got.Baseline)
	}
}

func TestIncompleteFetchDoesNotShrinkBaseline(t *testing.T) {
	got := Apply(Input{
		Policy:     PolicyOff,
		Incomplete: true,
		Discovered: []string{"a"},
		Baseline:   []string{"a", "b"},
		Disabled:   []string{"c"},
	})
	if got.Changed {
		t.Fatalf("incomplete must be a no-op: %+v", got)
	}
	if !slices.Equal(got.Baseline, []string{"a", "b"}) {
		t.Fatalf("baseline=%v", got.Baseline)
	}
}

func TestDisappearanceKeepsBaselineHistory(t *testing.T) {
	got := Apply(Input{
		Policy:     PolicyOff,
		Discovered: []string{"a"},
		Baseline:   []string{"a", "b"},
	})
	if !slices.Equal(got.Baseline, []string{"a", "b"}) {
		t.Fatalf("baseline shrank: %v", got.Baseline)
	}
	if got.Changed {
		t.Fatalf("steady disappearance rewrite: %+v", got)
	}
}

func TestEmptyLiveListWithExistingBaselineIsIncomplete(t *testing.T) {
	got := Apply(Input{
		Policy:     PolicyOff,
		Discovered: nil,
		Baseline:   []string{"a"},
	})
	if got.Changed {
		t.Fatalf("empty live list must not reset: %+v", got)
	}
}

func containsAll(list []string, id string) bool {
	return slices.Contains(list, id)
}

func filterString(list []string, drop string) []string {
	out := make([]string, 0, len(list))
	for _, id := range list {
		if id != drop {
			out = append(out, id)
		}
	}
	return out
}
