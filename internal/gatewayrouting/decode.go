package gatewayrouting

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Wibias/Benes/internal/timeline"
)

func (s Settings) Empty() bool {
	if !s.Provider.Empty() {
		return false
	}
	for _, policy := range s.Models {
		if !policy.Empty() {
			return false
		}
	}
	return true
}

func (s Settings) Clone() Settings {
	out := Settings{Provider: clonePolicy(s.Provider)}
	if len(s.Models) == 0 {
		return out
	}
	out.Models = make(map[string]Policy, len(s.Models))
	for model, policy := range s.Models {
		out.Models[model] = clonePolicy(policy)
	}
	return out
}

func DecodePolicy(raw []byte) (Policy, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return Policy{}, nil
	}
	var probe map[string]json.RawMessage
	if json.Unmarshal(trimmed, &probe) != nil || probe == nil {
		return Policy{}, fmt.Errorf("gateway routing policy must be an object")
	}
	for key := range probe {
		switch key {
		case "only", "order", "sort":
		default:
			return Policy{}, fmt.Errorf("gateway routing field %q is not supported", key)
		}
	}
	var policy Policy
	if err := json.Unmarshal(trimmed, &policy); err != nil {
		return Policy{}, fmt.Errorf("gateway routing policy is invalid")
	}
	if err := policy.Validate(); err != nil {
		return Policy{}, err
	}
	return policy, nil
}

func DecodeModelPolicies(raw []byte) (map[string]Policy, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	var probe map[string]json.RawMessage
	if json.Unmarshal(trimmed, &probe) != nil || probe == nil {
		return nil, fmt.Errorf("model gateway routing must be an object")
	}
	out := make(map[string]Policy, len(probe))
	for model, policyRaw := range probe {
		if strings.TrimSpace(model) == "" || strings.TrimSpace(model) != model {
			return nil, fmt.Errorf("model gateway routing requires an exact model id")
		}
		policy, err := DecodePolicy(policyRaw)
		if err != nil {
			return nil, fmt.Errorf("model %q: %w", model, err)
		}
		out[model] = policy
	}
	return out, nil
}

func RecordRequested(ctx context.Context, policy Policy, applied bool) {
	view := SafeView(policy)
	if len(view) == 0 {
		return
	}
	payload, err := json.Marshal(map[string]any{
		"requested_gateway_routing": view,
		"applied":                   applied,
	})
	if err != nil {
		return
	}
	if tr := timeline.FromContext(ctx); tr != nil {
		tr.Mark(timeline.StagePreDispatch, timeline.SideLocal, timeline.MilestoneDispatch, true, string(payload))
	}
}
