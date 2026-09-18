package claude

import (
	"fmt"
	"strings"
)

const (
	FamilyOpus   = "opus"
	FamilySonnet = "sonnet"
	FamilyHaiku  = "haiku"
	FamilyFable  = "fable"
)

var FamilyKeys = []string{FamilyOpus, FamilySonnet, FamilyHaiku, FamilyFable}

type FamilyRoutes struct {
	Opus   string
	Sonnet string
	Haiku  string
	Fable  string
}

func (r *FamilyRoutes) Set(family, value string) {
	value = strings.TrimSpace(value)
	switch family {
	case FamilyOpus:
		r.Opus = value
	case FamilySonnet:
		r.Sonnet = value
	case FamilyHaiku:
		r.Haiku = value
	case FamilyFable:
		r.Fable = value
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func familyFromObject(raw any) FamilyRoutes {
	var out FamilyRoutes
	block, ok := raw.(map[string]any)
	if !ok || block == nil {
		return out
	}
	for _, key := range FamilyKeys {
		value, _ := block[key].(string)
		out.Set(key, value)
	}
	return out
}

func ReadFamilyRoutes(block map[string]any) FamilyRoutes {
	if block == nil {
		return FamilyRoutes{}
	}
	legacy := familyFromObject(block["modelMap"])
	canonical := familyFromObject(block["tierModels"])
	return FamilyRoutes{
		Opus:   firstNonEmpty(canonical.Opus, legacy.Opus),
		Sonnet: firstNonEmpty(canonical.Sonnet, legacy.Sonnet),
		Haiku:  firstNonEmpty(canonical.Haiku, legacy.Haiku),
		Fable:  firstNonEmpty(canonical.Fable, legacy.Fable),
	}
}

func ParseFamilyObject(raw any) (FamilyRoutes, error) {
	if raw == nil {
		return FamilyRoutes{}, nil
	}
	block, ok := raw.(map[string]any)
	if !ok {
		return FamilyRoutes{}, fmt.Errorf("must be an object")
	}
	var out FamilyRoutes
	for _, key := range FamilyKeys {
		value, exists := block[key]
		if !exists || value == nil {
			continue
		}
		s, ok := value.(string)
		if !ok {
			return FamilyRoutes{}, fmt.Errorf("%s must be a string", key)
		}
		out.Set(key, s)
	}
	return out, nil
}

func ProjectFamilyMap(routes FamilyRoutes) map[string]any {
	out := map[string]any{}
	if routes.Opus != "" {
		out[FamilyOpus] = routes.Opus
	}
	if routes.Sonnet != "" {
		out[FamilySonnet] = routes.Sonnet
	}
	if routes.Haiku != "" {
		out[FamilyHaiku] = routes.Haiku
	}
	if routes.Fable != "" {
		out[FamilyFable] = routes.Fable
	}
	return out
}

func WriteFamilyRoutes(block map[string]any, routes FamilyRoutes) {
	if block == nil {
		return
	}
	projected := ProjectFamilyMap(routes)
	if len(projected) == 0 {
		delete(block, "tierModels")
	} else {
		block["tierModels"] = projected
	}
	delete(block, "modelMap")
}

func ApplyFamilyRoutes(settings *CodeSettings, routes FamilyRoutes) {
	if settings == nil {
		return
	}
	settings.Opus = routes.Opus
	settings.Sonnet = routes.Sonnet
	settings.Haiku = routes.Haiku
	settings.Fable = routes.Fable
}
