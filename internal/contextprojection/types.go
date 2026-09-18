package contextprojection

import (
	"context"
)

type PolicyVariant string

const (
	VariantDuplicateV1 PolicyVariant = "duplicate-v1"
	VariantRecoveryV1  PolicyVariant = "recovery-v1"
)

type MetricsVariant string

const (
	MetricsShadow    MetricsVariant = "shadow"
	MetricsDuplicate MetricsVariant = "duplicate"
	MetricsRecovery  MetricsVariant = "recovery"
)

type ContinuationState string

const (
	StateActive   ContinuationState = "active"
	StateDisabled ContinuationState = "disabled"
)

type Mode string

const (
	ModeOff       Mode = "off"
	ModeShadow    Mode = "shadow"
	ModeDuplicate Mode = "duplicate"
	ModeRecovery  Mode = "recovery"
	ModeOn        Mode = "on"
)

type EligibilityState string

const (
	EligibilityEligible  EligibilityState = "eligible"
	EligibilitySuspended EligibilityState = "suspended"
)

type ReplacementKind string

const (
	KindDuplicate ReplacementKind = "duplicate"
	KindLarge     ReplacementKind = "large"
)

type SuspensionReason string

const (
	ReasonAborted           SuspensionReason = "aborted"
	ReasonPlannerScanLimit  SuspensionReason = "planner_scan_limit"
	ReasonInactiveEpoch     SuspensionReason = "inactive_epoch"
	ReasonNativePassthrough SuspensionReason = "native_passthrough"
	ReasonStatefulAdapter   SuspensionReason = "stateful_adapter"
	ReasonToolChoice        SuspensionReason = "tool_choice"
	ReasonStructuredOutput  SuspensionReason = "structured_output"
	ReasonSidecar           SuspensionReason = "sidecar"
	ReasonCompaction        SuspensionReason = "compaction"
	ReasonToolNameCollision SuspensionReason = "tool_name_collision"
	ReasonToolBudget        SuspensionReason = "tool_budget"
	ReasonPlanner           SuspensionReason = "planner"
	ReasonEmergencyDisabled SuspensionReason = "emergency_disabled"
)

type SidecarKind string

const (
	SidecarWebSearch SidecarKind = "web_search"
	SidecarImage     SidecarKind = "image"
	SidecarVideo     SidecarKind = "video"
	SidecarVision    SidecarKind = "vision"
)

type Policy struct {
	SchemaVersion       int
	Variant             PolicyVariant
	PolicyID            string
	DuplicateMinBytes   int
	LargeMinBytes       int
	PreviewHeadBytes    int
	PreviewTailBytes    int
	MaxScannedUTF8Bytes int
}

type Continuation struct {
	SchemaVersion int               `json:"schemaVersion"`
	Variant       PolicyVariant     `json:"variant"`
	PolicyID      string            `json:"policyId"`
	State         ContinuationState `json:"state"`
}

type Identity struct {
	ToolCallID        string
	ToolNamespace     string
	ToolName          string
	OccurrenceOrdinal int
}

type Artifact struct {
	Ref            string
	Identity       Identity
	MessageIndex   int
	Content        string
	UTF8ByteLength int
	LineCount      int
	IsError        bool
}

type Replacement struct {
	MessageIndex     int
	Kind             ReplacementKind
	Ref              string
	ProjectedContent string
	OriginalBytes    int
	ProjectedBytes   int
}

type Eligibility struct {
	State  EligibilityState
	Reason SuspensionReason
}

type Metrics struct {
	Version                        int              `json:"version"`
	Variant                        MetricsVariant   `json:"variant"`
	Candidates                     int              `json:"candidates"`
	DuplicateCandidates            int              `json:"duplicateCandidates"`
	DuplicateProjected             int              `json:"duplicateProjected"`
	LargeCandidates                int              `json:"largeCandidates"`
	LargeProjected                 int              `json:"largeProjected"`
	OriginalBytes                  int              `json:"originalBytes"`
	ProjectedBytes                 int              `json:"projectedBytes"`
	PlanningMs                     float64          `json:"planningMs"`
	PlannerScannedBytes            int              `json:"plannerScannedBytes"`
	HashScannedBytes               int              `json:"hashScannedBytes"`
	RecoveryCalls                  int              `json:"recoveryCalls"`
	RecoveryBytes                  int              `json:"recoveryBytes"`
	RecoveryMisses                 int              `json:"recoveryMisses"`
	RecoveryLimitHits              int              `json:"recoveryLimitHits"`
	RecoveryScanBytes              int              `json:"recoveryScanBytes"`
	InternalModelRounds            int              `json:"internalModelRounds"`
	FailOpenRestarts               int              `json:"failOpenRestarts"`
	ReceiptActiveTurns             int              `json:"receiptActiveTurns"`
	ReceiptActiveTurnsWithRecovery int              `json:"receiptActiveTurnsWithRecovery"`
	SuspensionReason               SuspensionReason `json:"suspensionReason,omitempty"`
	SidecarKind                    SidecarKind      `json:"sidecarKind,omitempty"`
}

type PlanResult struct {
	Registry     *Registry
	Replacements []Replacement
	Metrics      Metrics
	Eligibility  Eligibility
}

type PlanOptions struct {
	Ctx         context.Context
	HashContent func(content string) string
}

type EpochInput struct {
	PreviousResponseID string
	Previous           *Continuation
	RequestedMode      Mode
	EmergencyDisable   bool
}
