package server

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Wibias/Benes/internal/catalog"
	"github.com/Wibias/Benes/internal/harnesspolicy"
	"github.com/Wibias/Benes/internal/protocol"
	providercontract "github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/sidecar"
	"github.com/Wibias/Benes/internal/sidecar/vision"
)

type visionPreDispatchError struct {
	Reason string
	Err    error
}

func (e *visionPreDispatchError) Error() string {
	if e == nil {
		return "vision pre-dispatch failed"
	}
	reason := strings.TrimSpace(e.Reason)
	if reason == "" {
		reason = "failed"
	}
	if e.Err != nil {
		return fmt.Sprintf("vision pre-dispatch %s: %v", reason, e.Err)
	}
	return "vision pre-dispatch " + reason
}

func (e *visionPreDispatchError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

type visionPolicyProvider struct {
	handler    *handler
	inner      Provider
	providerID string
	modelID    string
}

func (h *handler) withVisionPolicyProvider(inner Provider, providerID, modelID string) Provider {
	if h == nil || inner == nil {
		return inner
	}
	base := &visionPolicyProvider{
		handler:    h,
		inner:      inner,
		providerID: strings.TrimSpace(providerID),
		modelID:    strings.TrimSpace(modelID),
	}
	return preserveVisionProviderCapabilities(base, inner)
}

func preserveVisionProviderCapabilities(base *visionPolicyProvider, inner Provider) Provider {
	forwarder, hasForwarder := inner.(nativeCodexForwarder)
	compactor, hasCompactor := inner.(nativeCompactor)
	imageRelay, hasImageRelay := inner.(providercontract.ImageRelay)
	nativeWebSearch, hasNativeWebSearch := inner.(providercontract.NativeHostedWebSearch)

	mask := 0
	if hasForwarder {
		mask |= 1
	}
	if hasCompactor {
		mask |= 2
	}
	if hasImageRelay {
		mask |= 4
	}
	if hasNativeWebSearch {
		mask |= 8
	}

	// Optional provider capabilities are discovered with type assertions. Preserve
	// exactly the interfaces implemented by the wrapped provider; advertising an
	// unsupported capability here would be just as incorrect as hiding one.
	switch mask {
	case 0:
		return base
	case 1:
		return struct {
			*visionPolicyProvider
			nativeCodexForwarder
		}{base, forwarder}
	case 2:
		return struct {
			*visionPolicyProvider
			nativeCompactor
		}{base, compactor}
	case 3:
		return struct {
			*visionPolicyProvider
			nativeCodexForwarder
			nativeCompactor
		}{base, forwarder, compactor}
	case 4:
		return struct {
			*visionPolicyProvider
			providercontract.ImageRelay
		}{base, imageRelay}
	case 5:
		return struct {
			*visionPolicyProvider
			nativeCodexForwarder
			providercontract.ImageRelay
		}{base, forwarder, imageRelay}
	case 6:
		return struct {
			*visionPolicyProvider
			nativeCompactor
			providercontract.ImageRelay
		}{base, compactor, imageRelay}
	case 7:
		return struct {
			*visionPolicyProvider
			nativeCodexForwarder
			nativeCompactor
			providercontract.ImageRelay
		}{base, forwarder, compactor, imageRelay}
	case 8:
		return struct {
			*visionPolicyProvider
			providercontract.NativeHostedWebSearch
		}{base, nativeWebSearch}
	case 9:
		return struct {
			*visionPolicyProvider
			nativeCodexForwarder
			providercontract.NativeHostedWebSearch
		}{base, forwarder, nativeWebSearch}
	case 10:
		return struct {
			*visionPolicyProvider
			nativeCompactor
			providercontract.NativeHostedWebSearch
		}{base, compactor, nativeWebSearch}
	case 11:
		return struct {
			*visionPolicyProvider
			nativeCodexForwarder
			nativeCompactor
			providercontract.NativeHostedWebSearch
		}{base, forwarder, compactor, nativeWebSearch}
	case 12:
		return struct {
			*visionPolicyProvider
			providercontract.ImageRelay
			providercontract.NativeHostedWebSearch
		}{base, imageRelay, nativeWebSearch}
	case 13:
		return struct {
			*visionPolicyProvider
			nativeCodexForwarder
			providercontract.ImageRelay
			providercontract.NativeHostedWebSearch
		}{base, forwarder, imageRelay, nativeWebSearch}
	case 14:
		return struct {
			*visionPolicyProvider
			nativeCompactor
			providercontract.ImageRelay
			providercontract.NativeHostedWebSearch
		}{base, compactor, imageRelay, nativeWebSearch}
	case 15:
		return struct {
			*visionPolicyProvider
			nativeCodexForwarder
			nativeCompactor
			providercontract.ImageRelay
			providercontract.NativeHostedWebSearch
		}{base, forwarder, compactor, imageRelay, nativeWebSearch}
	default:
		return base
	}
}

func (p *visionPolicyProvider) Open(ctx context.Context, dispatch providercontract.DispatchRequest) (EventStream, error) {
	prepared, err := p.prepare(ctx, dispatch)
	if err != nil {
		return nil, err
	}
	return p.inner.Open(ctx, prepared)
}

func (p *visionPolicyProvider) OpenCommitted(ctx context.Context, dispatch providercontract.DispatchRequest) (EventStream, error) {
	prepared, err := p.prepare(ctx, dispatch)
	if err != nil {
		return nil, err
	}
	if committed, ok := p.inner.(interface {
		OpenCommitted(context.Context, providercontract.DispatchRequest) (EventStream, error)
	}); ok {
		return committed.OpenCommitted(ctx, prepared)
	}
	return p.inner.Open(ctx, prepared)
}

func (p *visionPolicyProvider) Protocol() string {
	if p == nil || p.inner == nil {
		return ""
	}
	if reporter, ok := p.inner.(interface{ Protocol() string }); ok {
		return strings.TrimSpace(reporter.Protocol())
	}
	return ""
}

func (p *visionPolicyProvider) prepare(ctx context.Context, dispatch providercontract.DispatchRequest) (providercontract.DispatchRequest, error) {
	if p == nil || p.handler == nil || p.inner == nil || !parsedRequestHasImages(dispatch.Parsed) {
		return dispatch, nil
	}

	// Only proven text-only candidates need Benes vision mediation. Proven native
	// vision and unknown capability both receive the immutable original request.
	if p.handler.visionCapability(p.providerID, p.modelID) != catalog.CapabilityFalse {
		return dispatch, nil
	}

	candidate, candidateErr := p.handler.resolveVisionSidecarCandidate()
	sidecarProvider := Provider(nil)
	if candidateErr == nil {
		sidecarProvider = p.handler.providers[candidate.ProviderID]
		if sidecarProvider == nil {
			candidateErr = sidecar.ErrUnsupportedBackend
		}
	}
	decision, decisionErr := p.handler.resolveVisionActivation(ctx, candidateErr == nil)
	if decisionErr != nil {
		return dispatch, &visionPreDispatchError{Reason: "policy resolution failed", Err: decisionErr}
	}
	if !decision.Enabled {
		if decision.Source == harnesspolicy.SourceUnsupported {
			if candidateErr == nil {
				candidateErr = sidecar.ErrUnsupportedBackend
			}
			return dispatch, &visionPreDispatchError{Reason: "sidecar is unsupported", Err: candidateErr}
		}
		return dispatch, &visionPreDispatchError{Reason: "sidecar is disabled"}
	}
	if candidateErr != nil || sidecarProvider == nil {
		return dispatch, &visionPreDispatchError{Reason: "sidecar selection failed", Err: candidateErr}
	}

	maxImages, maxImageBytes, timeout := p.handler.visionSidecarLimits()
	client := vision.New(sidecarProvider, maxImages, maxImageBytes, vision.DefaultMaxDescription)
	transformed, err := vision.TransformRequest(ctx, dispatch.Parsed, candidate, client, vision.TransformOptions{
		MaxImages:      maxImages,
		MaxImageBytes:  maxImageBytes,
		MaxDescription: vision.DefaultMaxDescription,
		Timeout:        timeout,
	})
	if err != nil {
		// The transform executed and failed. Record the category only; a raw
		// upstream message can carry request or credential detail.
		evidence := sidecarPolicyEvidenceFor(ctx)
		evidence.markVisionRan()
		evidence.recordVisionFailure(sidecarFailureTransform)
		return dispatch, &visionPreDispatchError{Reason: "transform failed", Err: err}
	}
	sidecarPolicyEvidenceFor(ctx).markVisionRan()

	// ParsedRequest is the canonical semantic source for migrated providers. Drop
	// Raw after a transform so an alternate serializer cannot replay stale image
	// bytes and bypass the transformed structured context.
	transformed.Raw = nil
	prepared := providercontract.CloneDispatch(dispatch)
	prepared.Parsed = transformed
	return prepared, nil
}

func parsedRequestHasImages(request protocol.ParsedRequest) bool {
	for _, message := range request.Context.Messages {
		for _, part := range message.Content {
			if part.Type == protocol.ContentImage {
				return true
			}
		}
	}
	return false
}

func (h *handler) visionCapability(providerID, modelID string) catalog.CapabilityState {
	if h == nil {
		return catalog.CapabilityUnknown
	}
	providerID = strings.TrimSpace(providerID)
	modelID = strings.TrimSpace(modelID)
	namespaced := modelID
	if providerID != "" && !strings.HasPrefix(modelID, providerID+"/") {
		namespaced = providerID + "/" + modelID
	}
	for _, model := range h.catalogModels {
		id := strings.TrimSpace(model.ID)
		if id == namespaced || id == modelID {
			return model.Vision
		}
	}
	return catalog.CapabilityUnknown
}

func (h *handler) resolveVisionSidecarCandidate() (sidecar.Candidate, error) {
	if h == nil {
		return sidecar.Candidate{}, sidecar.ErrUnsupportedBackend
	}
	root := h.loadConfigRoot()
	section, _ := root["visionSidecar"].(map[string]any)
	modelID, _ := section["model"].(string)
	backend, _ := section["backend"].(string)
	return sidecar.Resolve(sidecar.Selection{
		Modality:   sidecar.ModalityVisionDescribe,
		ModelID:    strings.TrimSpace(modelID),
		Backend:    strings.TrimSpace(backend),
		Candidates: h.sidecarBackends(),
	})
}

func (h *handler) harnessVisionOverride(ctx context.Context) (*harnesspolicy.Activation, bool) {
	identity := harnessIdentityFromContext(ctx)
	if identity.Status != harnessIdentityAccepted {
		return nil, true
	}
	caps, ok := harnessSidecarCapabilitiesFor(identity.ID)
	if !ok || !caps.Vision {
		return nil, false
	}
	if overrides, ok := harnessSidecarRuntimeFor(h).Overrides(identity.ID); ok {
		return overrides.Vision, true
	}
	return nil, true
}

func (h *handler) resolveVisionActivation(ctx context.Context, sidecarProven bool) (harnesspolicy.Decision, error) {
	override, eligible := h.harnessVisionOverride(ctx)
	root := h.loadConfigRoot()
	section, _ := root["visionSidecar"].(map[string]any)
	decision, err := harnesspolicy.Resolve(harnesspolicy.ResolveInput{
		GlobalEnabled: sidecarSectionEnabled(section),
		Override:      override,
		SidecarProven: sidecarProven && eligible,
	})
	if err == nil {
		// Record the policy outcome only. Execution is recorded by the transform
		// above, never inferred from this decision.
		sidecarPolicyEvidenceFor(ctx).recordVisionDecision(decision)
	}
	return decision, err
}

func (h *handler) visionSidecarLimits() (maxImages, maxImageBytes int, timeout time.Duration) {
	maxImages = vision.DefaultMaxImages
	maxImageBytes = vision.DefaultMaxImageBytes
	if h == nil {
		return maxImages, maxImageBytes, 0
	}
	if h.imageMaxRequestBytes > 0 {
		maxImageBytes = int(h.imageMaxRequestBytes)
	}
	if h.imageTimeout > 0 {
		timeout = h.imageTimeout
	}
	root := h.loadConfigRoot()
	section, _ := root["visionSidecar"].(map[string]any)
	if configured := configPositiveInt(section["maxDescriptionsPerTurn"]); configured > 0 {
		maxImages = configured
	}
	if configured := configPositiveInt(section["timeoutMs"]); configured > 0 {
		timeout = time.Duration(configured) * time.Millisecond
	}
	return maxImages, maxImageBytes, timeout
}

func configPositiveInt(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case float32:
		return int(typed)
	default:
		return 0
	}
}
