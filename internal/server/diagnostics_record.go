package server

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Wibias/Benes/internal/combo"
	"github.com/Wibias/Benes/internal/protocol"
	providercontract "github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/router"
	"github.com/Wibias/Benes/internal/resourcebudget"
	"github.com/Wibias/Benes/internal/sessions"
	"github.com/Wibias/Benes/internal/timeline"
	"github.com/Wibias/Benes/internal/usage"
)

type diagnosticsRecorderKey struct{}

type diagnosticsRecorder struct {
	mu            sync.Mutex
	requestID     string
	correlationID string
	legacyID      string
	started       time.Time
	usage         *protocol.Usage
	routing       diagnosticsRouting
	attempts      []sessions.Attempt
	prices        *priceOverlaySnapshot
	account       string
	surface       string
	admitted      bool
	// Service-tier evidence captured from request state, never re-derived from the
	// serialized outbound body. The configured tier is the request-scoped admission
	// carried in the request context, not a second read of live settings.
	requestedServiceTier string
	physicalSendTurn     *resourcebudget.Turn
	// sidecarPolicy is the request-scoped sidecar decision evidence. It is seeded
	// when the request becomes a recorded data-plane request and is snapshotted
	// at usage emission.
	sidecarPolicy *sidecarPolicyEvidenceRecorder
}

func withDiagnosticsRecorder(ctx context.Context, rec *diagnosticsRecorder) context.Context {
	if rec == nil {
		return ctx
	}
	return context.WithValue(ctx, diagnosticsRecorderKey{}, rec)
}

func diagnosticsRecorderFrom(ctx context.Context) *diagnosticsRecorder {
	if ctx == nil {
		return nil
	}
	rec, _ := ctx.Value(diagnosticsRecorderKey{}).(*diagnosticsRecorder)
	return rec
}

func (rec *diagnosticsRecorder) NoteUsage(src *protocol.Usage) {
	if rec == nil || src == nil {
		return
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	clone := *src
	rec.usage = &clone
}

func (rec *diagnosticsRecorder) SetRoute(requestedModel string, route router.Route) {
	if rec == nil {
		return
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if requestedModel != "" && rec.routing.RequestedModel == "" {
		rec.routing.RequestedModel = requestedModel
	}
	rec.routing.RequestedProvider = route.Provider
	switch route.Provider {
	case comboNamespace:
		rec.routing.Kind = "combo"
		rec.routing.ComboID = route.Model
	case policyNamespace:
		rec.routing.Kind = "policy"
		rec.routing.PolicyID = route.Model
	default:
		rec.routing.Kind = "direct"
	}
}

func (rec *diagnosticsRecorder) NoteCommittedUsageAccount(account providercontract.CommittedUsageAccount) {
	if rec == nil {
		return
	}
	rec.mu.Lock()
	rec.account = usage.ClipField(string(account))
	rec.mu.Unlock()
}

func (rec *diagnosticsRecorder) markAdmitted() {
	if rec == nil {
		return
	}
	rec.mu.Lock()
	rec.admitted = true
	rec.mu.Unlock()
}

// NoteServiceTier captures the tier the client explicitly asked for. A blank or
// absent client tier records nothing; the configured policy tier is recorded
// separately and must not be folded into the client-requested fact.
func (rec *diagnosticsRecorder) NoteServiceTier(requested *string) {
	if rec == nil {
		return
	}
	value := ""
	if requested != nil {
		value = clipTelemetryString(strings.TrimSpace(*requested), diagnosticsStringMax)
	}
	rec.mu.Lock()
	rec.requestedServiceTier = value
	rec.mu.Unlock()
}

// clientRequestedServiceTier returns the explicit client tier captured for this
// request, if the client sent one.
func (rec *diagnosticsRecorder) clientRequestedServiceTier() string {
	if rec == nil {
		return ""
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return rec.requestedServiceTier
}

func (rec *diagnosticsRecorder) BindPhysicalSendTurn(turn *resourcebudget.Turn) {
	if rec == nil || turn == nil {
		return
	}
	rec.mu.Lock()
	if rec.physicalSendTurn == nil {
		rec.physicalSendTurn = turn
	}
	rec.mu.Unlock()
}

func (rec *diagnosticsRecorder) PhysicalSends() []resourcebudget.PhysicalSend {
	if rec == nil {
		return nil
	}
	rec.mu.Lock()
	turn := rec.physicalSendTurn
	rec.mu.Unlock()
	if turn == nil {
		return nil
	}
	return turn.PhysicalSends()
}

func (rec *diagnosticsRecorder) wasAdmitted() bool {
	if rec == nil {
		return false
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return rec.admitted
}

func (rec *diagnosticsRecorder) NoteOpen(provider Provider) {
	if rec == nil {
		return
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	inner := unwrapSessionProvider(provider)
	if member, ok := inner.(interface{ CommittedID() string }); ok {
		if id := member.CommittedID(); id != "" {
			rec.routing.CommittedMember = id
		}
	}
	if src, ok := inner.(interface{ Attempts() []combo.Attempt }); ok {
		rec.attempts = copySessionAttempts(src.Attempts())
	}
}

func (rec *diagnosticsRecorder) snapshot() (diagnosticsRouting, []sessions.Attempt) {
	if rec == nil {
		return diagnosticsRouting{}, nil
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return rec.routing, append([]sessions.Attempt(nil), rec.attempts...)
}

func (rec *diagnosticsRecorder) Usage() *protocol.Usage {
	if rec == nil {
		return nil
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if rec.usage == nil {
		return nil
	}
	clone := *rec.usage
	return &clone
}

func (h *handler) recordRequestTelemetry(r *http.Request, cap *statusCapture, start time.Time, rec *sessionRecorder, trace *timeline.Trace) {
	if h == nil || r == nil {
		return
	}
	if h.diagnostics == nil {
		h.diagnostics = newRequestTelemetryState()
	}
	diag := diagnosticsRecorderFrom(r.Context())
	status := finalObservedStatus(cap)
	duration := time.Since(start).Milliseconds()
	record := requestTelemetryRecord{
		Timestamp:  start.UTC(),
		Method:     r.Method,
		Path:       r.URL.Path,
		Status:     status,
		DurationMs: duration,
		Protocol:   protocolFromPath(r.URL.Path),
	}
	if diag != nil {
		record.RequestID = diag.requestID
		record.CorrelationID = clipTelemetryString(diag.correlationID, diagnosticsStringMax)
		record.LegacyID = clipTelemetryString(diag.legacyID, diagnosticsStringMax)
		record.RequestedServiceTier = diag.clientRequestedServiceTier()
		record.PhysicalSends = projectDiagnosticsPhysicalSends(diag.PhysicalSends())
	}
	if configuredServiceTier, admitted := admittedServiceTierFrom(r.Context()); admitted {
		record.ConfiguredServiceTier = clipTelemetryString(configuredServiceTier, diagnosticsStringMax)
	}
	if record.RequestID == "" {
		record.RequestID = sessions.NewRequestID()
	}
	if record.LegacyID == "" {
		record.LegacyID = record.RequestID
	}
	if rec != nil {
		record.SessionID = rec.SessionID()
		rec.mu.Lock()
		in := rec.in
		rec.mu.Unlock()
		record.Routing = routingFromSession(in)
		record.Attempts = copyTelemetryAttempts(in.Attempts)
		record.RequestBytes = in.RequestBytes
		record.ResponseBytes = in.ResponseBytes
		if record.Protocol == "" {
			record.Protocol = in.Protocol
		}
		if record.RequestID == "" {
			record.RequestID = in.RequestID
		}
		if record.CorrelationID == "" {
			record.CorrelationID = clipTelemetryString(in.CorrelationID, diagnosticsStringMax)
		}
	}
	if record.Routing == (diagnosticsRouting{}) {
		if routing, attempts := diag.snapshot(); routing != (diagnosticsRouting{}) || len(attempts) > 0 {
			record.Routing = routing
			if len(record.Attempts) == 0 {
				record.Attempts = copyTelemetryAttempts(attempts)
			}
		} else if trace != nil {
			record.Routing = routingFromTrace(trace.Route())
		}
	} else if len(record.Attempts) == 0 {
		_, attempts := diag.snapshot()
		record.Attempts = copyTelemetryAttempts(attempts)
	}
	if trace != nil {
		route := trace.Route()
		if route.ProviderConnection != "" {
			record.Routing.Provider = clipTelemetryString(route.ProviderConnection, diagnosticsStringMax)
		}
		if route.Model != "" {
			record.Routing.ResolvedModel = clipTelemetryString(route.Model, diagnosticsStringMax)
		}
	}
	if record.RequestBytes == nil && r.ContentLength > 0 {
		n := r.ContentLength
		record.RequestBytes = &n
	}
	if record.ResponseBytes == nil && cap != nil && cap.bytes > 0 {
		n := cap.bytes
		record.ResponseBytes = &n
	}
	if trace != nil {
		events, timing, failure, errorCode := copyTelemetryTimeline(trace.Events())
		timing.TotalMs = duration
		record.Timeline = events
		record.Timing = timing
		record.Failure = failure
		record.ErrorCode = errorCode
	} else {
		record.Timing.TotalMs = duration
	}
	capturedUsage := diag.Usage()
	record.Usage = projectDiagnosticsUsage(capturedUsage)
	if capturedUsage != nil {
		record.ResponseServiceTier = clipTelemetryString(strings.TrimSpace(capturedUsage.ServiceTier), diagnosticsStringMax)
		record.Cost = projectDiagnosticsCost(record.Routing.Provider, record.Routing.ResolvedModel, start, capturedUsage, overlaysForRequest(diag, rec))
	}
	if cap != nil && cap.panicked {
		record.Failure = &diagnosticsFailure{
			Side:  string(timeline.SideLocal),
			Stage: string(timeline.StageTerminalDelivery),
			Cause: "internal_panic",
		}
		record.ErrorCode = "internal_panic"
	}
	// Take the terminal snapshot at the emission point, so identity, policy, and
	// execution updates recorded anywhere in the lifecycle are already included.
	record.SidecarPolicy = diag.finalSidecarPolicyEvidence(harnessIdentityFromContext(r.Context()))
	h.diagnostics.append(record)
	h.appendUsageLog(r, record, diag)
}

func (h *handler) appendUsageLog(r *http.Request, record requestTelemetryRecord, diag *diagnosticsRecorder) {
	if h == nil || h.usageLedger == nil || r == nil || !isSessionPath(r.URL.Path) || !diag.wasAdmitted() {
		return
	}
	item := usage.Entry{
		Timestamp:      record.Timestamp.UTC().UnixMilli(),
		RequestID:      usage.ClipField(record.RequestID),
		Status:         record.Status,
		DurationMs:     record.DurationMs,
		RequestedModel: usage.ClipField(record.Routing.RequestedModel),
		Provider:       usage.ClipField(record.Routing.Provider),
		Model:          usage.ClipField(record.Routing.ResolvedModel),
		UsageStatus:    "unreported",
		RouteDecision:  usageRouteDecision(record),
	}
	if item.Provider == "" {
		item.Provider = usage.ClipField(record.Routing.RequestedProvider)
	}
	if item.Model == "" {
		item.Model = usage.ClipField(record.Routing.RequestedModel)
	}
	if diag != nil {
		diag.mu.Lock()
		item.Account = usage.ClipField(diag.account)
		item.Surface = usage.CanonicalSurface(diag.surface)
		diag.mu.Unlock()
	}
	if record.Usage != nil {
		item.UsageStatus = record.Usage.Status
		if item.UsageStatus == "" {
			item.UsageStatus = "reported"
		}
		item.Usage = usageTokensFromDiagnostics(record.Usage)
		item.TotalTokens = record.Usage.TotalTokens
	}
	if len(record.Attempts) > 0 {
		item.Attempts = make([]usage.Attempt, 0, len(record.Attempts))
		for _, attempt := range record.Attempts {
			item.Attempts = append(item.Attempts, usage.Attempt{
				Provider:    usage.ClipField(attempt.Member),
				UsageStatus: item.UsageStatus,
			})
		}
	}
	raw, err := usage.Encode(item)
	if err != nil {
		return
	}
	if err := h.usageLedger.Append(raw); err != nil {
		return
	}
	if h.requestHistory == nil {
		return
	}
	if err := h.requestHistory.CatchUpBestEffort(); err != nil {
		h.debug.appendProvider("request-history index failed")
	}
}

func usageRouteDecision(record requestTelemetryRecord) *usage.RouteDecision {
	out := &usage.RouteDecision{
		RouteKind: usage.ClipField(record.Routing.Kind),
	}
	if record.Routing.Kind == "policy" {
		if id := usage.ClipField(record.Routing.PolicyID); id != "" {
			out.Profile = &usage.RouteProfile{ID: id}
		}
	}
	provider := usage.ClipField(record.Routing.Provider)
	model := usage.ClipField(record.Routing.ResolvedModel)
	if provider != "" || model != "" {
		out.Selected = &usage.RouteSelected{Provider: provider, Model: model}
	}
	out.SidecarPolicy = usageSidecarPolicyEvidence(record.SidecarPolicy)
	return out
}

func usageSidecarPolicyEvidence(evidence *sidecarPolicyEvidence) *usage.RouteSidecarPolicy {
	if evidence == nil {
		return nil
	}
	return &usage.RouteSidecarPolicy{
		Identity: usage.RouteSidecarIdentity{
			Status:    usage.ClipField(string(evidence.Identity.Status)),
			HarnessID: usage.ClipField(evidence.Identity.HarnessID),
		},
		WebSearch: usageSidecarPolicyOutcome(evidence.WebSearch),
		Vision:    usageSidecarPolicyOutcome(evidence.Vision),
	}
}

func usageSidecarPolicyOutcome(outcome sidecarEvidenceDecision) usage.RouteSidecarOutcome {
	return usage.RouteSidecarOutcome{
		Configured: usage.RouteSidecarConfigured{
			Enabled: outcome.Configured.Enabled,
			Source:  usage.ClipField(string(outcome.Configured.Source)),
		},
		Resolution: usage.ClipField(string(outcome.Resolution)),
		Ran:        outcome.Ran,
		Failure:    usage.ClipField(string(outcome.Failure)),
	}
}

func usageTokensFromDiagnostics(src *diagnosticsUsage) *usage.TokenUsage {
	if src == nil {
		return nil
	}
	out := &usage.TokenUsage{}
	if src.InputTokens != nil {
		out.InputTokens = *src.InputTokens
	}
	if src.OutputTokens != nil {
		out.OutputTokens = *src.OutputTokens
	}
	out.CachedInputTokens = src.CachedInputTokens
	out.CacheReadInputTokens = src.CacheReadInputTokens
	out.CacheCreationInputTokens = src.CacheCreationInputTokens
	out.ReasoningOutputTokens = src.ReasoningOutputTokens
	return out
}

func newDiagnosticsRecorder(legacyID, correlationID string, started time.Time) *diagnosticsRecorder {
	return &diagnosticsRecorder{
		requestID:     sessions.NewRequestID(),
		correlationID: clipTelemetryString(correlationID, diagnosticsStringMax),
		legacyID:      clipTelemetryString(legacyID, diagnosticsStringMax),
		started:       started,
	}
}

func overlaysForRequest(diag *diagnosticsRecorder, rec *sessionRecorder) []usage.PriceRecord {
	if diag != nil && diag.prices != nil {
		return diag.prices.Get()
	}
	if rec != nil && rec.prices != nil {
		return rec.prices.Get()
	}
	return nil
}
