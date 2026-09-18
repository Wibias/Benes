package contextprojection

import (
	"testing"
)

func TestPolicyIDsAreExact(t *testing.T) {
	if DuplicatePolicy().PolicyID != "duplicate-v1:dup16k:scan64m" {
		t.Fatalf("duplicate=%q", DuplicatePolicy().PolicyID)
	}
	if RecoveryPolicy().PolicyID != "recovery-v1:dup16k:large96k:head8k:tail8k:scan64m" {
		t.Fatalf("recovery=%q", RecoveryPolicy().PolicyID)
	}
	if ShadowPolicy().PolicyID != "shadow-v1:dup8k:large32k:head8k:tail8k:scan64m" {
		t.Fatalf("shadow=%q", ShadowPolicy().PolicyID)
	}
}

func TestLiveAndShadowThresholds(t *testing.T) {
	dup := DuplicatePolicy()
	rec := RecoveryPolicy()
	shadow := ShadowPolicy()
	if dup.DuplicateMinBytes != 16*1024 || dup.MaxScannedUTF8Bytes != 64*1024*1024 {
		t.Fatalf("duplicate thresholds: %+v", dup)
	}
	if rec.LargeMinBytes != 96*1024 || rec.PreviewHeadBytes != 8*1024 || rec.PreviewTailBytes != 8*1024 {
		t.Fatalf("recovery thresholds: %+v", rec)
	}
	if shadow.DuplicateMinBytes != 8*1024 || shadow.LargeMinBytes != 32*1024 {
		t.Fatalf("shadow thresholds: %+v", shadow)
	}
	if rec.Variant != VariantRecoveryV1 || dup.Variant != VariantDuplicateV1 || shadow.Variant != VariantRecoveryV1 {
		t.Fatalf("variants dup=%s rec=%s shadow=%s", dup.Variant, rec.Variant, shadow.Variant)
	}
}

func TestObserveShadowOverridesMetricsVariant(t *testing.T) {
	metrics := ObserveShadow(ctxOf(toolResult("shadow-sample", nil)), PlanOptions{})
	if metrics.Variant != MetricsShadow {
		t.Fatalf("variant=%q", metrics.Variant)
	}
}
