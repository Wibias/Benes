package config

import (
	"encoding/json"
	"sort"
	"strings"
	"unicode"
)

type ComboTarget struct {
	ProviderID string
	Model      string
}

type ComboSpec struct {
	ID       string
	Strategy string
	Targets  []ComboTarget
}

type ComboProjectionSkip struct {
	ID   string
	Code string
}

type ComboProjection struct {
	Combos  []ComboSpec
	Skipped []ComboProjectionSkip
}

func ProjectCombos(disk DiskConfig) ComboProjection {
	var root struct {
		Combos json.RawMessage `json:"combos"`
	}
	if json.Unmarshal(disk.Raw, &root) != nil || len(strings.TrimSpace(string(root.Combos))) == 0 {
		return ComboProjection{}
	}
	var decoded map[string]json.RawMessage
	if json.Unmarshal(root.Combos, &decoded) != nil || decoded == nil {
		return ComboProjection{Skipped: []ComboProjectionSkip{{ID: "combos", Code: "malformed"}}}
	}
	ids := make([]string, 0, len(decoded))
	for id := range decoded {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := ComboProjection{}
	for _, id := range ids {
		spec, skip := projectCombo(id, decoded[id], disk.Providers)
		if skip != nil {
			out.Skipped = append(out.Skipped, *skip)
			continue
		}
		out.Combos = append(out.Combos, spec)
	}
	return out
}

func projectCombo(id string, raw json.RawMessage, providers map[string]json.RawMessage) (ComboSpec, *ComboProjectionSkip) {
	if !validComboID(id) {
		return ComboSpec{}, &ComboProjectionSkip{ID: id, Code: "invalid_id"}
	}
	var body struct {
		Strategy string `json:"strategy"`
		Targets  []struct {
			Provider string `json:"provider"`
			Model    string `json:"model"`
		} `json:"targets"`
	}
	if json.Unmarshal(raw, &body) != nil {
		return ComboSpec{}, &ComboProjectionSkip{ID: id, Code: "malformed"}
	}
	strategy := strings.TrimSpace(body.Strategy)
	if strategy == "" || strings.EqualFold(strategy, "round-robin") {
		strategy = "failover"
	}
	if strategy != "failover" {
		return ComboSpec{}, &ComboProjectionSkip{ID: id, Code: "unsupported_strategy"}
	}
	if len(body.Targets) == 0 {
		return ComboSpec{}, &ComboProjectionSkip{ID: id, Code: "empty_targets"}
	}
	targets := make([]ComboTarget, 0, len(body.Targets))
	for _, target := range body.Targets {
		providerID := strings.TrimSpace(target.Provider)
		model := strings.TrimSpace(target.Model)
		if providerID == "" || model == "" {
			return ComboSpec{}, &ComboProjectionSkip{ID: id, Code: "invalid_target"}
		}
		if _, ok := providers[providerID]; !ok {
			return ComboSpec{}, &ComboProjectionSkip{ID: id, Code: "unknown_provider"}
		}
		targets = append(targets, ComboTarget{ProviderID: providerID, Model: model})
	}
	return ComboSpec{ID: id, Strategy: strategy, Targets: targets}, nil
}

func ValidComboID(id string) bool {
	return validComboID(id)
}

func validComboID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for i, r := range id {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			continue
		}
		if i > 0 && (r == '.' || r == '_' || r == '-') {
			continue
		}
		return false
	}
	return true
}
