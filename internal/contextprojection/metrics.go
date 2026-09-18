package contextprojection

import (
	"encoding/json"
	"math"
)

var metricKeys = map[string]struct{}{
	"version":                        {},
	"variant":                        {},
	"candidates":                     {},
	"duplicateCandidates":            {},
	"duplicateProjected":             {},
	"largeCandidates":                {},
	"largeProjected":                 {},
	"originalBytes":                  {},
	"projectedBytes":                 {},
	"planningMs":                     {},
	"plannerScannedBytes":            {},
	"hashScannedBytes":               {},
	"recoveryCalls":                  {},
	"recoveryBytes":                  {},
	"recoveryMisses":                 {},
	"recoveryLimitHits":              {},
	"recoveryScanBytes":              {},
	"internalModelRounds":            {},
	"failOpenRestarts":               {},
	"receiptActiveTurns":             {},
	"receiptActiveTurnsWithRecovery": {},
	"suspensionReason":               {},
	"sidecarKind":                    {},
}

var metricsVariants = map[string]struct{}{
	string(MetricsShadow):    {},
	string(MetricsDuplicate): {},
	string(MetricsRecovery):  {},
}

var sidecarKinds = map[string]struct{}{
	string(SidecarWebSearch): {},
	string(SidecarImage):     {},
	string(SidecarVideo):     {},
	string(SidecarVision):    {},
}

var suspensionReasons = map[string]struct{}{
	string(ReasonAborted):           {},
	string(ReasonPlannerScanLimit):  {},
	string(ReasonInactiveEpoch):     {},
	string(ReasonNativePassthrough): {},
	string(ReasonStatefulAdapter):   {},
	string(ReasonToolChoice):        {},
	string(ReasonStructuredOutput):  {},
	string(ReasonSidecar):           {},
	string(ReasonCompaction):        {},
	string(ReasonToolNameCollision): {},
	string(ReasonToolBudget):        {},
	string(ReasonPlanner):           {},
	string(ReasonEmergencyDisabled): {},
}

var metricCounterKeys = []string{
	"candidates",
	"duplicateCandidates",
	"duplicateProjected",
	"largeCandidates",
	"largeProjected",
	"originalBytes",
	"projectedBytes",
	"plannerScannedBytes",
	"hashScannedBytes",
	"recoveryCalls",
	"recoveryBytes",
	"recoveryMisses",
	"recoveryLimitHits",
	"recoveryScanBytes",
	"internalModelRounds",
	"failOpenRestarts",
	"receiptActiveTurns",
	"receiptActiveTurnsWithRecovery",
}

func emptyMetrics(policy Policy) Metrics {
	variant := MetricsDuplicate
	if policy.Variant == VariantRecoveryV1 {
		variant = MetricsRecovery
	}
	return Metrics{Version: 1, Variant: variant}
}

func intField(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		if typed < 0 {
			return 0, false
		}
		return typed, true
	case int64:
		if typed < 0 || typed > math.MaxInt {
			return 0, false
		}
		return int(typed), true
	case float64:
		if typed < 0 || typed != math.Trunc(typed) || typed > math.MaxInt {
			return 0, false
		}
		return int(typed), true
	case json.Number:
		n, err := typed.Int64()
		if err != nil || n < 0 || n > math.MaxInt {
			return 0, false
		}
		return int(n), true
	default:
		return 0, false
	}
}

func NormalizeMetrics(raw any) (Metrics, bool) {
	value, ok := asObject(raw)
	if !ok {
		return Metrics{}, false
	}
	for key := range value {
		if _, known := metricKeys[key]; !known {
			return Metrics{}, false
		}
	}
	if version, ok := intField(value["version"]); !ok || version != 1 {
		return Metrics{}, false
	}
	variant, _ := value["variant"].(string)
	if _, ok := metricsVariants[variant]; !ok {
		return Metrics{}, false
	}
	planningMs, ok := floatField(value["planningMs"])
	if !ok || planningMs < 0 || math.IsInf(planningMs, 0) || math.IsNaN(planningMs) {
		return Metrics{}, false
	}
	out := Metrics{Version: 1, Variant: MetricsVariant(variant), PlanningMs: planningMs}
	counters := []*int{
		&out.Candidates,
		&out.DuplicateCandidates,
		&out.DuplicateProjected,
		&out.LargeCandidates,
		&out.LargeProjected,
		&out.OriginalBytes,
		&out.ProjectedBytes,
		&out.PlannerScannedBytes,
		&out.HashScannedBytes,
		&out.RecoveryCalls,
		&out.RecoveryBytes,
		&out.RecoveryMisses,
		&out.RecoveryLimitHits,
		&out.RecoveryScanBytes,
		&out.InternalModelRounds,
		&out.FailOpenRestarts,
		&out.ReceiptActiveTurns,
		&out.ReceiptActiveTurnsWithRecovery,
	}
	for i, key := range metricCounterKeys {
		n, ok := intField(value[key])
		if !ok {
			return Metrics{}, false
		}
		*counters[i] = n
	}
	if reason, exists := value["suspensionReason"]; exists {
		text, _ := reason.(string)
		if _, ok := suspensionReasons[text]; !ok {
			return Metrics{}, false
		}
		out.SuspensionReason = SuspensionReason(text)
	}
	if kind, exists := value["sidecarKind"]; exists {
		text, _ := kind.(string)
		if _, ok := sidecarKinds[text]; !ok {
			return Metrics{}, false
		}
		out.SidecarKind = SidecarKind(text)
	}
	return out, true
}

func floatField(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case json.Number:
		n, err := typed.Float64()
		if err != nil {
			return 0, false
		}
		return n, true
	default:
		return 0, false
	}
}

func asObject(raw any) (map[string]any, bool) {
	switch typed := raw.(type) {
	case map[string]any:
		return typed, true
	case Metrics:
		encoded, err := json.Marshal(typed)
		if err != nil {
			return nil, false
		}
		return asObject(encoded)
	case *Metrics:
		if typed == nil {
			return nil, false
		}
		return asObject(*typed)
	case []byte:
		var value map[string]any
		if err := json.Unmarshal(typed, &value); err != nil || value == nil {
			return nil, false
		}
		return value, true
	case string:
		return asObject([]byte(typed))
	case json.RawMessage:
		return asObject([]byte(typed))
	default:
		return nil, false
	}
}
