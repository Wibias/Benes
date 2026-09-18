package modelpreset

import (
	"sort"
	"time"
)

type Mode string

const (
	ModePreset Mode = "preset"
	ModeAll    Mode = "all"
	ModeCustom Mode = "custom"
)

type Marker struct {
	Mode           Mode   `json:"mode"`
	AppliedVersion int    `json:"appliedVersion,omitempty"`
	AppliedAt      string `json:"appliedAt,omitempty"`
	Warning        string `json:"warning,omitempty"`
}

type Decision struct {
	Mode     Mode
	Catalog  []string
	Custom   []string
	Previous []string
	Marker   Marker
	Spec     Spec
	Now      time.Time
}

type Result struct {
	Selected []string
	Marker   Marker
	Changed  bool
	Warning  string
}

func Seed(catalogIDs, customIDs []string, spec Spec) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0)
	for _, id := range Match(catalogIDs, spec) {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	for _, id := range customIDs {
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

func Apply(in Decision) Result {
	now := in.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	stamp := now.Format(time.RFC3339)
	switch in.Mode {
	case ModeAll:
		return Result{
			Selected: nil,
			Marker:   Marker{Mode: ModeAll, AppliedAt: stamp},
			Changed:  true,
		}
	case ModePreset:
		matched := Match(in.Catalog, in.Spec)
		if len(matched) == 0 {
			return Result{
				Selected: append([]string(nil), in.Previous...),
				Marker:   in.Marker,
				Changed:  false,
				Warning:  "preset matched 0 catalog models; previous selection kept",
			}
		}
		selected := Seed(in.Catalog, in.Custom, in.Spec)
		return Result{
			Selected: selected,
			Marker: Marker{
				Mode:           ModePreset,
				AppliedVersion: in.Spec.Version,
				AppliedAt:      stamp,
			},
			Changed: true,
		}
	default:
		return Result{
			Selected: append([]string(nil), in.Previous...),
			Marker:   in.Marker,
			Changed:  false,
			Warning:  "unknown preset mode",
		}
	}
}

func MarkCustom(mode Mode) bool {
	return mode == ModePreset
}
