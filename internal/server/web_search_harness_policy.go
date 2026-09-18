package server

import (
	"context"
	"net/http"
	"strings"

	"github.com/Wibias/Benes/internal/harnesspolicy"
	"github.com/Wibias/Benes/internal/sidecar/websearch"
)

func (h *handler) harnessWebSearchOverride(ctx context.Context) (*harnesspolicy.Activation, bool) {
	identity := harnessIdentityFromContext(ctx)
	if identity.Status != harnessIdentityAccepted {
		return nil, true
	}
	caps, ok := harnessSidecarCapabilitiesFor(identity.ID)
	if !ok || !caps.WebSearch {
		return nil, false
	}
	if overrides, ok := harnessSidecarRuntimeFor(h).Overrides(identity.ID); ok {
		return overrides.WebSearch, true
	}
	return nil, true
}

func (h *handler) resolveWebSearchActivation(ctx context.Context, nativeProven, sidecarProven bool) (harnesspolicy.Decision, error) {
	override, eligible := h.harnessWebSearchOverride(ctx)
	decision, err := harnesspolicy.Resolve(harnesspolicy.ResolveInput{
		GlobalEnabled:   h.webSearchSidecarEnabled(),
		Override:        override,
		NativeRequested: true,
		NativeProven:    nativeProven,
		SidecarProven:   sidecarProven && eligible,
	})
	if err == nil {
		// Record the policy outcome only. Whether the sidecar ran is recorded at
		// the execution point and is never inferred from this decision.
		sidecarPolicyEvidenceFor(ctx).recordWebSearchDecision(decision)
	}
	return decision, err
}

func (h *handler) webSearchClientForRequest(ctx context.Context, providerID, modelID string, nativeProven bool) (harnesspolicy.Decision, *websearch.Client, error) {
	decision, err := h.resolveWebSearchActivation(ctx, nativeProven, true)
	if err != nil {
		return decision, nil, err
	}
	if decision.Native {
		return decision, nil, nil
	}
	if !decision.Enabled {
		return decision, nil, websearch.ErrUnsupportedSelection
	}

	client, err := h.webSearchClientCandidate(providerID, modelID)
	if err != nil {
		unsupported, resolveErr := h.resolveWebSearchActivation(ctx, false, false)
		if resolveErr != nil {
			return unsupported, nil, resolveErr
		}
		return unsupported, nil, err
	}
	// The observer fires where the sidecar is actually invoked, so a resolved but
	// unused client still reports ran=false.
	client.Observe(func() { sidecarPolicyEvidenceFor(ctx).markWebSearchRan() })
	return decision, client, nil
}

func (h *handler) webSearchClientCandidate(providerID, modelID string) (*websearch.Client, error) {
	cfg := h.webSearchConfig(providerID, modelID)
	if err := websearch.ValidateSelection(cfg); err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		if h == nil || h.credentials == nil || (cfg.CredentialRef.ID == "" && cfg.CredentialRef.Source == "") {
			return nil, websearch.ErrMissingCredential
		}
		secret, err := h.credentials.Get(cfg.CredentialRef)
		if err != nil || len(secret) == 0 {
			return nil, websearch.ErrMissingCredential
		}
		cfg.APIKey = string(secret)
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{}
	}
	return websearch.New(cfg)
}
