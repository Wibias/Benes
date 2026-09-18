package outbound

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/resourcebudget"
	"github.com/Wibias/Benes/internal/timeline"
	"github.com/Wibias/Benes/internal/usage"
)

const (
	defaultMaxCallArgumentBytes = 2 * 1024 * 1024
	defaultMaxTurnBytes         = 32 * 1024 * 1024
)

var ErrNotTerminal = errors.New("chat completion is not terminal")

var fallbackIDSeq atomic.Uint64

type Options struct {
	IDGenerator          func() string
	CreatedUnix          int64
	MaxCallArgumentBytes int
	MaxTurnBytes         int
	Trace                *timeline.Trace
	Turn                 *resourcebudget.Turn
	RequestedServiceTier *string
}

type Frame struct {
	Payload map[string]any
	Done    bool
}

type TerminalError struct {
	Status  int
	Type    string
	Code    string
	Message string
}

func (e *TerminalError) Error() string { return e.Message }

type toolCall struct {
	index     int
	id        string
	name      string
	arguments string
	ended     bool
	emitted   bool
}

type Encoder struct {
	model         string
	id            string
	created       int64
	requestedTier string

	maxCallArgumentBytes int
	maxTurnBytes         int
	retainedBytes        int

	started      bool
	terminal     bool
	failure      *TerminalError
	finishReason string
	usage        *protocol.Usage

	content   strings.Builder
	reasoning strings.Builder
	details   []protocol.MiniMaxReasoningDetail
	detailKey map[string]int
	tools     map[string]*toolCall
	toolOrder []string
	citations []protocol.URLCitation
}

func New(model string, options Options) (*Encoder, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		return nil, fmt.Errorf("chat completion model is required")
	}
	if options.MaxCallArgumentBytes < 0 || options.MaxTurnBytes < 0 {
		return nil, fmt.Errorf("chat outbound byte limits cannot be negative")
	}
	maxCall := options.MaxCallArgumentBytes
	if maxCall == 0 {
		maxCall = defaultMaxCallArgumentBytes
	}
	maxTurn := options.MaxTurnBytes
	if maxTurn == 0 {
		maxTurn = defaultMaxTurnBytes
	}
	idGenerator := options.IDGenerator
	if idGenerator == nil {
		idGenerator = completionID
	}
	id := strings.TrimSpace(idGenerator())
	if id == "" {
		return nil, fmt.Errorf("chat completion id generator returned a blank id")
	}
	created := options.CreatedUnix
	if created == 0 {
		created = time.Now().Unix()
	}
	requestedTier := ""
	if options.RequestedServiceTier != nil {
		requestedTier = strings.TrimSpace(*options.RequestedServiceTier)
	}
	return &Encoder{
		model:                model,
		id:                   id,
		created:              created,
		requestedTier:        requestedTier,
		maxCallArgumentBytes: maxCall,
		maxTurnBytes:         maxTurn,
		tools:                make(map[string]*toolCall),
		detailKey:            make(map[string]int),
	}, nil
}

func (e *Encoder) Handle(event protocol.Event) ([]Frame, error) {
	if e.terminal {
		return nil, fmt.Errorf("chat completion already terminated")
	}
	if err := event.Validate(); err != nil {
		return e.fail("invalid canonical provider event", 502, "upstream_error", "upstream_event_invalid"), nil
	}

	switch event.Type {
	case protocol.EventHeartbeat,
		protocol.EventThinkingSignature,
		protocol.EventRedactedThinking,
		protocol.EventKiroRedactedReasoning,
		protocol.EventAssistantBoundary:
		return nil, nil

	case protocol.EventTextDelta:
		if event.Text == "" {
			return nil, nil
		}
		if !e.reserveTurn(len(event.Text)) {
			return e.bufferFailure(), nil
		}
		e.content.WriteString(event.Text)
		frames := e.ensureRole()
		return append(frames, e.chunk(map[string]any{"content": event.Text}, nil)), nil

	case protocol.EventThinkingDelta:
		if event.Thinking == "" {
			return nil, nil
		}
		if !e.reserveTurn(len(event.Thinking)) {
			return e.bufferFailure(), nil
		}
		e.reasoning.WriteString(event.Thinking)
		frames := e.ensureRole()
		return append(frames, e.chunk(map[string]any{"reasoning_content": event.Thinking}, nil)), nil

	case protocol.EventReasoningRawDelta:
		if event.Text == "" {
			return nil, nil
		}
		if !e.reserveTurn(len(event.Text)) {
			return e.bufferFailure(), nil
		}
		e.reasoning.WriteString(event.Text)
		frames := e.ensureRole()
		delta := map[string]any{"reasoning_content": event.Text}
		if wire := e.ingestReasoningDetail(event); wire != nil {
			delta["reasoning_details"] = []any{wire}
		}
		return append(frames, e.chunk(delta, nil)), nil

	case protocol.EventToolCallStart:
		if _, exists := e.tools[event.ID]; exists {
			return e.fail("upstream reused a tool call id", 502, "upstream_error", "upstream_tool_call_invalid"), nil
		}
		if !e.reserveTurn(len(event.ID) + len(event.Name)) {
			return e.bufferFailure(), nil
		}
		call := &toolCall{index: len(e.toolOrder), id: event.ID, name: event.Name}
		e.tools[event.ID] = call
		e.toolOrder = append(e.toolOrder, event.ID)
		return e.ensureRole(), nil

	case protocol.EventToolCallDelta:
		call := e.tools[event.ID]
		if call == nil || call.ended {
			return e.fail("upstream streamed tool arguments without an open call", 502, "upstream_error", "upstream_tool_call_invalid"), nil
		}
		if len(call.arguments)+len(event.Arguments) > e.maxCallArgumentBytes || !e.reserveTurn(len(event.Arguments)) {
			return e.bufferFailure(), nil
		}
		call.arguments += event.Arguments
		return nil, nil

	case protocol.EventToolCallEnd:
		call := e.tools[event.ID]
		if call == nil || call.ended {
			return e.fail("upstream closed an unknown tool call", 502, "upstream_error", "upstream_tool_call_invalid"), nil
		}
		call.ended = true
		frames := e.ensureRole()
		return append(frames, e.emitToolCall(call)), nil

	case protocol.EventDone:
		e.usage = cloneUsage(event.Usage)
		finish := finishReasonForDone(event.StopReason, len(e.toolOrder) > 0)
		return e.finish(finish), nil

	case protocol.EventIncomplete:
		e.usage = cloneUsage(event.Usage)
		switch event.Reason {
		case "max_output_tokens", "max_tokens":
			return e.finish("length"), nil
		case "content_filter":
			return e.finish("content_filter"), nil
		default:
			message := event.Message
			if strings.TrimSpace(message) == "" {
				message = fmt.Sprintf("upstream stream ended early (%s)", event.Reason)
			}
			return e.fail(message, 502, "upstream_error", "upstream_incomplete"), nil
		}

	case protocol.EventError:
		status := event.HTTPStatus
		if status <= 0 {
			status = 502
		}
		typ := event.ErrorType
		if typ == "" {
			typ = "upstream_error"
		}
		return e.fail(event.Message, status, typ, event.Code), nil

	case protocol.EventWebSearchCallBegin:
		return nil, nil
	case protocol.EventWebSearchCallEnd:
		e.addCitations(event.Sources)
		return nil, nil

	default:
		return e.fail("unsupported canonical provider event", 502, "upstream_error", "upstream_event_invalid"), nil
	}
}

func (e *Encoder) Completion() (map[string]any, error) {
	if !e.terminal {
		return nil, ErrNotTerminal
	}
	if e.failure != nil {
		return nil, e.failure
	}
	message := map[string]any{
		"role":    "assistant",
		"content": nil,
	}
	if e.content.Len() > 0 {
		message["content"] = e.content.String()
	}
	if e.reasoning.Len() > 0 {
		message["reasoning_content"] = e.reasoning.String()
	}
	if len(e.details) > 0 {
		message["reasoning_details"] = wireReasoningDetails(e.details)
	}
	if len(e.toolOrder) > 0 {
		calls := make([]any, 0, len(e.toolOrder))
		for _, id := range e.toolOrder {
			call := e.tools[id]
			calls = append(calls, toolCallPayload(call, false))
		}
		message["tool_calls"] = calls
	}
	if annotations := citationAnnotations(e.citations); len(annotations) > 0 {
		message["annotations"] = annotations
	}
	return map[string]any{
		"id":      e.id,
		"object":  "chat.completion",
		"created": e.created,
		"model":   e.model,
		"choices": []any{map[string]any{
			"index":         0,
			"message":       message,
			"finish_reason": e.finishReason,
			"logprobs":      nil,
		}},
		"usage": chatUsage(e.model, e.requestedTier, e.usage),
	}, nil
}

func (e *Encoder) ensureRole() []Frame {
	if e.started {
		return nil
	}
	e.started = true
	return []Frame{e.chunk(map[string]any{"role": "assistant", "content": ""}, nil)}
}

func (e *Encoder) finish(reason string) []Frame {
	frames := e.ensureRole()
	for _, id := range e.toolOrder {
		call := e.tools[id]
		if call.emitted {
			continue
		}
		call.ended = true
		frames = append(frames, e.emitToolCall(call))
	}
	e.finishReason = reason
	e.terminal = true
	terminal := e.chunk(map[string]any{}, reason)
	terminal.Payload["usage"] = chatUsage(e.model, e.requestedTier, e.usage)

	frames = append(frames, terminal, Frame{Done: true})
	return frames
}

func (e *Encoder) addCitations(sources []protocol.URLCitation) {
	if len(sources) == 0 {
		return
	}
	seen := make(map[string]struct{}, len(e.citations)+len(sources))
	for _, citation := range e.citations {
		seen[citation.URL] = struct{}{}
	}
	for _, source := range sources {
		url := strings.TrimSpace(source.URL)
		if url == "" {
			continue
		}
		if _, exists := seen[url]; exists {
			continue
		}
		seen[url] = struct{}{}
		e.citations = append(e.citations, protocol.URLCitation{URL: url, Title: source.Title})
	}
}

func citationAnnotations(citations []protocol.URLCitation) []any {
	if len(citations) == 0 {
		return nil
	}
	out := make([]any, 0, len(citations))
	for _, citation := range citations {
		item := map[string]any{
			"type":        "url_citation",
			"url":         citation.URL,
			"start_index": 0,
			"end_index":   0,
		}
		if citation.Title != "" {
			item["title"] = citation.Title
		}
		out = append(out, item)
	}
	return out
}

func (e *Encoder) ingestReasoningDetail(event protocol.Event) map[string]any {
	detail, ok := protocol.MiniMaxReasoningDetailFromEvent(event)
	if !ok {
		return nil
	}
	key := detail.ID
	if key == "" && detail.Index != nil {
		key = "i:" + strconv.Itoa(*detail.Index)
	}
	if key == "" {
		key = "seq:" + strconv.Itoa(len(e.details))
	}
	if index, exists := e.detailKey[key]; exists {
		current := e.details[index]
		current.Text += event.Text
		if detail.Type != "" {
			current.Type = detail.Type
		}
		if detail.Format != "" {
			current.Format = detail.Format
		}
		if detail.ID != "" {
			current.ID = detail.ID
		}
		if detail.Index != nil {
			current.Index = detail.Index
		}
		e.details[index] = current
		return wireReasoningDetail(current, event.Text)
	}
	detail.Text = event.Text
	e.detailKey[key] = len(e.details)
	e.details = append(e.details, detail)
	return wireReasoningDetail(detail, event.Text)
}

func wireReasoningDetails(details []protocol.MiniMaxReasoningDetail) []any {
	out := make([]any, 0, len(details))
	for _, detail := range details {
		out = append(out, wireReasoningDetail(detail, detail.Text))
	}
	return out
}

func wireReasoningDetail(detail protocol.MiniMaxReasoningDetail, text string) map[string]any {
	item := map[string]any{}
	if detail.Type != "" {
		item["type"] = detail.Type
	}
	if detail.ID != "" {
		item["id"] = detail.ID
	}
	if detail.Format != "" {
		item["format"] = detail.Format
	}
	if detail.Index != nil {
		item["index"] = *detail.Index
	}
	if text != "" {
		item["text"] = text
	}
	return item
}

func (e *Encoder) emitToolCall(call *toolCall) Frame {
	call.emitted = true
	return e.chunk(map[string]any{
		"tool_calls": []any{toolCallPayload(call, true)},
	}, nil)
}

func toolCallPayload(call *toolCall, includeIndex bool) map[string]any {
	payload := map[string]any{
		"id":   call.id,
		"type": "function",
		"function": map[string]any{
			"name":      call.name,
			"arguments": call.arguments,
		},
	}
	if includeIndex {
		payload["index"] = call.index
	}
	return payload
}

func (e *Encoder) chunk(delta map[string]any, finish any) Frame {
	return Frame{Payload: map[string]any{
		"id":      e.id,
		"object":  "chat.completion.chunk",
		"created": e.created,
		"model":   e.model,
		"choices": []any{map[string]any{
			"index":         0,
			"delta":         delta,
			"finish_reason": finish,
		}},
	}}
}

func (e *Encoder) fail(message string, status int, typ, code string) []Frame {
	if strings.TrimSpace(message) == "" {
		message = "upstream request failed"
	}
	if typ == "" {
		typ = "upstream_error"
	}
	e.terminal = true
	e.failure = &TerminalError{Status: status, Type: typ, Code: code, Message: message}
	var wireCode any
	if code != "" {
		wireCode = code
	}
	return []Frame{{Payload: map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    typ,
			"param":   nil,
			"code":    wireCode,
		},
	}}}
}

func (e *Encoder) bufferFailure() []Frame {
	return e.fail("translator buffer exceeded the safe limit", 502, "upstream_error", "translation_buffer_limit")
}

func (e *Encoder) reserveTurn(delta int) bool {
	if delta < 0 || e.retainedBytes+delta > e.maxTurnBytes {
		return false
	}
	e.retainedBytes += delta
	return true
}

func finishReasonForDone(stopReason string, toolUsed bool) string {
	switch stopReason {
	case "max_output_tokens", "max_tokens":
		return "length"
	case "content_filter":
		return "content_filter"
	}
	if toolUsed {
		return "tool_calls"
	}
	return "stop"
}

func chatUsage(model, requestedTier string, usageData *protocol.Usage) map[string]any {
	var input, output, cached, reasoning int64
	confirmed := ""
	if usageData != nil {
		input = usageData.InputTokens
		output = usageData.OutputTokens
		cached = usageData.CachedInputTokens
		reasoning = usageData.ReasoningOutputTokens
		confirmed = usageData.ServiceTier
	}
	payload := map[string]any{
		"prompt_tokens":             input,
		"completion_tokens":         output,
		"total_tokens":              input + output,
		"prompt_tokens_details":     map[string]any{"cached_tokens": cached},
		"completion_tokens_details": map[string]any{"reasoning_tokens": reasoning},
	}
	if estimate, ok := usage.ForRoutedModel(model, confirmed, requestedTier, input, usage.Cost4{}, 0); ok {
		payload["xai_priority_applied"] = estimate.PriorityApplied
		payload["xai_lower_bound"] = estimate.LowerBound
	}
	return payload
}

func cloneUsage(usage *protocol.Usage) *protocol.Usage {
	if usage == nil {
		return nil
	}
	copy := *usage
	return &copy
}

func completionID() string {
	var bytes [12]byte
	if _, err := rand.Read(bytes[:]); err == nil {
		return "chatcmpl-" + hex.EncodeToString(bytes[:])
	}
	return fmt.Sprintf("chatcmpl-%016x%08x", uint64(time.Now().UnixNano()), fallbackIDSeq.Add(1))
}
