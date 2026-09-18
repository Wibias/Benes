package zcode

import (
	"encoding/json"
	"fmt"
)

const providerID = "benes"

func MergeProvider(existing []byte, fragment map[string]any) ([]byte, error) {
	if fragment == nil {
		return nil, fmt.Errorf("ZCode provider fragment is required")
	}
	root := map[string]json.RawMessage{}
	if len(existing) > 0 {
		if err := json.Unmarshal(existing, &root); err != nil {
			return nil, fmt.Errorf("ZCode config is unparseable")
		}
	}
	providers := map[string]json.RawMessage{}
	if raw, ok := root["provider"]; ok {
		if err := json.Unmarshal(raw, &providers); err != nil {
			return nil, fmt.Errorf("ZCode provider map is unparseable")
		}
	}
	owned, err := mergeOwnedFragment(providers[providerID], fragment)
	if err != nil {
		return nil, err
	}
	providers[providerID] = owned
	providerRaw, err := json.Marshal(providers)
	if err != nil {
		return nil, err
	}
	root["provider"] = providerRaw
	return json.Marshal(root)
}

func RemoveProvider(existing []byte) ([]byte, error) {
	root := map[string]json.RawMessage{}
	if len(existing) > 0 {
		if err := json.Unmarshal(existing, &root); err != nil {
			return nil, fmt.Errorf("ZCode config is unparseable")
		}
	}
	raw, ok := root["provider"]
	if !ok {
		return json.Marshal(root)
	}
	providers := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &providers); err != nil {
		return nil, fmt.Errorf("ZCode provider map is unparseable")
	}
	delete(providers, providerID)
	providerRaw, err := json.Marshal(providers)
	if err != nil {
		return nil, err
	}
	root["provider"] = providerRaw
	return json.Marshal(root)
}

func mergeOwnedFragment(existing json.RawMessage, fragment map[string]any) (json.RawMessage, error) {
	out := map[string]any{}
	for key, value := range fragment {
		if key != "models" {
			out[key] = value
		}
	}
	var prev map[string]any
	if len(existing) > 0 {
		if err := json.Unmarshal(existing, &prev); err != nil || prev == nil {
			return nil, fmt.Errorf("ZCode provider.benes is unparseable")
		}
		if _, ok := prev["options"]; ok {
			return nil, fmt.Errorf("ZCode provider.benes has a second connection envelope")
		}
	}
	fragModels, hasModels := fragment["models"]
	if hasModels {
		if prev != nil {
			if raw, ok := prev["models"]; ok && raw != nil {
				if _, ok := raw.(map[string]any); !ok {
					return nil, fmt.Errorf("ZCode provider.benes models are unparseable")
				}
			}
		}
		next, err := mergeOwnedModels(asObject(prev, "models"), fragModels)
		if err != nil {
			return nil, err
		}
		out["models"] = next
	} else if prev != nil {
		if models, ok := prev["models"]; ok {
			out["models"] = models
		}
	}
	return json.Marshal(out)
}

func mergeOwnedModels(prev map[string]any, fragment any) (map[string]any, error) {
	frag, ok := fragment.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("ZCode provider.benes models are unparseable")
	}
	out := map[string]any{}
	for id, raw := range frag {
		entry, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("ZCode provider.benes model %s is unparseable", id)
		}
		next := map[string]any{}
		for key, value := range entry {
			next[key] = value
		}
		merged, err := preserveZCodeModelMetadata(asObject(prev, id), next)
		if err != nil {
			return nil, err
		}
		out[id] = merged
	}
	return out, nil
}

func preserveZCodeModelMetadata(prev, next map[string]any) (map[string]any, error) {
	if prev == nil {
		return next, nil
	}
	if raw, ok := prev["reasoning"]; ok {
		if _, ok := raw.(map[string]any); !ok {
			return nil, fmt.Errorf("ZCode model reasoning is unparseable")
		}
		next["reasoning"] = raw
	}
	limit, err := preserveZCodeLimit(prev, hasContextWindow(next))
	if err != nil {
		return nil, err
	}
	if limit != nil {
		next["limit"] = limit
	}
	return next, nil
}

func preserveZCodeLimit(prev map[string]any, benesOwnsContext bool) (map[string]any, error) {
	raw, ok := prev["limit"]
	if !ok {
		return nil, nil
	}
	limit, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("ZCode model limit is unparseable")
	}
	out := map[string]any{}
	if output, ok := limit["output"]; ok {
		if !isJSONNumber(output) {
			return nil, fmt.Errorf("ZCode model limit.output is unparseable")
		}
		out["output"] = output
	}
	if !benesOwnsContext {
		if contextWindow, ok := limit["context"]; ok {
			if !isJSONNumber(contextWindow) {
				return nil, fmt.Errorf("ZCode model limit.context is unparseable")
			}
			out["context"] = contextWindow
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func hasContextWindow(entry map[string]any) bool {
	_, ok := entry["contextWindow"]
	return ok
}

func asObject(root map[string]any, key string) map[string]any {
	if root == nil {
		return nil
	}
	obj, _ := root[key].(map[string]any)
	return obj
}

func isJSONNumber(value any) bool {
	switch value.(type) {
	case float64, json.Number, int, int64:
		return true
	default:
		return false
	}
}
