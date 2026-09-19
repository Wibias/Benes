package kiro

import (
	"fmt"
	"strings"
)

type nativeEffortContract struct {
	Field   string
	Efforts map[string]struct{}
}

func nativeEffortSet(values ...string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		out[value] = struct{}{}
	}
	return out
}

var nativeEffortContracts = map[string]nativeEffortContract{
	"gpt-5.6-sol": {
		Field: "reasoning", Efforts: nativeEffortSet("low", "medium", "high", "xhigh", "max"),
	},
	"gpt-5.6-terra": {
		Field: "reasoning", Efforts: nativeEffortSet("low", "medium", "high", "xhigh", "max"),
	},
	"gpt-5.6-luna": {
		Field: "reasoning", Efforts: nativeEffortSet("low", "medium", "high", "xhigh", "max"),
	},
	"claude-opus-5": {
		Field: "output_config", Efforts: nativeEffortSet("low", "medium", "high", "xhigh", "max"),
	},
}

func NativeEffortField(model string) string {
	return nativeEffortContracts[strings.TrimSpace(strings.ToLower(model))].Field
}

func ApplyNativeEffort(payload map[string]any, model, effort string) error {
	effort = strings.TrimSpace(effort)
	if effort == "" || effort == "none" {
		return nil
	}
	contract, ok := nativeEffortContracts[strings.TrimSpace(strings.ToLower(model))]
	if !ok || contract.Field == "" {
		return nil
	}
	if _, ok := contract.Efforts[effort]; !ok {
		return fmt.Errorf("Kiro %s does not support reasoning effort %q", model, effort)
	}
	payload["additionalModelRequestFields"] = map[string]any{contract.Field: map[string]any{"effort": effort}}
	return nil
}
