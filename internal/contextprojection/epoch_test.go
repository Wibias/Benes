package contextprojection

import (
	"testing"
)

func TestResolveEpochLatchesRequestedMode(t *testing.T) {
	dup, ok := ResolveEpoch(EpochInput{RequestedMode: ModeDuplicate})
	if !ok || dup != (Continuation{SchemaVersion: 1, Variant: VariantDuplicateV1, PolicyID: PolicyIDDuplicate, State: StateActive}) {
		t.Fatalf("duplicate=%+v ok=%v", dup, ok)
	}
	rec, ok := ResolveEpoch(EpochInput{RequestedMode: ModeRecovery})
	if !ok || rec.Variant != VariantRecoveryV1 || rec.PolicyID != PolicyIDRecovery || rec.State != StateActive {
		t.Fatalf("recovery=%+v", rec)
	}
	on, ok := ResolveEpoch(EpochInput{RequestedMode: ModeOn})
	if !ok || on != rec {
		t.Fatalf("on=%+v recovery=%+v", on, rec)
	}
	if _, ok := ResolveEpoch(EpochInput{RequestedMode: ModeOff}); ok {
		t.Fatal("off should not start an epoch")
	}
	if _, ok := ResolveEpoch(EpochInput{RequestedMode: ModeShadow}); ok {
		t.Fatal("shadow should not start an epoch")
	}
}

func TestActiveEpochIgnoresLaterModeChanges(t *testing.T) {
	previous := Continuation{SchemaVersion: 1, Variant: VariantDuplicateV1, PolicyID: PolicyIDDuplicate, State: StateActive}
	got, ok := ResolveEpoch(EpochInput{PreviousResponseID: "resp-1", Previous: &previous, RequestedMode: ModeOff})
	if !ok || got != previous {
		t.Fatalf("got=%+v", got)
	}
	got, ok = ResolveEpoch(EpochInput{PreviousResponseID: "resp-1", Previous: &previous, RequestedMode: ModeRecovery})
	if !ok || got != previous {
		t.Fatalf("got=%+v", got)
	}
}

func TestLegacyChainWithoutMetadataStaysDisabled(t *testing.T) {
	got, ok := ResolveEpoch(EpochInput{PreviousResponseID: "legacy-response", RequestedMode: ModeDuplicate})
	if !ok || got.State != StateDisabled || got.PolicyID != PolicyIDDuplicate {
		t.Fatalf("got=%+v ok=%v", got, ok)
	}
}

func TestUnknownStoredPolicyFailsOpenDisabled(t *testing.T) {
	unknown := Continuation{SchemaVersion: 1, Variant: VariantDuplicateV1, PolicyID: "duplicate-v1:unknown-policy", State: StateActive}
	got, ok := ResolveEpoch(EpochInput{PreviousResponseID: "resp-old", Previous: &unknown, RequestedMode: ModeDuplicate})
	if !ok || got.State != StateDisabled || got.PolicyID != unknown.PolicyID {
		t.Fatalf("got=%+v", got)
	}
}

func TestEmergencyDisableFlagSuspendsActiveEpoch(t *testing.T) {
	previous := Continuation{SchemaVersion: 1, Variant: VariantDuplicateV1, PolicyID: PolicyIDDuplicate, State: StateActive}
	got, ok := ResolveEpoch(EpochInput{
		PreviousResponseID: "resp-1",
		Previous:           &previous,
		RequestedMode:      ModeDuplicate,
		EmergencyDisable:   true,
	})
	want := previous
	want.State = StateDisabled
	if !ok || got != want {
		t.Fatalf("got=%+v", got)
	}
}

func TestEmergencyDisableEnvSuspendsActiveEpoch(t *testing.T) {
	t.Setenv(EmergencyDisableEnv, "1")
	previous := Continuation{SchemaVersion: 1, Variant: VariantDuplicateV1, PolicyID: PolicyIDDuplicate, State: StateActive}
	got, ok := ResolveEpoch(EpochInput{PreviousResponseID: "resp-live", Previous: &previous, RequestedMode: ModeDuplicate})
	if !ok || got.State != StateDisabled {
		t.Fatalf("got=%+v", got)
	}
}

func TestModeChangesStayLatchedWithoutEmergencyEnv(t *testing.T) {
	t.Setenv(EmergencyDisableEnv, "")
	previous := Continuation{SchemaVersion: 1, Variant: VariantDuplicateV1, PolicyID: PolicyIDDuplicate, State: StateActive}
	got, ok := ResolveEpoch(EpochInput{PreviousResponseID: "resp-live", Previous: &previous, RequestedMode: ModeOff})
	if !ok || got != previous {
		t.Fatalf("got=%+v", got)
	}
}

func TestNormalizeContinuationRejectsUnknownKeys(t *testing.T) {
	active := Continuation{SchemaVersion: 1, Variant: VariantDuplicateV1, PolicyID: PolicyIDDuplicate, State: StateActive}
	if got, ok := NormalizeContinuation(active); !ok || got != active {
		t.Fatalf("got=%+v ok=%v", got, ok)
	}
	if _, ok := NormalizeContinuation(map[string]any{
		"schemaVersion": 1,
		"variant":       "duplicate-v1",
		"policyId":      PolicyIDDuplicate,
		"state":         "active",
		"ref":           "ctx_secret",
	}); ok {
		t.Fatal("extra keys must be rejected")
	}
	if _, ok := NormalizeContinuation(map[string]any{
		"schemaVersion": 2,
		"variant":       "duplicate-v1",
		"policyId":      PolicyIDDuplicate,
		"state":         "active",
	}); ok {
		t.Fatal("schemaVersion 2 must be rejected")
	}
	if _, ok := NormalizeContinuation(map[string]any{
		"schemaVersion": 1,
		"variant":       "duplicate-v1",
		"policyId":      "",
		"state":         "active",
	}); ok {
		t.Fatal("empty policyId must be rejected")
	}
}
