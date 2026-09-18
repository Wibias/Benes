package export

import (
	"encoding/json"
	"fmt"
	"strings"
)

const ConfigContentEnv = "OPENCODE_CONFIG_CONTENT"

func MergeOpencodeRuntime(inherited string, providerBlock any) (map[string]any, error) {
	if strings.TrimSpace(inherited) == "" {
		return map[string]any{
			"$schema": schemaURL,
			"provider": map[string]any{
				ProviderID: providerBlock,
			},
		}, nil
	}
	var parsed any
	if err := json.Unmarshal([]byte(inherited), &parsed); err != nil {
		return nil, fmt.Errorf("OPENCODE_CONFIG_CONTENT is not valid JSON")
	}
	root, ok := parsed.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("OPENCODE_CONFIG_CONTENT must be a JSON object")
	}
	providers := map[string]any{}
	if raw, exists := root["provider"]; exists {
		existing, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("OPENCODE_CONFIG_CONTENT provider must be a JSON object when present")
		}
		for key, value := range existing {
			providers[key] = value
		}
	}
	providers[ProviderID] = providerBlock
	schema := schemaURL
	if existing, ok := root["$schema"].(string); ok && strings.TrimSpace(existing) != "" {
		schema = existing
	}
	out := map[string]any{}
	for key, value := range root {
		out[key] = value
	}
	out["$schema"] = schema
	out["provider"] = providers
	return out, nil
}

func OpencodeProviderBlock(doc any) (any, error) {
	root, ok := doc.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("opencode document must be an object")
	}
	providers, ok := root["provider"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("opencode document is missing provider")
	}
	block, ok := providers[ProviderID]
	if !ok {
		return nil, fmt.Errorf("opencode document is missing provider.%s", ProviderID)
	}
	return block, nil
}
