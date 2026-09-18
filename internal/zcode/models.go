package zcode

import (
	"strings"

	"github.com/Wibias/Benes/internal/catalog"
)

func ModelSelectors(models []catalog.Model) map[string]any {
	out := make(map[string]any)
	for _, model := range models {
		id := strings.TrimSpace(model.ID)
		if id == "" || !model.Availability.Selectable {
			continue
		}
		var declared []string
		switch model.Vision {
		case catalog.CapabilityTrue:
			declared = []string{"text", "image"}
		case catalog.CapabilityFalse:
			declared = []string{"text"}
		}
		input := inputForZCode(declared)
		if input == nil {
			continue
		}
		entry := map[string]any{
			"name":  id,
			"input": input,
		}
		if model.Context.Tokens > 0 && model.Context.Source != catalog.ContextConservativeDefault {
			entry["contextWindow"] = model.Context.Tokens
		}
		out[id] = entry
	}
	return out
}

func inputForZCode(declared []string) []string {
	if len(declared) == 0 {
		return []string{"text"}
	}
	kept := make([]string, 0, 2)
	seen := map[string]bool{}
	for _, value := range declared {
		if value != "text" && value != "image" {
			continue
		}
		if seen[value] {
			continue
		}
		seen[value] = true
		kept = append(kept, value)
	}
	if len(kept) == 0 {
		return nil
	}
	return kept
}
