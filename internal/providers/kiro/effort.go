package kiro

import (
	"fmt"
	"strings"
)

var nativeEffortFields = map[string]string{
	"gpt-5.6-sol":   "reasoning",
	"claude-opus-5": "output_config",
}

var nativeEfforts = map[string]struct{}{
	"low": {}, "medium": {}, "high": {}, "xhigh": {}, "max": {},
}

func NativeEffortField(model string) string {
	return nativeEffortFields[strings.TrimSpace(strings.ToLower(model))]
}

func ApplyNativeEffort(payload map[string]any, model, effort string) error {
	effort = strings.TrimSpace(effort)
	if effort == "" || effort == "none" {
		return nil
	}
	field := NativeEffortField(model)
	if field == "" {
		return nil
	}
	if _, ok := nativeEfforts[effort]; !ok {
		return fmt.Errorf("Kiro %s does not support reasoning effort %q", model, effort)
	}
	payload["additionalModelRequestFields"] = map[string]any{field: map[string]any{"effort": effort}}
	return nil
}
