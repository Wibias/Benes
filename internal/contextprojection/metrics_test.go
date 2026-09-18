package contextprojection

import (
	"encoding/json"
	"testing"
)

func TestNormalizeMetricsRejectsRefsAndUnknownKeys(t *testing.T) {
	raw := map[string]any{
		"version":                        1,
		"variant":                        "duplicate",
		"candidates":                     1,
		"duplicateCandidates":            1,
		"duplicateProjected":             1,
		"largeCandidates":                0,
		"largeProjected":                 0,
		"originalBytes":                  10,
		"projectedBytes":                 4,
		"planningMs":                     1.5,
		"plannerScannedBytes":            10,
		"hashScannedBytes":               10,
		"recoveryCalls":                  0,
		"recoveryBytes":                  0,
		"recoveryMisses":                 0,
		"recoveryLimitHits":              0,
		"recoveryScanBytes":              0,
		"internalModelRounds":            0,
		"failOpenRestarts":               0,
		"receiptActiveTurns":             0,
		"receiptActiveTurnsWithRecovery": 0,
		"ref":                            "ctx_secret",
	}
	if _, ok := NormalizeMetrics(raw); ok {
		t.Fatal("metrics must reject ref/content keys")
	}
	delete(raw, "ref")
	got, ok := NormalizeMetrics(raw)
	if !ok || got.Variant != MetricsDuplicate || got.DuplicateProjected != 1 {
		t.Fatalf("got=%+v ok=%v", got, ok)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var asMap map[string]any
	if err := json.Unmarshal(encoded, &asMap); err != nil {
		t.Fatal(err)
	}
	if _, exists := asMap["ref"]; exists {
		t.Fatal("encoded metrics included a ref")
	}
}

func TestNormalizeMetricsRejectsNegativeCounters(t *testing.T) {
	raw := map[string]any{
		"version": 1, "variant": "recovery", "candidates": -1,
		"duplicateCandidates": 0, "duplicateProjected": 0, "largeCandidates": 0, "largeProjected": 0,
		"originalBytes": 0, "projectedBytes": 0, "planningMs": 0, "plannerScannedBytes": 0, "hashScannedBytes": 0,
		"recoveryCalls": 0, "recoveryBytes": 0, "recoveryMisses": 0, "recoveryLimitHits": 0, "recoveryScanBytes": 0,
		"internalModelRounds": 0, "failOpenRestarts": 0, "receiptActiveTurns": 0, "receiptActiveTurnsWithRecovery": 0,
	}
	if _, ok := NormalizeMetrics(raw); ok {
		t.Fatal("negative counters must be rejected")
	}
}
