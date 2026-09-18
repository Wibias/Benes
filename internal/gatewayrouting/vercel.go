package gatewayrouting

import (
	"encoding/json"
	"fmt"
)

// ApplyVercelChat injects providerOptions.gateway for canonical Vercel AI Gateway
// Chat Completions. Empty policy leaves the body unchanged. Lookalike hosts are
// left untouched rather than receiving vendor fields.
func ApplyVercelChat(body []byte, endpoint string, policy Policy) ([]byte, error) {
	if policy.Empty() {
		return body, nil
	}
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	if !CanonicalVercel(endpoint) {
		return body, nil
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode chat body for gateway routing: %w", err)
	}
	if payload == nil {
		return nil, fmt.Errorf("chat body is not an object")
	}
	providerOptions, _ := payload["providerOptions"].(map[string]any)
	if providerOptions == nil {
		providerOptions = map[string]any{}
	}
	gateway, _ := providerOptions["gateway"].(map[string]any)
	if gateway == nil {
		gateway = map[string]any{}
	}
	view := SafeView(policy)
	for key, value := range view {
		gateway[key] = value
	}
	providerOptions["gateway"] = gateway
	payload["providerOptions"] = providerOptions
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode chat body with gateway routing: %w", err)
	}
	return encoded, nil
}
