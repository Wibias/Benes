package google

import "strings"

var googleThinkingLevels = map[string]struct{}{
	"minimal": {},
	"low":     {},
	"medium":  {},
	"high":    {},
}

func ThinkingLevel(requested string, catalogEfforts []string) string {
	if len(catalogEfforts) == 0 {
		return ""
	}
	requested = strings.ToLower(strings.TrimSpace(requested))
	if requested == "" || requested == "none" {
		return ""
	}
	if requested == "xhigh" || requested == "max" || requested == "ultra" {
		requested = "high"
	}
	if _, ok := googleThinkingLevels[requested]; !ok {
		return ""
	}
	if containsEffort(catalogEfforts, requested) || catalogHasThinking(catalogEfforts) {
		return requested
	}
	return ""
}

func ApplyThinking(body map[string]any, requested string, catalogEfforts []string) {
	level := ThinkingLevel(requested, catalogEfforts)
	if level == "" || body == nil {
		return
	}
	gc, _ := body["generationConfig"].(map[string]any)
	if gc == nil {
		gc = map[string]any{}
	}
	gc["thinkingConfig"] = map[string]any{"thinkingLevel": level}
	body["generationConfig"] = gc
}

func catalogHasThinking(efforts []string) bool {
	for _, effort := range efforts {
		if _, ok := googleThinkingLevels[strings.ToLower(strings.TrimSpace(effort))]; ok {
			return true
		}
	}
	return false
}

func containsEffort(efforts []string, want string) bool {
	for _, effort := range efforts {
		if strings.EqualFold(strings.TrimSpace(effort), want) {
			return true
		}
	}
	return false
}
