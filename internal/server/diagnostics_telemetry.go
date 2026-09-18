package server

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/resourcebudget"
	"github.com/Wibias/Benes/internal/sessions"
	"github.com/Wibias/Benes/internal/timeline"
	"github.com/Wibias/Benes/internal/usage"
)

const (
	diagnosticsDefaultLimit     = 200
	diagnosticsMaxLimit         = requestLogMax
	diagnosticsStringMax        = 256
	diagnosticsCauseMax         = 64
	diagnosticsAttemptMax       = 16
	diagnosticsPhysicalSendMax  = 64
	diagnosticsTimelineEventMax = 16
)

type requestTelemetryState struct {
	mu         sync.Mutex
	generation string
	nextSeq    uint64
	records    []requestTelemetryRecord
}

type requestTelemetryRecord struct {
	Seq           uint64
	RequestID     string
	CorrelationID string
	LegacyID      string
	Timestamp     time.Time
	SessionID     string
	Protocol      string
	Method        string
	Path          string
	Status        int
	DurationMs    int64
	RequestBytes  *int64
	ResponseBytes *int64
	Routing       diagnosticsRouting
	Attempts      []diagnosticsAttempt
	PhysicalSends []diagnosticsPhysicalSend
	Timing        diagnosticsTiming
	Timeline      []diagnosticsTimelineEvent
	Failure       *diagnosticsFailure
	Usage         *diagnosticsUsage
	Cost          *diagnosticsCost
	ErrorCode     string
	// Service-tier evidence: the tier the client asked for, the tier the canonical
	// Benes request policy configured for this request, and the tier the provider
	// reported back. Configured and observed must stay distinct facts.
	RequestedServiceTier  string
	ConfiguredServiceTier string
	ResponseServiceTier   string
	// SidecarPolicy is the terminal per-request sidecar decision evidence. It is
	// attached to the usage record routeDecision and is not part of the legacy
	// request-log projection.
	SidecarPolicy *sidecarPolicyEvidence
}

type diagnosticsRouting struct {
	Kind              string `json:"kind,omitempty"`
	RequestedModel    string `json:"requestedModel,omitempty"`
	RequestedProvider string `json:"requestedProvider,omitempty"`
	ResolvedModel     string `json:"resolvedModel,omitempty"`
	Provider          string `json:"provider,omitempty"`
	PolicyID          string `json:"policyId,omitempty"`
	ComboID           string `json:"comboId,omitempty"`
	CommittedMember   string `json:"committedMember,omitempty"`
}

type diagnosticsAttempt struct {
	Ordinal  int    `json:"ordinal"`
	Member   string `json:"member,omitempty"`
	Status   int    `json:"status,omitempty"`
	Code     string `json:"code,omitempty"`
	Decision string `json:"decision,omitempty"`
}

type diagnosticsPhysicalSend struct {
	Ordinal int    `json:"ordinal"`
	Reason  string `json:"reason,omitempty"`
}

type diagnosticsTiming struct {
	TotalMs           int64  `json:"totalMs"`
	HeadersMs         *int64 `json:"headersMs,omitempty"`
	FirstByteMs       *int64 `json:"firstByteMs,omitempty"`
	TTFTMs            *int64 `json:"ttftMs,omitempty"`
	FirstDownstreamMs *int64 `json:"firstDownstreamMs,omitempty"`
	UpstreamEndMs     *int64 `json:"upstreamEndMs,omitempty"`
	DownstreamEndMs   *int64 `json:"downstreamEndMs,omitempty"`
}

type diagnosticsTimelineEvent struct {
	Stage           string `json:"stage,omitempty"`
	Side            string `json:"side,omitempty"`
	Milestone       string `json:"milestone,omitempty"`
	ElapsedMs       int64  `json:"elapsedMs"`
	Attempt         int    `json:"attempt,omitempty"`
	OK              bool   `json:"ok"`
	NormalizedCause string `json:"normalizedCause,omitempty"`
}

type diagnosticsFailure struct {
	Side  string `json:"side,omitempty"`
	Stage string `json:"stage,omitempty"`
	Cause string `json:"cause,omitempty"`
}

type diagnosticsUsage struct {
	Status                   string `json:"status"`
	InputTokens              *int64 `json:"inputTokens,omitempty"`
	CachedInputTokens        *int64 `json:"cachedInputTokens,omitempty"`
	CacheReadInputTokens     *int64 `json:"cacheReadInputTokens,omitempty"`
	CacheCreationInputTokens *int64 `json:"cacheCreationInputTokens,omitempty"`
	OutputTokens             *int64 `json:"outputTokens,omitempty"`
	ReasoningOutputTokens    *int64 `json:"reasoningOutputTokens,omitempty"`
	TotalTokens              *int64 `json:"totalTokens,omitempty"`
	ContextTotalTokens       *int64 `json:"contextTotalTokens,omitempty"`
}

type diagnosticsCost struct {
	Kind     string                `json:"kind"`
	Currency string                `json:"currency,omitempty"`
	Total    *float64              `json:"total,omitempty"`
	Reason   string                `json:"reason,omitempty"`
	Price    *diagnosticsPriceInfo `json:"price,omitempty"`
}

type diagnosticsPriceInfo struct {
	Provider   string `json:"provider,omitempty"`
	ModelID    string `json:"modelId,omitempty"`
	Source     string `json:"source,omitempty"`
	VerifiedAt *int64 `json:"verifiedAt,omitempty"`
	Confidence string `json:"confidence,omitempty"`
}

type diagnosticsRequestSummary struct {
	RequestID     string `json:"requestId"`
	Timestamp     string `json:"timestamp"`
	SessionID     string `json:"sessionId,omitempty"`
	Protocol      string `json:"protocol,omitempty"`
	Method        string `json:"method"`
	Path          string `json:"path"`
	ResolvedModel string `json:"resolvedModel,omitempty"`
	Provider      string `json:"provider,omitempty"`
	Status        int    `json:"status"`
	DurationMs    int64  `json:"durationMs"`
	TotalTokens   *int64 `json:"totalTokens,omitempty"`
	UsageStatus   string `json:"usageStatus,omitempty"`
}

type diagnosticsRequestDetail struct {
	RequestID     string                     `json:"requestId"`
	CorrelationID string                     `json:"correlationId,omitempty"`
	SessionID     string                     `json:"sessionId,omitempty"`
	Timestamp     string                     `json:"timestamp"`
	Protocol      string                     `json:"protocol,omitempty"`
	Method        string                     `json:"method"`
	Path          string                     `json:"path"`
	Status        int                        `json:"status"`
	DurationMs    int64                      `json:"durationMs"`
	RequestBytes  *int64                     `json:"requestBytes,omitempty"`
	ResponseBytes *int64                     `json:"responseBytes,omitempty"`
	ErrorCode     string                     `json:"errorCode,omitempty"`
	Routing       *diagnosticsRouting        `json:"routing,omitempty"`
	Attempts      []diagnosticsAttempt       `json:"attempts,omitempty"`
	PhysicalSends []diagnosticsPhysicalSend  `json:"physicalSends,omitempty"`
	Timing        diagnosticsTiming          `json:"timing"`
	Timeline      []diagnosticsTimelineEvent `json:"timeline,omitempty"`
	Failure       *diagnosticsFailure        `json:"failure,omitempty"`
	Usage         *diagnosticsUsage          `json:"usage,omitempty"`
	Cost          *diagnosticsCost           `json:"cost,omitempty"`

	RequestedServiceTier  string `json:"requestedServiceTier,omitempty"`
	ConfiguredServiceTier string `json:"configuredServiceTier,omitempty"`
	ResponseServiceTier   string `json:"responseServiceTier,omitempty"`
}

type diagnosticsListResponse struct {
	Requests         []diagnosticsRequestSummary `json:"requests"`
	NextCursor       string                      `json:"nextCursor"`
	Reset            bool                        `json:"reset"`
	HistoryTruncated bool                        `json:"historyTruncated"`
}

type diagnosticsQuery struct {
	Cursor        string
	Limit         int
	SessionID     string
	Status        int
	HasStatus     bool
	Protocol      string
	Provider      string
	Model         string
	RouteKind     string
	RequestID     string
	CorrelationID string
}

func newRequestTelemetryState() *requestTelemetryState {
	return &requestTelemetryState{generation: newTelemetryGeneration(), records: make([]requestTelemetryRecord, 0, 64)}
}

func newTelemetryGeneration() string {
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(raw)
}

func (s *requestTelemetryState) append(record requestTelemetryRecord) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextSeq++
	record.Seq = s.nextSeq
	s.records = append(s.records, record)
	if len(s.records) > requestLogMax {
		s.records = s.records[len(s.records)-requestLogMax:]
	}
}

func (s *requestTelemetryState) legacyList(status, sessionID string, limit int) []requestLogEntry {
	if s == nil {
		return []requestLogEntry{}
	}
	if limit <= 0 {
		limit = diagnosticsDefaultLimit
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	filtered := s.records
	if status != "" {
		want, err := strconv.Atoi(status)
		if err == nil {
			out := make([]requestTelemetryRecord, 0, len(filtered))
			for _, record := range filtered {
				if record.Status == want {
					out = append(out, record)
				}
			}
			filtered = out
		}
	}
	if sessionID != "" {
		out := make([]requestTelemetryRecord, 0, len(filtered))
		for _, record := range filtered {
			if record.SessionID == sessionID {
				out = append(out, record)
			}
		}
		filtered = out
	}
	if len(filtered) > limit {
		filtered = filtered[len(filtered)-limit:]
	}
	out := make([]requestLogEntry, 0, len(filtered))
	for _, record := range filtered {
		out = append(out, record.legacy())
	}
	return out
}

func (s *requestTelemetryState) lookup(requestID string) (requestTelemetryRecord, bool) {
	if s == nil {
		return requestTelemetryRecord{}, false
	}
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return requestTelemetryRecord{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.records) - 1; i >= 0; i-- {
		if s.records[i].RequestID == requestID {
			return s.records[i], true
		}
	}
	return requestTelemetryRecord{}, false
}

func (s *requestTelemetryState) query(q diagnosticsQuery) (diagnosticsListResponse, error) {
	if s == nil {
		return diagnosticsListResponse{Requests: []diagnosticsRequestSummary{}}, nil
	}
	if q.Limit <= 0 {
		q.Limit = diagnosticsDefaultLimit
	}
	if q.Limit > diagnosticsMaxLimit {
		q.Limit = diagnosticsMaxLimit
	}
	s.mu.Lock()
	generation := s.generation
	high := s.nextSeq
	var floor uint64
	if len(s.records) > 0 {
		floor = s.records[0].Seq
	}
	records := append([]requestTelemetryRecord(nil), s.records...)
	s.mu.Unlock()

	reset := false
	truncated := false
	after := uint64(0)
	if strings.TrimSpace(q.Cursor) != "" {
		gen, seq, err := decodeDiagnosticsCursor(q.Cursor)
		if err != nil {
			return diagnosticsListResponse{}, err
		}
		if gen != generation {
			reset = true
		} else if floor > 0 && seq+1 < floor {
			reset = true
			truncated = true
		} else {
			after = seq
		}
	}

	matched := make([]requestTelemetryRecord, 0, 16)
	if reset || q.Cursor == "" {
		for _, record := range records {
			if record.matches(q) {
				matched = append(matched, record)
			}
		}
		if len(matched) > q.Limit {
			matched = matched[len(matched)-q.Limit:]
		}
		return diagnosticsListResponse{
			Requests:         projectSummaries(matched),
			NextCursor:       encodeDiagnosticsCursor(generation, high),
			Reset:            reset,
			HistoryTruncated: truncated,
		}, nil
	}

	for _, record := range records {
		if record.Seq <= after {
			continue
		}
		if record.matches(q) {
			matched = append(matched, record)
			if len(matched) == q.Limit {
				break
			}
		}
	}
	next := high
	if len(matched) == q.Limit && matched[len(matched)-1].Seq < high {
		next = matched[len(matched)-1].Seq
	}
	return diagnosticsListResponse{
		Requests:         projectSummaries(matched),
		NextCursor:       encodeDiagnosticsCursor(generation, next),
		Reset:            false,
		HistoryTruncated: false,
	}, nil
}

func (s *requestTelemetryState) rotateGenerationForTest() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.generation = newTelemetryGeneration()
	s.nextSeq = 0
	s.records = s.records[:0]
}

func (record requestTelemetryRecord) matches(q diagnosticsQuery) bool {
	if q.SessionID != "" && record.SessionID != q.SessionID {
		return false
	}
	if q.HasStatus && record.Status != q.Status {
		return false
	}
	if q.Protocol != "" && record.Protocol != q.Protocol {
		return false
	}
	if q.Provider != "" && record.Routing.Provider != q.Provider {
		return false
	}
	if q.Model != "" && record.Routing.ResolvedModel != q.Model {
		return false
	}
	if q.RouteKind != "" && record.Routing.Kind != q.RouteKind {
		return false
	}
	if q.RequestID != "" && record.RequestID != q.RequestID {
		return false
	}
	if q.CorrelationID != "" && record.CorrelationID != q.CorrelationID {
		return false
	}
	return true
}

func (record requestTelemetryRecord) legacy() requestLogEntry {
	return requestLogEntry{
		ID:         record.LegacyID,
		Timestamp:  record.Timestamp.UTC().Format(time.RFC3339Nano),
		Method:     record.Method,
		Path:       record.Path,
		Status:     record.Status,
		DurationMs: record.DurationMs,
		SessionID:  record.SessionID,
	}
}

func (record requestTelemetryRecord) summary() diagnosticsRequestSummary {
	out := diagnosticsRequestSummary{
		RequestID:     record.RequestID,
		Timestamp:     record.Timestamp.UTC().Format(time.RFC3339Nano),
		SessionID:     record.SessionID,
		Protocol:      record.Protocol,
		Method:        record.Method,
		Path:          record.Path,
		ResolvedModel: record.Routing.ResolvedModel,
		Provider:      record.Routing.Provider,
		Status:        record.Status,
		DurationMs:    record.DurationMs,
	}
	if record.Usage != nil && record.Usage.Status != "unreported" {
		out.UsageStatus = record.Usage.Status
		out.TotalTokens = record.Usage.TotalTokens
	}
	return out
}

func (record requestTelemetryRecord) detail() diagnosticsRequestDetail {
	out := diagnosticsRequestDetail{
		RequestID:     record.RequestID,
		CorrelationID: record.CorrelationID,
		SessionID:     record.SessionID,
		Timestamp:     record.Timestamp.UTC().Format(time.RFC3339Nano),
		Protocol:      record.Protocol,
		Method:        record.Method,
		Path:          record.Path,
		Status:        record.Status,
		DurationMs:    record.DurationMs,
		RequestBytes:  record.RequestBytes,
		ResponseBytes: record.ResponseBytes,
		ErrorCode:     record.ErrorCode,
		Attempts:      record.Attempts,
		PhysicalSends: record.PhysicalSends,
		Timing:        record.Timing,
		Timeline:      record.Timeline,
		Failure:       record.Failure,
		Usage:         record.Usage,
		Cost:          record.Cost,

		RequestedServiceTier:  record.RequestedServiceTier,
		ConfiguredServiceTier: record.ConfiguredServiceTier,
		ResponseServiceTier:   record.ResponseServiceTier,
	}
	if out.Usage == nil {
		out.Usage = &diagnosticsUsage{Status: "unreported"}
	}
	if record.Routing != (diagnosticsRouting{}) {
		routing := record.Routing
		out.Routing = &routing
	}
	return out
}

func projectSummaries(records []requestTelemetryRecord) []diagnosticsRequestSummary {
	if records == nil {
		return []diagnosticsRequestSummary{}
	}
	out := make([]diagnosticsRequestSummary, 0, len(records))
	for _, record := range records {
		out = append(out, record.summary())
	}
	return out
}

func encodeDiagnosticsCursor(generation string, seq uint64) string {
	raw := "1|" + generation + "|" + strconv.FormatUint(seq, 10)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

var errInvalidDiagnosticsCursor = errors.New("invalid_cursor")

func decodeDiagnosticsCursor(cursor string) (string, uint64, error) {
	cursor = strings.TrimSpace(cursor)
	if cursor == "" {
		return "", 0, errInvalidDiagnosticsCursor
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return "", 0, errInvalidDiagnosticsCursor
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != 3 || parts[0] != "1" || parts[1] == "" {
		return "", 0, errInvalidDiagnosticsCursor
	}
	seq, err := strconv.ParseUint(parts[2], 10, 64)
	if err != nil {
		return "", 0, errInvalidDiagnosticsCursor
	}
	return parts[1], seq, nil
}

func clipTelemetryString(value string, max int) string {
	value = strings.TrimSpace(value)
	if max <= 0 || len(value) <= max {
		return value
	}
	for max > 0 && !utf8.RuneStart(value[max]) {
		max--
	}
	return value[:max]
}

func sanitizeNormalizedCause(cause string) string {
	cause = strings.TrimSpace(cause)
	if cause == "" || len(cause) > diagnosticsCauseMax {
		return ""
	}
	for i, r := range cause {
		if i == 0 {
			if !(unicode.IsLetter(r) && unicode.IsLower(r)) {
				return ""
			}
			continue
		}
		if unicode.IsLower(r) || unicode.IsDigit(r) || r == '_' {
			continue
		}
		return ""
	}
	return cause
}

func int64Value(v int64) *int64 {
	return &v
}

func float64Value(v float64) *float64 {
	return &v
}

func copyTelemetryAttempts(values []sessions.Attempt) []diagnosticsAttempt {
	if len(values) == 0 {
		return nil
	}
	if len(values) > diagnosticsAttemptMax {
		values = values[:diagnosticsAttemptMax]
	}
	out := make([]diagnosticsAttempt, 0, len(values))
	for i, item := range values {
		out = append(out, diagnosticsAttempt{
			Ordinal:  i + 1,
			Member:   clipTelemetryString(item.Member, diagnosticsStringMax),
			Status:   item.Status,
			Code:     sanitizeNormalizedCause(item.Code),
			Decision: clipTelemetryString(item.Decision, diagnosticsCauseMax),
		})
	}
	return out
}

func projectDiagnosticsPhysicalSends(values []resourcebudget.PhysicalSend) []diagnosticsPhysicalSend {
	if len(values) == 0 {
		return nil
	}
	if len(values) > diagnosticsPhysicalSendMax {
		values = values[:diagnosticsPhysicalSendMax]
	}
	out := make([]diagnosticsPhysicalSend, 0, len(values))
	for _, value := range values {
		out = append(out, diagnosticsPhysicalSend{
			Ordinal: value.Ordinal,
			Reason:  sanitizePhysicalSendReason(value.Reason),
		})
	}
	return out
}

func sanitizePhysicalSendReason(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	value = strings.ReplaceAll(value, "-", "_")
	if value == "" || len(value) > diagnosticsCauseMax {
		return ""
	}
	for i, r := range value {
		if i == 0 {
			if !(unicode.IsLetter(r) && unicode.IsLower(r)) {
				return ""
			}
			continue
		}
		if unicode.IsLower(r) || unicode.IsDigit(r) || r == '_' {
			continue
		}
		return ""
	}
	return value
}

func copyTelemetryTimeline(events []timeline.Event) ([]diagnosticsTimelineEvent, diagnosticsTiming, *diagnosticsFailure, string) {
	timing := diagnosticsTiming{}
	if len(events) > diagnosticsTimelineEventMax {
		events = events[:diagnosticsTimelineEventMax]
	}
	out := make([]diagnosticsTimelineEvent, 0, len(events))
	var failure *diagnosticsFailure
	errorCode := ""
	for _, event := range events {
		cause := sanitizeNormalizedCause(event.Cause)
		item := diagnosticsTimelineEvent{
			Stage:           string(event.Stage),
			Side:            string(event.Side),
			Milestone:       string(event.Milestone),
			ElapsedMs:       event.Elapsed.Milliseconds(),
			Attempt:         event.Attempt,
			OK:              event.OK,
			NormalizedCause: cause,
		}
		out = append(out, item)
		if event.OK {
			elapsed := event.Elapsed.Milliseconds()
			switch event.Milestone {
			case timeline.MilestoneHeaders:
				if timing.HeadersMs == nil {
					timing.HeadersMs = int64Value(elapsed)
				}
			case timeline.MilestoneFirstByte:
				if timing.FirstByteMs == nil {
					timing.FirstByteMs = int64Value(elapsed)
				}
			case timeline.MilestoneTTFT:
				if timing.TTFTMs == nil {
					timing.TTFTMs = int64Value(elapsed)
				}
			case timeline.MilestoneFirstDownstream:
				if timing.FirstDownstreamMs == nil {
					timing.FirstDownstreamMs = int64Value(elapsed)
				}
			case timeline.MilestoneUpstreamEnd:
				if timing.UpstreamEndMs == nil {
					timing.UpstreamEndMs = int64Value(elapsed)
				}
			case timeline.MilestoneDownstreamEnd:
				if timing.DownstreamEndMs == nil {
					timing.DownstreamEndMs = int64Value(elapsed)
				}
			}
		}
		if !event.OK {
			failure = &diagnosticsFailure{Side: string(event.Side), Stage: string(event.Stage), Cause: cause}
			if cause != "" {
				errorCode = cause
			}
		}
	}
	if len(out) == 0 {
		out = nil
	}
	return out, timing, failure, errorCode
}

func projectDiagnosticsUsage(src *protocol.Usage) *diagnosticsUsage {
	if src == nil {
		return nil
	}
	status := "reported"
	if src.Estimated {
		status = "estimated"
	}
	out := &diagnosticsUsage{Status: status}
	out.InputTokens = int64Value(src.InputTokens)
	out.OutputTokens = int64Value(src.OutputTokens)
	if src.CachedInputTokens > 0 {
		out.CachedInputTokens = int64Value(src.CachedInputTokens)
	}
	if src.CacheReadInputTokens > 0 {
		out.CacheReadInputTokens = int64Value(src.CacheReadInputTokens)
	}
	if src.CacheCreationInputTokens > 0 {
		out.CacheCreationInputTokens = int64Value(src.CacheCreationInputTokens)
	}
	if src.ReasoningOutputTokens > 0 {
		out.ReasoningOutputTokens = int64Value(src.ReasoningOutputTokens)
	}
	if src.ContextTotalTokens > 0 {
		out.ContextTotalTokens = int64Value(src.ContextTotalTokens)
	}
	total := src.TotalTokens
	if total <= 0 {
		total = src.InputTokens + src.OutputTokens
	}
	out.TotalTokens = int64Value(total)
	return out
}

func projectDiagnosticsCost(provider, model string, started time.Time, src *protocol.Usage, overlays []usage.PriceRecord) *diagnosticsCost {
	if src == nil {
		return nil
	}
	provider = clipTelemetryString(provider, diagnosticsStringMax)
	model = clipTelemetryString(model, diagnosticsStringMax)
	if provider == "" || model == "" {
		return &diagnosticsCost{Kind: "unavailable", Reason: "price_unmatched"}
	}
	table := usage.DefaultTable()
	if len(overlays) > 0 {
		table.AddOperatorOverlays(overlays)
	}
	resolved, ok := table.Lookup(provider, model, started.UTC().UnixMilli())
	if !ok {
		return &diagnosticsCost{Kind: "unavailable", Reason: "price_unmatched"}
	}
	if resolved.Currency != "" && !strings.EqualFold(resolved.Currency, "USD") {
		return &diagnosticsCost{Kind: "unavailable", Reason: "price_unmatched"}
	}
	tokens := usage.TokenUse{Input: src.InputTokens, Output: src.OutputTokens}
	if src.CacheReadInputTokens > 0 {
		tokens.CacheRead = src.CacheReadInputTokens
	} else if src.CachedInputTokens > 0 {
		tokens.CacheRead = src.CachedInputTokens
	}
	if src.CacheCreationInputTokens > 0 {
		tokens.CacheWrite = src.CacheCreationInputTokens
	}
	total, conf := resolved.Estimate(tokens, src.ServiceTier, "")
	kind := "exact"
	reason := ""
	if src.Estimated {
		kind = "estimated"
		reason = "usage_estimated"
	}
	switch conf {
	case usage.ConfidenceExact:
	case usage.ConfidenceEstimated, usage.ConfidenceLowerBound:
		kind = "estimated"
		if reason == "" {
			reason = string(conf)
		}
	case usage.ConfidenceStale:
		kind = "estimated"
		if reason == "" {
			reason = "stale_price"
		}
	default:
		return &diagnosticsCost{Kind: "unavailable", Reason: "price_unmatched"}
	}
	if kind == "exact" && conf != usage.ConfidenceExact {
		kind = "estimated"
	}
	source := strings.TrimSpace(resolved.SourceRef)
	if source == "" {
		source = string(resolved.SourceClass)
	}
	price := &diagnosticsPriceInfo{
		Provider:   clipTelemetryString(resolved.Provider, diagnosticsStringMax),
		ModelID:    clipTelemetryString(resolved.Model, diagnosticsStringMax),
		Source:     clipTelemetryString(source, diagnosticsStringMax),
		Confidence: string(conf),
	}
	if resolved.VerifiedAt > 0 {
		price.VerifiedAt = int64Value(resolved.VerifiedAt)
	}
	return &diagnosticsCost{
		Kind:     kind,
		Currency: "USD",
		Total:    float64Value(total),
		Reason:   reason,
		Price:    price,
	}
}

func routingFromSession(in sessions.RecordInput) diagnosticsRouting {
	return diagnosticsRouting{
		Kind:              clipTelemetryString(in.Routing.Kind, diagnosticsCauseMax),
		RequestedModel:    clipTelemetryString(in.Routing.RequestedModel, diagnosticsStringMax),
		RequestedProvider: clipTelemetryString(in.Routing.RequestedProvider, diagnosticsStringMax),
		ResolvedModel:     clipTelemetryString(in.Routing.ResolvedModel, diagnosticsStringMax),
		Provider:          clipTelemetryString(in.Routing.Provider, diagnosticsStringMax),
		PolicyID:          clipTelemetryString(in.Routing.PolicyID, diagnosticsStringMax),
		ComboID:           clipTelemetryString(in.Routing.ComboID, diagnosticsStringMax),
		CommittedMember:   clipTelemetryString(in.Routing.CommittedMember, diagnosticsStringMax),
	}
}

func routingFromTrace(route timeline.Route) diagnosticsRouting {
	if route == (timeline.Route{}) {
		return diagnosticsRouting{}
	}
	return diagnosticsRouting{
		RequestedProvider: clipTelemetryString(route.RequestedProvider, diagnosticsStringMax),
		Provider:          clipTelemetryString(route.ProviderConnection, diagnosticsStringMax),
		ResolvedModel:     clipTelemetryString(route.Model, diagnosticsStringMax),
	}
}
