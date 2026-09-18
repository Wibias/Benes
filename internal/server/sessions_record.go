package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Wibias/Benes/internal/combo"
	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/protocol"
	providercontract "github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/router"
	"github.com/Wibias/Benes/internal/sessions"
	"github.com/Wibias/Benes/internal/timeline"
	"github.com/Wibias/Benes/internal/usage"
)

type sessionRecorderKey struct{}

type sessionRecorder struct {
	mu             sync.Mutex
	in             sessions.RecordInput
	identityLocked bool
	trace          *timeline.Trace
	sessionID      string
	prices         *priceOverlaySnapshot
}

func (rec *sessionRecorder) SessionID() string {
	if rec == nil {
		return ""
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return rec.sessionID
}

func isSessionPath(path string) bool {
	return path == "/v1/responses" || path == chatCompletionsPath || path == anthropicMessagesPath
}

func protocolFromPath(path string) string {
	switch path {
	case "/v1/responses":
		return sessions.ProtocolResponses
	case chatCompletionsPath:
		return sessions.ProtocolChat
	case anthropicMessagesPath:
		return sessions.ProtocolAnthropic
	default:
		return ""
	}
}

func withSessionRecorder(ctx context.Context, rec *sessionRecorder) context.Context {
	if rec == nil {
		return ctx
	}
	return context.WithValue(ctx, sessionRecorderKey{}, rec)
}

func sessionRecorderFrom(ctx context.Context) *sessionRecorder {
	if ctx == nil {
		return nil
	}
	rec, _ := ctx.Value(sessionRecorderKey{}).(*sessionRecorder)
	return rec
}

func (h *handler) beginSession(r *http.Request, started time.Time, trace *timeline.Trace) *sessionRecorder {
	if h == nil || h.sessions == nil || r == nil || !isSessionPath(r.URL.Path) {
		return nil
	}
	protocol := protocolFromPath(r.URL.Path)
	requestID := sessions.NewRequestID()
	var prices *priceOverlaySnapshot
	if diag := diagnosticsRecorderFrom(r.Context()); diag != nil {
		if diag.requestID != "" {
			requestID = diag.requestID
		}
		prices = diag.prices
	}
	rec := &sessionRecorder{
		in: sessions.RecordInput{
			RequestID:          requestID,
			CorrelationID:      strings.TrimSpace(r.Header.Get("X-Request-ID")),
			StartedAt:          started.UTC(),
			Protocol:           protocol,
			Method:             r.Method,
			Path:               r.URL.Path,
			SeedResponsesChain: false,
		},
		trace:  trace,
		prices: prices,
	}
	if r.ContentLength > 0 {
		n := r.ContentLength
		rec.in.RequestBytes = &n
	}
	identity := sessions.Identify(sessions.IdentifyInput{Protocol: protocol, Header: r.Header})
	if identity.Groupable() {
		rec.in.Identity = identity
		rec.identityLocked = true
	}
	return rec
}

func (rec *sessionRecorder) SetParsed(req protocol.ParsedRequest) {
	if rec == nil {
		return
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if req.ModelID != "" && rec.in.Routing.RequestedModel == "" {
		rec.in.Routing.RequestedModel = req.ModelID
	}
	if prev := req.PreviousResponseID; prev != "" {
		rec.in.PreviousResponseID = prev
	}
	if len(req.Raw) > 0 {
		n := int64(len(req.Raw))
		rec.in.RequestBytes = &n
	}
	if rec.identityLocked {
		return
	}
	identity := sessions.Identify(sessions.IdentifyInput{Protocol: rec.in.Protocol, Metadata: req.Options.Metadata})
	if identity.Groupable() {
		rec.in.Identity = identity
		rec.identityLocked = true
	}
}

func (rec *sessionRecorder) SetRequestedModel(model string) {
	if rec == nil {
		return
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if rec.in.Routing.RequestedModel == "" {
		rec.in.Routing.RequestedModel = model
	}
}

func (rec *sessionRecorder) SetRoute(requestedModel string, route router.Route) {
	if rec == nil {
		return
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if requestedModel != "" {
		rec.in.Routing.RequestedModel = requestedModel
	}
	rec.in.Routing.RequestedProvider = route.Provider
	switch route.Provider {
	case comboNamespace:
		rec.in.Routing.Kind = "combo"
		rec.in.Routing.ComboID = route.Model
	case policyNamespace:
		rec.in.Routing.Kind = "policy"
		rec.in.Routing.PolicyID = route.Model
	default:
		rec.in.Routing.Kind = "direct"
	}
}

func (rec *sessionRecorder) SetOutgoingID(id string) {
	if rec == nil {
		return
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if rec.in.OutgoingResponseID == "" {
		rec.in.OutgoingResponseID = id
		if rec.in.Protocol == sessions.ProtocolResponses && strings.TrimSpace(id) != "" {
			rec.in.SeedResponsesChain = true
		}
	}
}

func (rec *sessionRecorder) NoteOpen(provider Provider) {
	if rec == nil {
		return
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	inner := unwrapSessionProvider(provider)
	if member, ok := inner.(interface{ CommittedID() string }); ok {
		if id := member.CommittedID(); id != "" {
			rec.in.Routing.CommittedMember = id
		}
	}
	if src, ok := inner.(interface{ Attempts() []combo.Attempt }); ok {
		rec.in.Attempts = copySessionAttempts(src.Attempts())
	}
}

func (rec *sessionRecorder) NoteUsage(src *protocol.Usage) {
	if rec == nil || src == nil || src.Estimated {
		return
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if rec.in.Usage != nil {
		return
	}
	usage := &sessions.Usage{}
	in, out := src.InputTokens, src.OutputTokens
	usage.InputTokens = &in
	usage.OutputTokens = &out
	if src.CachedInputTokens > 0 {
		v := src.CachedInputTokens
		usage.CachedInputTokens = &v
	} else if src.CacheReadInputTokens > 0 {
		v := src.CacheReadInputTokens
		usage.CachedInputTokens = &v
	}
	if src.TotalTokens > 0 {
		v := src.TotalTokens
		usage.TotalTokens = &v
	} else {
		total := src.InputTokens + src.OutputTokens
		usage.TotalTokens = &total
	}
	rec.in.Usage = usage
}

func (rec *sessionRecorder) NoteUpstreamConversation(stateKey, conversationID string) {
	if rec == nil {
		return
	}
	identity := sessions.UpstreamConversation(stateKey, conversationID)
	if !identity.Groupable() {
		return
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if rec.identityLocked {
		return
	}
	rec.in.Identity = identity
	rec.identityLocked = true
}

func (h *handler) finishSession(rec *sessionRecorder, capture *statusCapture, started time.Time) {
	if h == nil || rec == nil || h.sessions == nil {
		return
	}
	rec.mu.Lock()
	in := rec.in
	trace := rec.trace
	rec.mu.Unlock()
	in.Status = finalObservedStatus(capture)
	in.Duration = time.Since(started)
	if capture != nil && capture.bytes > 0 {
		n := capture.bytes
		in.ResponseBytes = &n
	}
	if trace != nil {
		route := trace.Route()
		if route.ProviderConnection != "" {
			in.Routing.Provider = route.ProviderConnection
		}
		if route.Model != "" {
			in.Routing.ResolvedModel = route.Model
		}
	}
	h.attachExactSessionCost(&in, rec.prices)
	if in.Protocol == sessions.ProtocolResponses && strings.TrimSpace(in.OutgoingResponseID) != "" {
		in.SeedResponsesChain = true
	} else {
		in.SeedResponsesChain = false
	}
	if sessionID, err := h.sessions.Record(in); err != nil {
		if trace != nil {
			trace.Mark(timeline.StageTerminalDelivery, timeline.SideLocal, "", false, "session_persist_failed")
		}
		if h.debug != nil {
			h.debug.appendProvider("sessions persist failed")
		}
	} else {
		rec.mu.Lock()
		rec.sessionID = sessionID
		rec.mu.Unlock()
	}
}

func (h *handler) attachExactSessionCost(in *sessions.RecordInput, prices *priceOverlaySnapshot) {
	if h == nil || in == nil || in.Usage == nil || in.Usage.InputTokens == nil || in.Usage.OutputTokens == nil {
		return
	}
	provider := in.Routing.Provider
	model := in.Routing.ResolvedModel
	if provider == "" || model == "" {
		return
	}
	table := usage.DefaultTable()
	if overlays := prices.Get(); len(overlays) > 0 {
		table.AddOperatorOverlays(overlays)
	}
	resolved, ok := table.Lookup(provider, model, in.StartedAt.UTC().UnixMilli())
	if !ok || resolved.Confidence != usage.ConfidenceExact {
		return
	}
	tokens := usage.TokenUse{Input: *in.Usage.InputTokens, Output: *in.Usage.OutputTokens}
	if in.Usage.CachedInputTokens != nil {
		tokens.CacheRead = *in.Usage.CachedInputTokens
	}
	usd, conf := resolved.Estimate(tokens, "", "")
	if conf != usage.ConfidenceExact {
		return
	}
	in.Usage.Cost = &usd
	in.Usage.Currency = "USD"
}

// priceOverlaySnapshot is request-scoped. The first usage-bearing cost consumer
// loads overlays; later consumers on the same request reuse that snapshot.
type priceOverlaySnapshot struct {
	mu     sync.Mutex
	load   func() []usage.PriceRecord
	loaded bool
	value  []usage.PriceRecord
}

func (h *handler) newPriceOverlaySnapshot() *priceOverlaySnapshot {
	var load func() []usage.PriceRecord
	if h != nil && h.priceOverlayLoad != nil {
		load = h.priceOverlayLoad
	} else {
		path := ""
		if h != nil {
			path = h.configPath
		}
		load = func() []usage.PriceRecord { return loadPriceOverlaysFromDisk(path) }
	}
	return &priceOverlaySnapshot{load: load}
}

func (s *priceOverlaySnapshot) Get() []usage.PriceRecord {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.loaded {
		if s.load != nil {
			s.value = append([]usage.PriceRecord(nil), s.load()...)
		}
		s.loaded = true
	}
	return append([]usage.PriceRecord(nil), s.value...)
}

func loadPriceOverlaysFromDisk(path string) []usage.PriceRecord {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	disk, err := config.LoadDiskConfig(path, 0)
	if err != nil {
		return nil
	}
	return usage.OverlaysFromDiskProviders(disk.Providers)
}

func (h *handler) watchSession(ctx context.Context, provider Provider, stream EventStream, err error) EventStream {
	rec := sessionRecorderFrom(ctx)
	diag := diagnosticsRecorderFrom(ctx)
	if rec != nil {
		rec.NoteOpen(provider)
	}
	if diag != nil {
		diag.NoteOpen(provider)
	}
	if stream == nil {
		return stream
	}
	if rec == nil && diag == nil {
		return stream
	}
	return &sessionStream{inner: stream, rec: rec, diag: diag}
}

type sessionStream struct {
	inner EventStream
	rec   *sessionRecorder
	diag  *diagnosticsRecorder
}

func (s *sessionStream) Next() (protocol.Event, error) {
	if s == nil || s.inner == nil {
		return protocol.Event{}, io.EOF
	}
	event, err := s.inner.Next()
	if s.diag != nil && event.Usage != nil {
		s.diag.NoteUsage(event.Usage)
	}
	if s.rec != nil {
		if event.Usage != nil {
			s.rec.NoteUsage(event.Usage)
		}
		for key, raw := range event.ProviderState {
			var payload struct {
				ConversationID string `json:"conversationId"`
			}
			if json.Unmarshal(raw, &payload) == nil {
				s.rec.NoteUpstreamConversation(key, payload.ConversationID)
			}
		}
	}
	return event, err
}

func (s *sessionStream) Close() error {
	if s == nil || s.inner == nil {
		return nil
	}
	return s.inner.Close()
}

// PhysicalOwnership forwards through session observation. watchSession wraps
// before runModelTurn capture when WatchSession is set.
func (s *sessionStream) PhysicalOwnership() *providercontract.PhysicalPin {
	if s == nil || s.inner == nil {
		return nil
	}
	if reporter, ok := s.inner.(interface{ PhysicalOwnership() *providercontract.PhysicalPin }); ok {
		return reporter.PhysicalOwnership()
	}
	return nil
}

func unwrapSessionProvider(provider Provider) Provider {
	for {
		if attributed, ok := provider.(attributedProvider); ok {
			provider = attributed.Provider
			continue
		}
		if pinned, ok := provider.(committedOpenProvider); ok {
			provider = pinned.inner
			continue
		}
		return provider
	}
}

func copySessionAttempts(values []combo.Attempt) []sessions.Attempt {
	if len(values) == 0 {
		return nil
	}
	out := make([]sessions.Attempt, 0, len(values))
	for _, item := range values {
		out = append(out, sessions.Attempt{Member: item.Member, Status: item.Status, Code: item.Code, Decision: item.Decision})
	}
	return out
}
