package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/Wibias/Benes/internal/protocol"
	providercontract "github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/resourcebudget"
	"github.com/Wibias/Benes/internal/responses/bridge"
	"github.com/Wibias/Benes/internal/router"
	"github.com/Wibias/Benes/internal/sessions"
	"github.com/Wibias/Benes/internal/sidecar/fabric"
	"github.com/Wibias/Benes/internal/sidecar/websearch"
	"github.com/Wibias/Benes/internal/timeline"
	"github.com/Wibias/Benes/internal/usage"
)

// modelTurnCallCount counts invocations of the shared model-turn primitive.
// Contract tests assert /v1/responses and Fabric execute both call it.
var modelTurnCallCount atomic.Int64

// modelTurnInput is the shared lower-level boundary used by /v1/responses and
// Fabric execute. HTTP-only concerns stay outside: SSE framing writers (passed
// in as EmitFrames), CORS, data-plane bearer/host admission, chat/anthropic
// wire conversion, and image endpoints.
type modelTurnInput struct {
	Model     string
	Input     string
	Parsed    *protocol.ParsedRequest // when set (responses), used instead of Input
	Surface   string
	RequestID string
	Turn      *resourcebudget.Turn // pre-acquired; transferred, not re-acquired
	Path      string
	Protocol  string // telemetry protocol label; default fabric-execute / responses
	Persist   bool   // Fabric: write internal diagnostics/usage/timeline artifacts

	// Responses-only optional extensions (still inside the shared primitive).
	ForwardHeaders   providercontract.ForwardHeaders
	SearchClient     *websearch.Client
	RoutedCompaction bool
	EmitFrames       func(frames []bridge.Frame) error // non-nil => streaming consume
	WatchSession     bool
	Diag             *diagnosticsRecorder // reuse caller's recorder when set
	Trace            *timeline.Trace      // reuse caller's trace when set
	SkipRouteParse   bool                 // when Parsed already has UpstreamModelID and route known via Model
	PreResolvedRoute *router.Route
	// PreResolved, when non-nil (Responses), is the sole provider-resolution
	// authority for this turn. runModelTurn must reuse it and must not resolve again.
	PreResolved *resolvedProvider
	// PreferCommitted, when true with PreResolved, opens via OpenCommitted/reopenProvider
	// so combo/policy cannot hop after the primary turn already committed a physical
	// member. Fabric post-child continuation must set this. Fail closed if unavailable.
	// Do not set on the first Responses/Fabric primary open (walker not yet committed).
	PreferCommitted bool
	// PhysicalPin forces the exact credential/account + destination that produced
	// provider-native continuation state. Memory-only; never written to Fabric events.
	PhysicalPin *providercontract.PhysicalPin
}

// modelTurnToolCall is a completed function_call observed during a collect turn.
// ThoughtSignature / ProviderMetadata are memory-only provider continuation state
// (e.g. Google ThoughtSignature). They must never be written to Fabric events/leases.
type modelTurnToolCall struct {
	ID               string
	CallID           string
	Name             string
	Arguments        string
	ThoughtSignature string
	ProviderMetadata *protocol.ProviderOpaqueMetadata
}

// modelTurnResult is the safe internal outcome. OutputText is for the bounded
// Fabric result store only - never for Fabric events/leases.
type modelTurnResult struct {
	Status           string
	Reason           string
	OutputText       string
	Truncated        bool
	Provider         string
	ResolvedModel    string
	RequestedModel   string
	RequestID        string
	Usage            *protocol.Usage
	TerminalResponse map[string]any
	ToolCalls        []modelTurnToolCall
	HTTPStatus       int
	ServingProtocol  string
	NativeResponseID string
	// Resolved is the in-process provider authority used for this turn. Fabric may
	// reuse it for post-child continuation; never persist into durable events/leases.
	Resolved *resolvedProvider
	// Route is the logical route used for this turn (for PreResolvedRoute reuse).
	Route router.Route
	// PhysicalPin is the per-turn credential/account + destination ownership handle
	// observed from the provider stream. Memory-only; never durable Fabric state.
	PhysicalPin *providercontract.PhysicalPin
}

// runModelTurn is the one shared model-turn primitive for /v1/responses and
// Fabric execute: route, resolveProviderWithEvidence (policy/combo), context
// projection/recovery, provider dispatch, bridge consumption, committed usage,
// attribution, and terminal usage. No Fabric HTTP loopback.
func (h *handler) runModelTurn(ctx context.Context, in modelTurnInput) (modelTurnResult, error) {
	modelTurnCallCount.Add(1)
	return h.runModelTurnImpl(ctx, in)
}

// runAuthoritativeTurn is a compatibility alias used by older call sites/tests.
func (h *handler) runAuthoritativeTurn(ctx context.Context, in authoritativeTurnInput) (authoritativeTurnResult, error) {
	out, err := h.runModelTurn(ctx, modelTurnInput{
		Model:     in.Model,
		Input:     in.Input,
		Surface:   in.Surface,
		RequestID: in.RequestID,
		Turn:      in.Turn,
		Path:      in.Path,
		Protocol:  "fabric-execute",
		Persist:   true,
	})
	return authoritativeTurnResult{
		Status:         out.Status,
		Reason:         out.Reason,
		OutputText:     out.OutputText,
		Provider:       out.Provider,
		ResolvedModel:  out.ResolvedModel,
		RequestedModel: out.RequestedModel,
		RequestID:      out.RequestID,
		Usage:          out.Usage,
	}, err
}

// authoritativeTurnInput/Result kept for compatibility with existing names.
type authoritativeTurnInput struct {
	Model     string
	Input     string
	Surface   string
	RequestID string
	Turn      *resourcebudget.Turn
	Path      string
}

type authoritativeTurnResult struct {
	Status         string
	Reason         string
	OutputText     string
	Provider       string
	ResolvedModel  string
	RequestedModel string
	RequestID      string
	Usage          *protocol.Usage
}

func (h *handler) runModelTurnImpl(ctx context.Context, in modelTurnInput) (modelTurnResult, error) {
	if h == nil {
		return modelTurnResult{}, fabric.InvalidTransition("handler unavailable")
	}
	started := time.Now()
	model := strings.TrimSpace(in.Model)
	if model == "" && in.Parsed != nil {
		model = strings.TrimSpace(in.Parsed.ModelID)
	}
	if model == "" {
		return modelTurnResult{}, fabric.InvalidTask("model is required")
	}
	requestID := strings.TrimSpace(in.RequestID)
	if requestID == "" {
		requestID = sessions.NewRequestID()
	}
	path := strings.TrimSpace(in.Path)
	if path == "" {
		path = "/v1/responses"
	}
	protocolLabel := strings.TrimSpace(in.Protocol)
	if protocolLabel == "" {
		if in.Persist {
			protocolLabel = "fabric-execute"
		} else {
			protocolLabel = "responses"
		}
	}
	surface := strings.TrimSpace(in.Surface)
	// Empty surface is intentional unattributed; non-empty must be allowlisted
	// via CanonicalSurface (unknown values become "").

	diag := in.Diag
	createdDiag := false
	if diag == nil {
		diag = newDiagnosticsRecorder(requestID, requestID, started)
		diag.surface = usage.CanonicalSurface(surface)
		diag.prices = h.newPriceOverlaySnapshot()
		diag.markAdmitted()
		createdDiag = true
	} else if diag.surface == "" && surface != "" {
		diag.surface = usage.CanonicalSurface(surface)
	}
	if createdDiag {
		// Fabric mints req_* at admission and must thread that same id through
		// diagnostics/usage/RH/timeline/result. newDiagnosticsRecorder otherwise
		// invents a second id. Responses supplies its own diag — do not overwrite
		// its canonical minted requestId with the client correlation header.
		diag.requestID = requestID
	}
	trace := in.Trace
	if trace == nil {
		trace = timeline.New(requestID, 16)
	}
	ctx = withDiagnosticsRecorder(ctx, diag)
	diag.bindSidecarPolicyEvidence(h, ctx)
	// Freeze the configured request policy for this logical request; every attempt and
	// continuation below reads the admitted value instead of live settings.
	ctx = admitServiceTierOnce(ctx)
	ctx = providercontract.WithCommittedUsageAccountObserver(ctx, diag)
	ctx = timeline.WithTrace(ctx, trace)

	httpStatus := 200
	persist := in.Persist
	defer func() {
		if persist {
			h.recordInternalTurnTelemetry(ctx, path, "POST", started, diag, trace, httpStatus, protocolLabel)
			if h.timeline != nil && trace != nil {
				_ = h.timeline.Save(trace)
			}
		}
	}()

	var route router.Route
	var err error
	if in.PreResolvedRoute != nil {
		route = *in.PreResolvedRoute
	} else {
		route, err = h.parseRoute(model)
		if err != nil {
			httpStatus = 400
			if message, ok := ambiguousAliasMessage(err); ok {
				trace.Mark(timeline.StagePreDispatch, timeline.SideLocal, "", false, "ambiguous_alias")
				return modelTurnResult{Status: "failed", Reason: message, RequestID: requestID, RequestedModel: model, HTTPStatus: httpStatus}, nil
			}
			trace.Mark(timeline.StagePreDispatch, timeline.SideLocal, "", false, "explicit_route_required")
			httpStatus = 501
			return modelTurnResult{Status: "failed", Reason: "explicit_route_required", RequestID: requestID, RequestedModel: model, HTTPStatus: httpStatus}, nil
		}
	}
	diag.SetRoute(model, route)
	trace.Mark(timeline.StagePreDispatch, timeline.SideLocal, "", true, "")

	var req protocol.ParsedRequest
	if in.Parsed != nil {
		req = *in.Parsed
		req.UpstreamModelID = route.Model
	} else {
		input := strings.TrimSpace(in.Input)
		if input == "" {
			input = "proceed"
		}
		req = protocol.ParsedRequest{
			Source:          protocol.RequestSourceResponses,
			ModelID:         model,
			UpstreamModelID: route.Model,
			Stream:          false,
			Context: protocol.Context{
				Messages: []protocol.Message{{
					Role:    protocol.RoleUser,
					Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: input}},
				}},
			},
		}
	}

	// Record the request-side service-tier facts once the request state is assembled:
	// the tier the client asked for, next to the configured policy tier the recorder
	// captured from the canonical request policy.
	diag.NoteServiceTier(req.Options.ServiceTier)

	var resolved resolvedProvider
	if in.PreResolved != nil {
		// Responses: reuse the exact preflight authority — no second resolve.
		resolved = *in.PreResolved
	} else {
		// Fabric (and other callers): resolve once inside the shared primitive.
		evidence := policyEvidenceFromRequest(req)
		resolved = h.resolveProviderWithEvidence(route, evidence)
	}
	if resolved.MissingCombo {
		httpStatus = 404
		trace.Mark(timeline.StagePreDispatch, timeline.SideLocal, "", false, "combo_not_found")
		return modelTurnResult{Status: "failed", Reason: "combo_not_found", RequestID: requestID, RequestedModel: model, HTTPStatus: httpStatus}, nil
	}
	if resolved.MissingPolicy {
		httpStatus = 404
		trace.Mark(timeline.StagePreDispatch, timeline.SideLocal, "", false, "policy_not_found")
		return modelTurnResult{Status: "failed", Reason: "policy_not_found", RequestID: requestID, RequestedModel: model, HTTPStatus: httpStatus}, nil
	}
	if resolved.MissingProvider || resolved.Provider == nil {
		httpStatus = 501
		return modelTurnResult{Status: "failed", Reason: "provider_unavailable", RequestID: requestID, RequestedModel: model, HTTPStatus: httpStatus}, nil
	}
	provider := resolved.Provider
	servingProtocol := servingProtocolFromProvider(provider)

	proj := h.projectProviderContext(&req, provider, in.RoutedCompaction, in.SearchClient != nil)
	turn := in.Turn
	ownsTurn := false
	if turn == nil {
		var turnErr error
		bodyHint := len(in.Input)
		if in.Parsed != nil {
			bodyHint = 0
		}
		turn, turnErr = h.acquireTurn(ctx, bodyHint)
		if turnErr != nil {
			httpStatus = 503
			return modelTurnResult{}, fabric.CapacityExceeded("request resource budget is exhausted")
		}
		ownsTurn = true
	}
	if ownsTurn && turn != nil {
		defer turn.Close()
	}

	dispatch := providercontract.DispatchRequest{
		Parsed:                req,
		ForwardHeaders:        in.ForwardHeaders,
		CodexAccountID:        route.CodexAccountID,
		Turn:                  turn,
		PreferCommitted:       in.PreferCommitted,
		PhysicalPin:           in.PhysicalPin,
		ConfiguredServiceTier: admittedServiceTier(ctx),
	}

	var stream EventStream
	var openErr error
	if in.PreferCommitted {
		// Post-commit Fabric continuation: reopen the committed physical member only.
		openCommitted := func(ctx context.Context, d providercontract.DispatchRequest) (EventStream, error) {
			return reopenProvider(ctx, provider, d)
		}
		if in.SearchClient != nil {
			stream, openErr = websearch.OpenLoop(ctx, openCommitted, dispatch, in.SearchClient)
		} else if proj.active {
			stream, openErr = openRecoveryLoop(ctx, committedOpenProvider{inner: provider}, dispatch, proj)
		} else {
			stream, openErr = reopenProvider(ctx, provider, dispatch)
		}
	} else if in.SearchClient != nil {
		stream, openErr = websearch.OpenLoop(ctx, provider.Open, dispatch, in.SearchClient)
	} else if proj.active {
		stream, openErr = openRecoveryLoop(ctx, provider, dispatch, proj)
	} else {
		stream, openErr = provider.Open(ctx, dispatch)
	}
	// NoteOpen AFTER Open/OpenLoop/recovery so Walker attempts/committed are populated
	// on both success and failure. watchSession also Notes after Open when enabled.
	if diag != nil {
		diag.NoteOpen(provider)
	}
	if rec := sessionRecorderFrom(ctx); rec != nil {
		rec.NoteOpen(provider)
	}
	if in.WatchSession {
		stream = h.watchSession(ctx, provider, stream, openErr)
	}
	if openErr != nil {
		if errors.Is(openErr, resourcebudget.ErrPhysicalSendBudgetExceeded) {
			httpStatus = http.StatusTooManyRequests
			trace.Mark(timeline.StageUpstreamWaitHeaders, timeline.SideLocal, timeline.MilestoneDispatch, false, physicalSendBudgetErrorCode)
			return modelTurnResult{
				Status: "failed", Reason: physicalSendBudgetErrorCode, RequestID: requestID,
				RequestedModel: model, Provider: route.Provider, ResolvedModel: route.Model, HTTPStatus: httpStatus,
			}, openErr
		}
		httpStatus = 502
		trace.Mark(timeline.StageUpstreamWaitHeaders, timeline.SideUpstream, timeline.MilestoneDispatch, false, "provider_open_failed")
		if errors.Is(ctx.Err(), context.Canceled) {
			httpStatus = 499
			return modelTurnResult{Status: "cancelled", Reason: "cancelled", RequestID: requestID, RequestedModel: model, Provider: route.Provider, ResolvedModel: route.Model, HTTPStatus: httpStatus}, nil
		}
		return modelTurnResult{Status: "failed", Reason: clipFabricReason(openErr.Error()), RequestID: requestID, RequestedModel: model, Provider: route.Provider, ResolvedModel: route.Model, HTTPStatus: httpStatus}, openErr
	}
	trace.Mark(timeline.StageUpstreamWaitHeaders, timeline.SideUpstream, timeline.MilestoneDispatch, true, "")
	if proto := servingProtocolFromProvider(provider); proto != "" {
		servingProtocol = proto
	}
	defer stream.Close()
	var streamPhysical *providercontract.PhysicalPin
	if reporter, ok := stream.(interface{ PhysicalOwnership() *providercontract.PhysicalPin }); ok {
		streamPhysical = reporter.PhysicalOwnership()
	}

	b := bridge.New(route.Model, bridge.Options{
		Tools:                req.Context.Tools,
		ToolChoice:           req.Options.ToolChoice,
		ParallelToolCalls:    req.Options.ParallelToolCalls,
		HideThinkingSummary:  req.Options.HideThinkingSummary,
		RequestedServiceTier: req.Options.ServiceTier,
		CompactionRequest:    in.RoutedCompaction,
	})
	bindPolicyOutgoingID(provider, b.ResponseID())
	if rec := sessionRecorderFrom(ctx); rec != nil {
		rec.SetOutgoingID(b.ResponseID())
	}

	attachResolved := func(out modelTurnResult) modelTurnResult {
		resolvedCopy := resolved
		out.Resolved = &resolvedCopy
		out.Route = route
		if streamPhysical != nil {
			pin := *streamPhysical
			out.PhysicalPin = &pin
		} else if in.PhysicalPin != nil {
			pin := *in.PhysicalPin
			out.PhysicalPin = &pin
		}
		return out
	}
	if in.EmitFrames != nil {
		out, err := h.consumeModelTurnStream(ctx, in, b, stream, turn, trace, route, model, requestID, servingProtocol, &httpStatus)
		return attachResolved(out), err
	}
	out, err := h.consumeModelTurnCollect(ctx, b, stream, turn, route, model, requestID, servingProtocol, &httpStatus)
	return attachResolved(out), err
}

func (h *handler) consumeModelTurnStream(
	ctx context.Context,
	in modelTurnInput,
	b *bridge.Bridge,
	stream EventStream,
	turn *resourcebudget.Turn,
	trace *timeline.Trace,
	route router.Route,
	model, requestID, servingProtocol string,
	httpStatus *int,
) (modelTurnResult, error) {
	out := modelTurnResult{
		RequestID:       requestID,
		RequestedModel:  model,
		Provider:        route.Provider,
		ResolvedModel:   route.Model,
		HTTPStatus:      200,
		ServingProtocol: servingProtocol,
	}
	if err := in.EmitFrames(b.Start()); err != nil {
		markTrace(trace, timeline.StageDownstreamWrite, timeline.SideDownstream, "", false, "downstream_write")
		*httpStatus = 499
		out.Status = "cancelled"
		out.Reason = "downstream_write"
		out.HTTPStatus = *httpStatus
		return out, nil
	}
	firstText := false
	for {
		if err := ctx.Err(); err != nil {
			markTrace(trace, timeline.StageClientCancel, timeline.SideClient, "", false, "client_cancel")
			*httpStatus = 499
			out.Status = "cancelled"
			out.Reason = "cancelled"
			out.HTTPStatus = *httpStatus
			return out, nil
		}
		event, err := stream.Next()
		if errors.Is(err, io.EOF) {
			markTrace(trace, timeline.StageUpstreamRead, timeline.SideUpstream, timeline.MilestoneUpstreamEnd, true, "")
			if err := in.EmitFrames(b.End()); err == nil {
				markTrace(trace, timeline.StageDownstreamWrite, timeline.SideDownstream, timeline.MilestoneDownstreamEnd, true, "")
			}
			if turn != nil {
				turn.MarkCommitted()
			}
			out.Status = "completed"
			out.HTTPStatus = *httpStatus
			return out, nil
		}
		if err != nil {
			frames, _ := b.Handle(protocol.Event{Type: protocol.EventError, Message: "provider stream failed"})
			_ = in.EmitFrames(frames)
			*httpStatus = 502
			out.Status = "failed"
			out.Reason = "upstream_error"
			out.HTTPStatus = *httpStatus
			return out, nil
		}
		if event.Type == protocol.EventDone {
			if id := nativeResponseIDFromEvent(event); id != "" {
				out.NativeResponseID = id
			}
			if event.Usage != nil {
				out.Usage = event.Usage
				if diag := diagnosticsRecorderFrom(ctx); diag != nil {
					diag.NoteUsage(event.Usage)
				}
			}
		}
		frames, bridgeErr := b.Handle(event)
		if err := in.EmitFrames(frames); err != nil {
			markTrace(trace, timeline.StageDownstreamWrite, timeline.SideDownstream, "", false, "downstream_write")
			*httpStatus = 499
			out.Status = "cancelled"
			out.Reason = "downstream_write"
			out.HTTPStatus = *httpStatus
			return out, nil
		}
		if !firstText && event.Type == protocol.EventTextDelta && event.Text != "" {
			firstText = true
			markTrace(trace, timeline.StageUpstreamRead, timeline.SideUpstream, timeline.MilestoneTTFT, true, "")
		}
		if bridgeErr != nil {
			cause := "translator"
			if event.Validate() != nil {
				cause = "malformed_frame"
			}
			markTrace(trace, timeline.StageRelayTransform, timeline.SideRelay, "", false, cause)
			*httpStatus = 502
			out.Status = "failed"
			out.Reason = cause
			out.HTTPStatus = *httpStatus
			return out, nil
		}
	}
}

func (h *handler) consumeModelTurnCollect(
	ctx context.Context,
	b *bridge.Bridge,
	stream EventStream,
	turn *resourcebudget.Turn,
	route router.Route,
	model, requestID, servingProtocol string,
	httpStatus *int,
) (modelTurnResult, error) {
	_ = b.Start()
	var (
		failed           bool
		outputText       strings.Builder
		truncated        bool
		outputSealed     bool
		termUsage        *protocol.Usage
		terminal         map[string]any
		toolCalls        []modelTurnToolCall
		nativeResponseID string
	)
	for {
		event, nextErr := stream.Next()
		if errors.Is(nextErr, io.EOF) {
			for _, frame := range b.End() {
				if response, ok := frame.Data["response"].(map[string]any); ok {
					terminal = response
				}
			}
			break
		}
		if nextErr != nil {
			if errors.Is(ctx.Err(), context.Canceled) {
				*httpStatus = 499
				return modelTurnResult{
					Status: "cancelled", Reason: "cancelled", RequestID: requestID,
					RequestedModel: model, Provider: route.Provider, ResolvedModel: route.Model,
					OutputText: outputText.String(), Truncated: truncated, HTTPStatus: *httpStatus,
				}, nil
			}
			failed = true
			frames, _ := b.Handle(protocol.Event{Type: protocol.EventError, Message: "provider stream failed"})
			for _, frame := range frames {
				if response, ok := frame.Data["response"].(map[string]any); ok {
					terminal = response
				}
			}
			break
		}
		if event.Type == protocol.EventError {
			failed = true
		}
		if event.Type == protocol.EventTextDelta && event.Text != "" {
			if !outputSealed {
				cur := outputText.String() + event.Text
				clamped, wasTrunc := clampFabricResultOutput(cur)
				outputText.Reset()
				outputText.WriteString(clamped)
				if wasTrunc {
					// Once any model-visible text is omitted at the per-item cap,
					// seal collection so later deltas cannot refill leftover
					// UTF-8-safe capacity and break exact-prefix of the original.
					truncated = true
					outputSealed = true
				}
			}
			// When sealed, still fall through: bridge, usage, errors, cancel, terminal.
		}
		if event.Type == protocol.EventDone {
			if id := nativeResponseIDFromEvent(event); id != "" {
				nativeResponseID = id
			}
			if event.Usage != nil {
				termUsage = event.Usage
				if diag := diagnosticsRecorderFrom(ctx); diag != nil {
					diag.NoteUsage(event.Usage)
				}
			}
		}
		frames, bridgeErr := b.Handle(event)
		for _, frame := range frames {
			if frame.Name == "response.output_item.done" {
				if item, ok := frame.Data["item"].(map[string]any); ok {
					if tc, ok := modelTurnToolCallFromItem(item); ok {
						toolCalls = append(toolCalls, tc)
					}
				}
			}
			if frame.Name == "response.completed" || frame.Name == "response.incomplete" || frame.Name == "response.failed" {
				if response, ok := frame.Data["response"].(map[string]any); ok {
					terminal = response
				}
				if frame.Name == "response.failed" {
					failed = true
				}
			}
		}
		if bridgeErr != nil {
			failed = true
			break
		}
		if errors.Is(ctx.Err(), context.Canceled) {
			*httpStatus = 499
			return modelTurnResult{
				Status: "cancelled", Reason: "cancelled", RequestID: requestID,
				RequestedModel: model, Provider: route.Provider, ResolvedModel: route.Model,
				OutputText: outputText.String(), Truncated: truncated, HTTPStatus: *httpStatus,
			}, nil
		}
		if terminal != nil {
			break
		}
	}
	if len(toolCalls) == 0 && terminal != nil {
		toolCalls = modelTurnToolCallsFromResponse(terminal)
	}
	out := modelTurnResult{
		RequestID:        requestID,
		RequestedModel:   model,
		Provider:         route.Provider,
		ResolvedModel:    route.Model,
		OutputText:       outputText.String(),
		Truncated:        truncated,
		Usage:            termUsage,
		TerminalResponse: terminal,
		ToolCalls:        toolCalls,
		HTTPStatus:       *httpStatus,
		ServingProtocol:  servingProtocol,
		NativeResponseID: nativeResponseID,
	}
	if failed {
		*httpStatus = 502
		out.Status = "failed"
		out.Reason = "upstream_error"
		out.HTTPStatus = *httpStatus
		return out, nil
	}
	if turn != nil {
		turn.MarkCommitted()
	}
	out.Status = "completed"
	return out, nil
}

func (h *handler) recordInternalTurnTelemetry(ctx context.Context, path, method string, start time.Time, diag *diagnosticsRecorder, trace *timeline.Trace, status int, protocolLabel string) {
	if h == nil {
		return
	}
	if h.diagnostics == nil {
		h.diagnostics = newRequestTelemetryState()
	}
	if status == 0 {
		status = 200
	}
	if protocolLabel == "" {
		protocolLabel = "fabric-execute"
	}
	duration := time.Since(start).Milliseconds()
	record := requestTelemetryRecord{
		Timestamp:  start.UTC(),
		Method:     method,
		Path:       path,
		Status:     status,
		DurationMs: duration,
		Protocol:   protocolLabel,
	}
	if diag != nil {
		record.RequestID = diag.requestID
		record.CorrelationID = clipTelemetryString(diag.correlationID, diagnosticsStringMax)
		record.LegacyID = clipTelemetryString(diag.legacyID, diagnosticsStringMax)
		record.RequestedServiceTier = diag.clientRequestedServiceTier()
		record.ConfiguredServiceTier = clipTelemetryString(admittedServiceTier(ctx), diagnosticsStringMax)
		if routing, attempts := diag.snapshot(); routing != (diagnosticsRouting{}) || len(attempts) > 0 {
			record.Routing = routing
			record.Attempts = copyTelemetryAttempts(attempts)
		}
	}
	if record.RequestID == "" {
		record.RequestID = sessions.NewRequestID()
	}
	if trace != nil {
		events, timing, failure, errorCode := copyTelemetryTimeline(trace.Events())
		timing.TotalMs = duration
		record.Timeline = events
		record.Timing = timing
		record.Failure = failure
		record.ErrorCode = errorCode
		route := trace.Route()
		if route.ProviderConnection != "" {
			record.Routing.Provider = clipTelemetryString(route.ProviderConnection, diagnosticsStringMax)
		}
		if route.Model != "" {
			record.Routing.ResolvedModel = clipTelemetryString(route.Model, diagnosticsStringMax)
		}
	} else {
		record.Timing.TotalMs = duration
	}
	if diag != nil {
		capturedUsage := diag.Usage()
		record.Usage = projectDiagnosticsUsage(capturedUsage)
		if capturedUsage != nil {
			record.ResponseServiceTier = clipTelemetryString(strings.TrimSpace(capturedUsage.ServiceTier), diagnosticsStringMax)
			record.Cost = projectDiagnosticsCost(record.Routing.Provider, record.Routing.ResolvedModel, start, capturedUsage, overlaysForRequest(diag, nil))
		}
	}
	record.SidecarPolicy = diag.finalSidecarPolicyEvidence(harnessIdentityFromContext(ctx))
	h.diagnostics.append(record)
	h.appendInternalUsageLog(record, diag)
}

func (h *handler) appendInternalUsageLog(record requestTelemetryRecord, diag *diagnosticsRecorder) {
	if h == nil || h.usageLedger == nil || diag == nil || !diag.wasAdmitted() {
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
		Surface:        usage.CanonicalSurface(diag.surface),
	}
	if item.Provider == "" {
		item.Provider = usage.ClipField(record.Routing.RequestedProvider)
	}
	if item.Model == "" {
		item.Model = usage.ClipField(record.Routing.RequestedModel)
	}
	diag.mu.Lock()
	item.Account = usage.ClipField(diag.account)
	diag.mu.Unlock()
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

// clampFabricResultOutput is the single authoritative bound for Fabric result
// text used by collect and the result store. It preserves exact whitespace
// (no TrimSpace) and truncates to fabricResultMaxPerItem without splitting a
// UTF-8 code point.
func clampFabricResultOutput(value string) (string, bool) {
	if len(value) <= fabricResultMaxPerItem {
		return value, false
	}
	max := fabricResultMaxPerItem
	for max > 0 && !utf8.RuneStart(value[max]) {
		max--
	}
	return value[:max], true
}

func modelTurnToolCallFromItem(item map[string]any) (modelTurnToolCall, bool) {
	if item == nil {
		return modelTurnToolCall{}, false
	}
	typ, _ := item["type"].(string)
	if typ != "function_call" {
		return modelTurnToolCall{}, false
	}
	name, _ := item["name"].(string)
	if strings.TrimSpace(name) == "" {
		return modelTurnToolCall{}, false
	}
	status, _ := item["status"].(string)
	if status != "" && status != "completed" {
		return modelTurnToolCall{}, false
	}
	id, _ := item["id"].(string)
	callID, _ := item["call_id"].(string)
	if callID == "" {
		callID = id
	}
	args, _ := item["arguments"].(string)
	tc := modelTurnToolCall{ID: id, CallID: callID, Name: name, Arguments: args}
	tc.ThoughtSignature, tc.ProviderMetadata = fabricProviderMetadataFromItem(item)
	return tc, true
}

// fabricProviderMetadataFromItem extracts memory-only Google ThoughtSignature /
// ProviderOpaqueMetadata from a bridge function_call item. Client-forged values
// are not trusted beyond what the provider event path already emitted.
func fabricProviderMetadataFromItem(item map[string]any) (string, *protocol.ProviderOpaqueMetadata) {
	if item == nil {
		return "", nil
	}
	if sig, ok := item["thoughtSignature"].(string); ok {
		sig = strings.TrimSpace(sig)
		if sig != "" && !strings.HasPrefix(sig, "fc_") {
			return sig, &protocol.ProviderOpaqueMetadata{Google: &protocol.GoogleOpaqueMetadata{ThoughtSignature: sig}}
		}
	}
	raw, ok := item["extra_content"]
	if !ok {
		raw, ok = item["extraContent"]
	}
	if !ok {
		return "", nil
	}
	var b []byte
	switch v := raw.(type) {
	case json.RawMessage:
		b = v
	case []byte:
		b = v
	case string:
		b = []byte(v)
	case map[string]any:
		b, _ = json.Marshal(v)
	default:
		return "", nil
	}
	return decodeFabricGoogleThoughtSignature(b)
}

func decodeFabricGoogleThoughtSignature(raw []byte) (string, *protocol.ProviderOpaqueMetadata) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return "", nil
	}
	var root map[string]json.RawMessage
	if json.Unmarshal(raw, &root) != nil {
		return "", nil
	}
	googleRaw, ok := root["google"]
	if !ok {
		return "", nil
	}
	var google map[string]json.RawMessage
	if json.Unmarshal(googleRaw, &google) != nil {
		return "", nil
	}
	for _, key := range []string{"thoughtSignature", "thought_signature"} {
		sigRaw, ok := google[key]
		if !ok {
			continue
		}
		var sig string
		if json.Unmarshal(sigRaw, &sig) != nil {
			continue
		}
		sig = strings.TrimSpace(sig)
		if sig == "" || strings.HasPrefix(sig, "fc_") || len(sig) > 64*1024 {
			continue
		}
		return sig, &protocol.ProviderOpaqueMetadata{Google: &protocol.GoogleOpaqueMetadata{ThoughtSignature: sig}}
	}
	return "", nil
}

func modelTurnToolCallsFromResponse(response map[string]any) []modelTurnToolCall {
	if response == nil {
		return nil
	}
	raw, ok := response["output"].([]any)
	if !ok {
		return nil
	}
	out := make([]modelTurnToolCall, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if tc, ok := modelTurnToolCallFromItem(m); ok {
			out = append(out, tc)
		}
	}
	return out
}

func servingProtocolFromProvider(provider Provider) string {
	// Do not use Provider as a map key: many test and production wrappers are
	// non-comparable (e.g. function-typed providers), and that panics under
	// recover into a generic /v1/responses 500.
	const maxDepth = 16
	cur := provider
	for depth := 0; cur != nil && depth < maxDepth; depth++ {
		if reporter, ok := cur.(interface{ Protocol() string }); ok {
			if proto := strings.TrimSpace(reporter.Protocol()); proto != "" {
				return proto
			}
		}
		switch typed := cur.(type) {
		case attributedProvider:
			cur = typed.Provider
		case timedPolicyProvider:
			cur = typed.inner
		case committedOpenProvider:
			cur = typed.inner
		default:
			return ""
		}
	}
	return ""
}

// committedOpenProvider forces Open through OpenCommitted so combo/policy cannot
// hop after the primary turn already committed a physical member. Used when
// PreferCommitted continues through recovery loops that call Provider.Open.
type committedOpenProvider struct {
	inner Provider
}

func (p committedOpenProvider) Open(ctx context.Context, dispatch providercontract.DispatchRequest) (EventStream, error) {
	dispatch.PreferCommitted = true
	return reopenProvider(ctx, p.inner, dispatch)
}

func (p committedOpenProvider) OpenCommitted(ctx context.Context, dispatch providercontract.DispatchRequest) (EventStream, error) {
	dispatch.PreferCommitted = true
	return reopenProvider(ctx, p.inner, dispatch)
}

func (p committedOpenProvider) Protocol() string {
	return servingProtocolFromProvider(p.inner)
}

func (p committedOpenProvider) CommittedID() string {
	inner := unwrapSessionProvider(p.inner)
	if member, ok := inner.(interface{ CommittedID() string }); ok {
		return member.CommittedID()
	}
	return ""
}

func nativeResponseIDFromEvent(event protocol.Event) string {
	if len(event.ProviderState) == 0 {
		return ""
	}
	raw, ok := event.ProviderState["openai_responses_previous"]
	if !ok {
		return ""
	}
	var body struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(raw, &body) != nil {
		return ""
	}
	return strings.TrimSpace(body.ID)
}
