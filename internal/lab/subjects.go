package lab

import (
	"sort"
	"strings"

	"github.com/Wibias/Benes/internal/compat"
	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/modeldiscovery"
)

type Subject struct {
	ID       string
	Provider string
	Model    string
	Adapter  string
}

func SubjectsFromConfig(disk config.DiskConfig) []Subject {
	snap := modeldiscovery.DecodeConfig(disk.Raw)
	catalogs := modeldiscovery.CatalogsFromConfig(disk.Providers, disk.Raw, snap)
	byID := map[string]Subject{}
	for _, cat := range catalogs {
		if cat.ID == "combo" || cat.ID == "policy" || strings.Contains(cat.ID, "/") {
			continue
		}
		adapter := compat.ProviderAdapter(disk.Providers[cat.ID])
		seen := map[string]struct{}{}
		add := func(id string) {
			if id == "" {
				return
			}
			full := modeldiscovery.CatalogID(cat.ID, id)
			if _, ok := seen[full]; ok {
				return
			}
			seen[full] = struct{}{}
			provider, model := compat.SplitModelID(full)
			byID[full] = Subject{ID: full, Provider: provider, Model: model, Adapter: adapter}
		}
		for _, id := range cat.Selected {
			add(id)
		}
		for _, id := range cat.Custom {
			add(id)
		}
		for _, id := range cat.Discovered {
			add(id)
		}
	}
	out := make([]Subject, 0, len(byID))
	for _, s := range byID {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
