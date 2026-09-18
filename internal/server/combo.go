package server

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Wibias/Benes/internal/combo"
	"github.com/Wibias/Benes/internal/config"
	providercontract "github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/router"
)

const (
	comboNamespace  = "combo"
	policyNamespace = "policy"
)

type ComboTarget struct {
	ProviderID string
	Model      string
	Protocol   string
}

type Combo struct {
	ID      string
	Targets []ComboTarget
}

type resolvedProvider struct {
	Provider        Provider
	ProviderID      string
	MissingCombo    bool
	MissingPolicy   bool
	MissingProvider bool
}

func resolveLogicalOpenAIRoute(route router.Route, defaultAccess string) router.Route {
	if route.CodexAccountID != "" || route.Provider != config.LogicalOpenAIID {
		return route
	}
	route.Provider = config.OpenAIDefaultConnection(defaultAccess)
	return route
}

func (h *handler) applyLogicalOpenAIAccess(route router.Route) router.Route {
	return resolveLogicalOpenAIRoute(route, h.logicalOpenAIDefaultAccess())
}

func cloneCombos(values []Combo) map[string]Combo {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]Combo, len(values))
	for _, value := range values {
		if value.ID == "" {
			continue
		}
		targets := make([]ComboTarget, len(value.Targets))
		copy(targets, value.Targets)
		cloned[value.ID] = Combo{ID: value.ID, Targets: targets}
	}
	return cloned
}

func (h *handler) comboByID(id string) (Combo, bool) {
	if h == nil {
		return Combo{}, false
	}
	h.combosMu.RLock()
	defer h.combosMu.RUnlock()
	spec, ok := h.combos[id]
	return spec, ok
}

func (h *handler) hasRuntimeCombos() bool {
	if h == nil {
		return false
	}
	h.combosMu.RLock()
	defer h.combosMu.RUnlock()
	return len(h.combos) > 0
}

func (h *handler) listRuntimeCombos() []Combo {
	if h == nil {
		return nil
	}
	h.combosMu.RLock()
	defer h.combosMu.RUnlock()
	out := make([]Combo, 0, len(h.combos))
	for _, spec := range h.combos {
		targets := make([]ComboTarget, len(spec.Targets))
		copy(targets, spec.Targets)
		out = append(out, Combo{ID: spec.ID, Targets: targets})
	}
	return out
}

func (h *handler) replaceRuntimeCombos(values []Combo) {
	if h == nil {
		return
	}
	h.combosMu.Lock()
	h.combos = cloneCombos(values)
	h.combosMu.Unlock()
}

func (h *handler) resolveProvider(route router.Route) resolvedProvider {
	return h.resolveProviderWithEvidence(route, policyRequestEvidence{})
}

// providerResolveCallCount counts resolveProviderWithEvidence invocations (tests).
var providerResolveCallCount atomic.Int64

func (h *handler) resolveProviderWithEvidence(route router.Route, evidence policyRequestEvidence) resolvedProvider {
	providerResolveCallCount.Add(1)
	requestedProvider := route.Provider
	requestedModel := route.Model
	route = h.applyLogicalOpenAIAccess(route)
	if route.Provider == comboNamespace {
		spec, ok := h.comboByID(route.Model)
		if ok {
			provider := withProviderRouteAttribution(h.comboWalker(spec), requestedProvider, comboNamespace, requestedModel)
			return resolvedProvider{Provider: provider, ProviderID: comboNamespace}
		}
		if h.hasRuntimeCombos() {
			return resolvedProvider{ProviderID: comboNamespace, MissingCombo: true}
		}
	}
	if route.Provider == policyNamespace {
		spec, ok := h.policyCombo(route.Model, evidence)
		if !ok {
			return resolvedProvider{ProviderID: policyNamespace, MissingPolicy: true}
		}
		provider := withProviderRouteAttribution(h.policyWalker(spec, evidence), requestedProvider, policyNamespace, requestedModel)
		return resolvedProvider{Provider: provider, ProviderID: policyNamespace}
	}
	provider, ok := h.providers[route.Provider]
	if !ok {
		return resolvedProvider{ProviderID: route.Provider, MissingProvider: true}
	}
	provider = withProviderRouteAttribution(provider, requestedProvider, route.Provider, requestedModel)
	provider = h.withVisionPolicyProvider(provider, route.Provider, route.Model)
	return resolvedProvider{Provider: provider, ProviderID: route.Provider}
}

func (h *handler) policyCombo(id string, evidence policyRequestEvidence) (Combo, bool) {
	record, ok := h.routingProfileRecord(id)
	if !ok {
		return Combo{}, false
	}
	sticky := ""
	if h != nil && h.policyRuntime != nil {
		sticky = h.policyRuntime.lookup(policyStickyKeys(id, evidence))
	}
	eligible, _, _ := evaluatePolicySelection(policyEvalInput{
		Record:       record,
		Views:        h.policyCandidateViews(record),
		Evidence:     evidence,
		Signals:      h.policySignals(),
		StickyMember: sticky,
	})
	disabled := h.disabledProviderIDs()
	targets := make([]ComboTarget, 0, len(eligible))
	for _, candidate := range eligible {
		if candidate.Provider == "" || candidate.Model == "" {
			continue
		}
		if disabled[candidate.Provider] {
			continue
		}
		if _, exists := h.providers[candidate.Provider]; !exists {
			continue
		}
		targets = append(targets, ComboTarget{ProviderID: candidate.Provider, Model: candidate.Model})
	}
	if len(targets) == 0 {
		return Combo{}, false
	}
	return Combo{ID: id, Targets: targets}, true
}

type policyWalkStats struct {
	hops   int
	lastID string
}

func (s *policyWalkStats) note(id string) {
	if s == nil {
		return
	}
	s.hops++
	s.lastID = id
}

type timedPolicyProvider struct {
	inner    Provider
	runtime  *policyRuntime
	memberID string
	stats    *policyWalkStats
}

func (t timedPolicyProvider) Open(ctx context.Context, dispatch providercontract.DispatchRequest) (EventStream, error) {
	if t.stats != nil {
		t.stats.note(t.memberID)
	}
	start := time.Now()
	stream, err := t.inner.Open(ctx, dispatch)
	if t.runtime != nil {
		t.runtime.observeOpen(t.memberID, time.Since(start), err == nil)
	}
	return stream, err
}

func (t timedPolicyProvider) OpenCommitted(ctx context.Context, dispatch providercontract.DispatchRequest) (EventStream, error) {
	if committed, ok := t.inner.(committedOpener); ok {
		return committed.OpenCommitted(ctx, dispatch)
	}
	return t.Open(ctx, dispatch)
}

type policyWalker struct {
	inner     *combo.Walker
	runtime   *policyRuntime
	profileID string
	evidence  policyRequestEvidence
	committed string
	stats     *policyWalkStats
}

func (h *handler) policyWalker(spec Combo, evidence policyRequestEvidence) Provider {
	stats := &policyWalkStats{}
	targets := make([]combo.Target, 0, len(spec.Targets))
	for _, target := range spec.Targets {
		resolved := h.applyLogicalOpenAIAccess(router.Route{Provider: target.ProviderID, Model: target.Model})
		id := resolved.Provider + "/" + target.Model
		provider := withProviderRouteAttribution(h.providers[resolved.Provider], target.ProviderID, resolved.Provider, target.Model)
		provider = h.withVisionPolicyProvider(provider, resolved.Provider, target.Model)
		provider = timedPolicyProvider{inner: provider, runtime: h.policyRuntime, memberID: id, stats: stats}
		targets = append(targets, combo.Target{
			Member:   combo.Member{ID: id, Protocol: target.Protocol},
			Model:    target.Model,
			Provider: provider,
		})
	}
	return &policyWalker{
		inner:     &combo.Walker{Targets: targets},
		runtime:   h.policyRuntime,
		profileID: spec.ID,
		evidence:  evidence,
		stats:     stats,
	}
}

func (p *policyWalker) Open(ctx context.Context, dispatch providercontract.DispatchRequest) (EventStream, error) {
	if p == nil || p.inner == nil {
		return nil, fmt.Errorf("policy walker is missing")
	}
	start := time.Now()
	stream, err := p.inner.Open(ctx, dispatch)
	member := ""
	if err == nil {
		p.committed = p.inner.CommittedID()
		member = p.committed
		if p.runtime != nil {
			p.runtime.bind(policyStickyKeys(p.profileID, p.evidence), p.committed)
		}
	} else if p.stats != nil {
		member = p.stats.lastID
	}
	if p.runtime != nil {
		hops := 0
		if p.stats != nil {
			hops = p.stats.hops
		}
		p.runtime.recordRequest(policyRequestSample{
			ProfileID:  p.profileID,
			Member:     member,
			DurationMs: int(time.Since(start).Milliseconds()),
			Success:    err == nil,
			Hops:       hops,
			At:         time.Now(),
		})
	}
	return stream, err
}

func (p *policyWalker) OpenCommitted(ctx context.Context, dispatch providercontract.DispatchRequest) (EventStream, error) {
	if p == nil || p.inner == nil {
		return nil, fmt.Errorf("policy walker is missing")
	}
	return p.inner.OpenCommitted(ctx, dispatch)
}

func (p *policyWalker) CommittedID() string {
	if p == nil {
		return ""
	}
	return p.committed
}

func (p *policyWalker) Attempts() []combo.Attempt {
	if p == nil || p.inner == nil {
		return nil
	}
	return p.inner.Attempts()
}

func (p *policyWalker) BindOutgoingID(id string) {
	if p == nil || p.runtime == nil {
		return
	}
	id = strings.TrimSpace(id)
	if id == "" || p.committed == "" {
		return
	}
	p.runtime.bind([]string{"resp:" + id}, p.committed)
}

func (h *handler) comboWalker(spec Combo) Provider {
	targets := make([]combo.Target, 0, len(spec.Targets))
	for _, target := range spec.Targets {
		resolved := h.applyLogicalOpenAIAccess(router.Route{Provider: target.ProviderID, Model: target.Model})
		id := resolved.Provider + "/" + target.Model
		provider := withProviderRouteAttribution(h.providers[resolved.Provider], target.ProviderID, resolved.Provider, target.Model)
		provider = h.withVisionPolicyProvider(provider, resolved.Provider, target.Model)
		targets = append(targets, combo.Target{
			Member:   combo.Member{ID: id, Protocol: target.Protocol},
			Model:    target.Model,
			Provider: provider,
		})
	}
	return &combo.Walker{Targets: targets}
}

func (t timedPolicyProvider) Protocol() string {
	if reporter, ok := t.inner.(interface{ Protocol() string }); ok {
		return strings.TrimSpace(reporter.Protocol())
	}
	return ""
}

func (p *policyWalker) Protocol() string {
	if p == nil || p.inner == nil {
		return ""
	}
	return p.inner.Protocol()
}
