package modeldiscovery

import (
	"slices"
	"sort"
	"strings"
)

type Policy string

const (
	PolicyOn  Policy = "on"
	PolicyOff Policy = "off"
)

func Resolve(global, perProvider string) Policy {
	switch strings.TrimSpace(perProvider) {
	case string(PolicyOff):
		return PolicyOff
	case string(PolicyOn):
		return PolicyOn
	}
	if strings.TrimSpace(global) == string(PolicyOff) {
		return PolicyOff
	}
	return PolicyOn
}

type Input struct {
	Policy         Policy
	Incomplete     bool
	Discovered     []string
	Baseline       []string
	Disabled       []string
	Show           []string
	SelectedModels []string
	CustomIDs      []string
}

type Result struct {
	Baseline      []string
	Disabled      []string
	Show          []string
	NewlyDisabled []string
	Bootstrap     bool
	Changed       bool
}

func Apply(in Input) Result {
	baseline := uniqueSorted(in.Baseline)
	disabled := uniqueSorted(in.Disabled)
	show := uniqueSorted(in.Show)
	discovered := uniqueSorted(in.Discovered)

	out := Result{Baseline: baseline, Disabled: disabled, Show: show}
	if in.Incomplete || (len(discovered) == 0 && len(baseline) > 0) {
		return out
	}
	if len(baseline) == 0 {
		out.Baseline = discovered
		out.Bootstrap = true
		out.Changed = len(discovered) > 0
		return out
	}

	custom := setOf(in.CustomIDs)
	known := setOf(baseline)
	arrivals := make([]string, 0)
	for _, id := range discovered {
		if _, ok := known[id]; ok {
			continue
		}
		if _, ok := custom[id]; ok {
			continue
		}
		arrivals = append(arrivals, id)
	}

	newBaseline := uniqueSorted(append(append([]string{}, baseline...), discovered...))
	skipDisable := in.Policy != PolicyOff || len(in.SelectedModels) > 0
	newly := make([]string, 0)
	if !skipDisable {
		blocked := setOf(disabled)
		for _, id := range arrivals {
			if _, ok := blocked[id]; ok {
				continue
			}
			newly = append(newly, id)
		}
		if len(newly) > 0 {
			disabled = uniqueSorted(append(disabled, newly...))
			show = uniqueSorted(append(show, newly...))
		}
	}

	out.Baseline = newBaseline
	out.Disabled = disabled
	out.Show = show
	out.NewlyDisabled = newly
	out.Changed = !slices.Equal(newBaseline, baseline) || len(newly) > 0 || !slices.Equal(show, uniqueSorted(in.Show))
	return out
}

func uniqueSorted(ids []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func setOf(ids []string) map[string]struct{} {
	out := make(map[string]struct{}, len(ids))
	for _, id := range uniqueSorted(ids) {
		out[id] = struct{}{}
	}
	return out
}
