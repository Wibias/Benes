package server

import (
	"context"
	"strings"
	"sync"

	"github.com/Wibias/Benes/internal/harnesspolicy"
)

// sidecarResolution is the actual runtime outcome of one sidecar modality for a
// single request. It stays separate from the configured policy: a request can be
// configured "on" and still resolve to request_native or unsupported depending
// on the selected provider/model.
type sidecarResolution string

const (
	sidecarResolutionUnknown        sidecarResolution = "unknown"
	sidecarResolutionRequestNative  sidecarResolution = "request_native"
	sidecarResolutionEnabled        sidecarResolution = "sidecar_enabled"
	sidecarResolutionPolicyDisabled sidecarResolution = "policy_disabled"
	sidecarResolutionUnsupported    sidecarResolution = "unsupported"
)

// sidecarFailure is a failure category, never an upstream message. Evidence must
// never carry request bodies, image payloads, credentials, or raw errors.
type sidecarFailure string

const (
	sidecarFailureNone      sidecarFailure = ""
	sidecarFailureTransform sidecarFailure = "transform"
)

// sidecarResolutionForDecision classifies a resolver decision. A proven native
// capability wins over the policy source because no sidecar runs in that case.
func sidecarResolutionForDecision(decision harnesspolicy.Decision) sidecarResolution {
	switch {
	case decision.Source == harnesspolicy.SourceUnsupported:
		return sidecarResolutionUnsupported
	case decision.Native && decision.Enabled:
		return sidecarResolutionRequestNative
	case decision.Enabled:
		return sidecarResolutionEnabled
	default:
		return sidecarResolutionPolicyDisabled
	}
}

// sidecarEvidenceIdentity carries an accepted Harness id, or the missing /
// rejected status on its own. A rejected selector is untrusted request metadata
// and is never persisted as identity.
type sidecarEvidenceIdentity struct {
	Status    harnessIdentityStatus `json:"status"`
	HarnessID string                `json:"harnessId,omitempty"`
}

type sidecarEvidenceDecision struct {
	Configured configuredHarnessSidecar `json:"configured"`
	Resolution sidecarResolution        `json:"resolution"`
	Ran        bool                     `json:"ran"`
	Failure    sidecarFailure           `json:"failure,omitempty"`
}

// sidecarPolicyEvidence is the immutable per-request snapshot. Every field is a
// closed enum, an allowlisted Harness id, or a stored policy source.
type sidecarPolicyEvidence struct {
	Identity  sidecarEvidenceIdentity `json:"identity"`
	WebSearch sidecarEvidenceDecision `json:"webSearch"`
	Vision    sidecarEvidenceDecision `json:"vision"`
}

// sidecarPolicyEvidenceRecorder collects sidecar evidence for one request. It is
// updated from several lifecycle points (request binding, policy resolution,
// pre-dispatch vision transform, terminal usage emission), possibly from
// different goroutines, and hands out value snapshots so a record that was
// already emitted can never be mutated by a later update.
type sidecarPolicyEvidenceRecorder struct {
	mu       sync.Mutex
	evidence sidecarPolicyEvidence
}

func newSidecarPolicyEvidenceRecorder(identity harnessRequestIdentity, configured configuredHarnessSidecarPolicy) *sidecarPolicyEvidenceRecorder {
	return &sidecarPolicyEvidenceRecorder{evidence: sidecarPolicyEvidence{
		Identity: sidecarIdentityEvidence(identity),
		WebSearch: sidecarEvidenceDecision{
			Configured: configured.WebSearch,
			Resolution: sidecarResolutionUnknown,
		},
		Vision: sidecarEvidenceDecision{
			Configured: configured.Vision,
			Resolution: sidecarResolutionUnknown,
		},
	}}
}

func sidecarIdentityEvidence(identity harnessRequestIdentity) sidecarEvidenceIdentity {
	status := identity.Status
	if status == "" {
		status = harnessIdentityMissing
	}
	if status != harnessIdentityAccepted {
		return sidecarEvidenceIdentity{Status: status}
	}
	return sidecarEvidenceIdentity{Status: status, HarnessID: strings.TrimSpace(identity.ID)}
}

// recordIdentity refreshes the request identity without dropping sidecar policy
// or execution evidence that was already recorded for the request.
func (recorder *sidecarPolicyEvidenceRecorder) recordIdentity(identity harnessRequestIdentity) {
	if recorder == nil {
		return
	}
	next := sidecarIdentityEvidence(identity)
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	recorder.evidence.Identity = next
}

// recordWebSearchDecision stores the resolution of the web-search policy. It is
// not proof that the web-search sidecar ran; markWebSearchRan does that at the
// execution point.
func (recorder *sidecarPolicyEvidenceRecorder) recordWebSearchDecision(decision harnesspolicy.Decision) {
	if recorder == nil {
		return
	}
	resolution := sidecarResolutionForDecision(decision)
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	recorder.evidence.WebSearch.Resolution = resolution
}

// recordVisionDecision stores the resolution of the vision policy. It is not
// proof that the vision sidecar ran.
func (recorder *sidecarPolicyEvidenceRecorder) recordVisionDecision(decision harnesspolicy.Decision) {
	if recorder == nil {
		return
	}
	resolution := sidecarResolutionForDecision(decision)
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	recorder.evidence.Vision.Resolution = resolution
}

func (recorder *sidecarPolicyEvidenceRecorder) markWebSearchRan() {
	if recorder == nil {
		return
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	recorder.evidence.WebSearch.Ran = true
}

func (recorder *sidecarPolicyEvidenceRecorder) markVisionRan() {
	if recorder == nil {
		return
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	recorder.evidence.Vision.Ran = true
}

func (recorder *sidecarPolicyEvidenceRecorder) recordVisionFailure(failure sidecarFailure) {
	if recorder == nil {
		return
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	recorder.evidence.Vision.Failure = failure
}

// snapshot returns an immutable copy. All evidence fields are values, so later
// recorder mutations cannot reach an emitted snapshot.
func (recorder *sidecarPolicyEvidenceRecorder) snapshot() sidecarPolicyEvidence {
	if recorder == nil {
		return sidecarPolicyEvidence{}
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	return recorder.evidence
}

// bindSidecarPolicyEvidence seeds the request-scoped recorder once, from the
// identity stamped at the request boundary and the policy configured for it.
func (diag *diagnosticsRecorder) bindSidecarPolicyEvidence(h *handler, ctx context.Context) {
	if diag == nil {
		return
	}
	diag.mu.Lock()
	bound := diag.sidecarPolicy != nil
	diag.mu.Unlock()
	if bound {
		return
	}
	identity := harnessIdentityFromContext(ctx)
	recorder := newSidecarPolicyEvidenceRecorder(identity, h.configuredHarnessSidecarPolicy(identity.ID))
	diag.mu.Lock()
	if diag.sidecarPolicy == nil {
		diag.sidecarPolicy = recorder
	}
	diag.mu.Unlock()
}

func (diag *diagnosticsRecorder) sidecarEvidence() *sidecarPolicyEvidenceRecorder {
	if diag == nil {
		return nil
	}
	diag.mu.Lock()
	defer diag.mu.Unlock()
	return diag.sidecarPolicy
}

// finalSidecarPolicyEvidence applies the late identity resolution and returns
// the terminal snapshot that the usage record carries.
func (diag *diagnosticsRecorder) finalSidecarPolicyEvidence(identity harnessRequestIdentity) *sidecarPolicyEvidence {
	recorder := diag.sidecarEvidence()
	if recorder == nil {
		return nil
	}
	recorder.recordIdentity(identity)
	snapshot := recorder.snapshot()
	return &snapshot
}

// sidecarPolicyEvidenceFor reaches the request-scoped recorder from a lifecycle
// point that only holds the request context. Every recorder method is nil-safe,
// so an unbound request records nothing instead of failing.
func sidecarPolicyEvidenceFor(ctx context.Context) *sidecarPolicyEvidenceRecorder {
	return diagnosticsRecorderFrom(ctx).sidecarEvidence()
}
