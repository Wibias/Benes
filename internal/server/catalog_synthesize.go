package server

import (
	"strings"

	"github.com/Wibias/Benes/internal/catalog"
)

func (h *handler) reloadSynthesizedCatalog() {
	if h == nil {
		return
	}
	base := make([]catalog.Model, 0, len(h.catalogModels))
	byID := map[string]catalog.Model{}
	for _, model := range h.catalogModels {
		id := strings.TrimSpace(model.ID)
		if id == "" || strings.HasPrefix(id, comboNamespace+"/") || strings.HasPrefix(id, policyNamespace+"/") {
			continue
		}
		base = append(base, model)
		byID[id] = model
	}
	next := append([]catalog.Model(nil), base...)
	for _, spec := range h.listRuntimeCombos() {
		if synthesized, ok := synthesizeMemberRow(comboNamespace+"/"+spec.ID, spec.Targets, byID); ok {
			next = append(next, synthesized)
		}
	}
	if strings.TrimSpace(h.configPath) != "" {
		if parsed, err := h.routingProfileRawMap(); err == nil {
			for id := range parsed {
				record, ok := h.routingProfileRecord(id)
				if !ok {
					continue
				}
				targets := make([]ComboTarget, 0, len(record.Candidates))
				for _, candidate := range record.Candidates {
					targets = append(targets, ComboTarget{ProviderID: candidate.Provider, Model: candidate.Model})
				}
				if synthesized, ok := synthesizeMemberRow(policyNamespace+"/"+id, targets, byID); ok {
					next = append(next, synthesized)
				}
			}
		}
	}
	h.catalogModels = next
	h.notifyCatalogueChanged()
}

func synthesizeMemberRow(id string, targets []ComboTarget, byID map[string]catalog.Model) (catalog.Model, bool) {
	members := make([]catalog.Model, 0, len(targets))
	for _, target := range targets {
		member, ok := byID[target.ProviderID+"/"+target.Model]
		if !ok {
			return catalog.Model{}, false
		}
		members = append(members, member)
	}
	if len(members) == 0 {
		return catalog.Model{}, false
	}
	row, err := catalog.SynthesizeCombo(id, members)
	if err != nil {
		return catalog.Model{}, false
	}
	return row, true
}
