package contextprojection

import (
	"encoding/json"
	"os"
	"strings"
)

const EmergencyDisableEnv = "BENES_CONTEXT_PROJECTION_EMERGENCY_DISABLE"

var continuationKeys = map[string]struct{}{
	"schemaVersion": {},
	"variant":       {},
	"policyId":      {},
	"state":         {},
}

func supportedPolicyID(policyID string) bool {
	return policyID == PolicyIDDuplicate || policyID == PolicyIDRecovery
}

func NormalizeContinuation(raw any) (Continuation, bool) {
	switch typed := raw.(type) {
	case Continuation:
		return normalizeContinuationMap(continuationObject(typed))
	case *Continuation:
		if typed == nil {
			return Continuation{}, false
		}
		return normalizeContinuationMap(continuationObject(*typed))
	default:
		value, ok := continuationAsObject(raw)
		if !ok {
			return Continuation{}, false
		}
		return normalizeContinuationMap(value)
	}
}

func continuationObject(value Continuation) map[string]any {
	return map[string]any{
		"schemaVersion": value.SchemaVersion,
		"variant":       string(value.Variant),
		"policyId":      value.PolicyID,
		"state":         string(value.State),
	}
}

func continuationAsObject(raw any) (map[string]any, bool) {
	switch typed := raw.(type) {
	case map[string]any:
		return typed, true
	case []byte:
		var value map[string]any
		if err := json.Unmarshal(typed, &value); err != nil || value == nil {
			return nil, false
		}
		return value, true
	case string:
		return continuationAsObject([]byte(typed))
	case json.RawMessage:
		return continuationAsObject([]byte(typed))
	default:
		return nil, false
	}
}

func normalizeContinuationMap(value map[string]any) (Continuation, bool) {
	if value == nil {
		return Continuation{}, false
	}
	for key := range value {
		if _, known := continuationKeys[key]; !known {
			return Continuation{}, false
		}
	}
	schemaVersion, ok := intField(value["schemaVersion"])
	if !ok || schemaVersion != 1 {
		return Continuation{}, false
	}
	variant, _ := value["variant"].(string)
	if variant != string(VariantDuplicateV1) && variant != string(VariantRecoveryV1) {
		return Continuation{}, false
	}
	policyID, _ := value["policyId"].(string)
	if strings.TrimSpace(policyID) == "" || len(policyID) > 256 {
		return Continuation{}, false
	}
	state, _ := value["state"].(string)
	if state != string(StateActive) && state != string(StateDisabled) {
		return Continuation{}, false
	}
	return Continuation{
		SchemaVersion: 1,
		Variant:       PolicyVariant(variant),
		PolicyID:      policyID,
		State:         ContinuationState(state),
	}, true
}

func requestedEpoch(mode Mode) (Continuation, bool) {
	switch mode {
	case ModeDuplicate:
		return Continuation{
			SchemaVersion: 1,
			Variant:       VariantDuplicateV1,
			PolicyID:      PolicyIDDuplicate,
			State:         StateActive,
		}, true
	case ModeRecovery, ModeOn:
		return Continuation{
			SchemaVersion: 1,
			Variant:       VariantRecoveryV1,
			PolicyID:      PolicyIDRecovery,
			State:         StateActive,
		}, true
	default:
		return Continuation{}, false
	}
}

func emergencyDisableRequested(explicit bool) bool {
	return explicit || os.Getenv(EmergencyDisableEnv) == "1"
}

// EmergencyDisabled is the process-wide kill switch for provider-visible projection.
func EmergencyDisabled() bool {
	return emergencyDisableRequested(false)
}

func ResolveEpoch(input EpochInput) (Continuation, bool) {
	emergencyDisable := emergencyDisableRequested(input.EmergencyDisable)
	if input.Previous != nil {
		previous, ok := NormalizeContinuation(*input.Previous)
		if ok {
			if emergencyDisable || previous.State == StateDisabled || !supportedPolicyID(previous.PolicyID) {
				previous.State = StateDisabled
			}
			return previous, true
		}
	}

	requested, ok := requestedEpoch(input.RequestedMode)
	if !ok {
		return Continuation{}, false
	}
	if input.PreviousResponseID != "" || emergencyDisable {
		requested.State = StateDisabled
	}
	return requested, true
}
