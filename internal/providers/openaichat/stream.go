package openaichat

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/resourcebudget"
	"github.com/Wibias/Benes/internal/responses/sse"
)

type Stream struct {
	decoder          *sse.Decoder
	pending          []protocol.Event
	toolCalls        []*streamToolCall
	usage            *protocol.Usage
	finishReason     string
	sawOutput        bool
	terminal         bool
	callSeq          int
	turn             *resourcebudget.Turn
	toolArgs         *resourcebudget.Accumulator
	reasoningDetails map[string]*protocol.MiniMaxReasoningDetail
	detailSeq        int
}

type streamToolCall struct {
	key       string
	id        string
	name      string
	arguments string
}

func NewStream(reader io.Reader, limits sse.Limits) *Stream {
	return &Stream{decoder: sse.NewDecoder(reader, limits)}
}

func (s *Stream) Next() (protocol.Event, error) {
	if event, ok := s.popPending(); ok {
		return event, nil
	}
	if s.terminal {
		return protocol.Event{}, io.EOF
	}

	for {
		event, err := s.decoder.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				s.handleEOF()
				if next, ok := s.popPending(); ok {
					return next, nil
				}
				return protocol.Event{}, io.EOF
			}
			return protocol.Event{}, err
		}
		if err := reserveTurnBytes(s.turn, resourcebudget.ClassStreamPending, int64(len(event.Data))); err != nil {
			return protocol.Event{}, err
		}
		s.handlePayload(strings.TrimSpace(event.Data))
		if next, ok := s.popPending(); ok {
			return next, nil
		}
		if s.terminal {
			return protocol.Event{}, io.EOF
		}
	}
}

func (s *Stream) handlePayload(payload string) {
	if s.terminal || payload == "" {
		return
	}
	if payload == "[DONE]" {
		if !s.flushToolCalls() {
			return
		}
		s.finish()
		return
	}

	var chunk map[string]json.RawMessage
	if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
		s.fail(protocol.Event{Type: protocol.EventError, Message: "malformed upstream SSE data frame", Usage: s.usage})
		return
	}
	if raw, ok := chunk["error"]; ok && !isJSONNull(raw) {
		s.fail(protocol.Event{Type: protocol.EventError, Message: "OpenAI Chat upstream failed", Usage: s.usage})
		return
	}
	if raw, ok := chunk["usage"]; ok && !isJSONNull(raw) {
		if usage := usageFromChat(raw); usage != nil {
			s.usage = usage
		}
	}
	if raw, ok := chunk["service_tier"]; ok && !isJSONNull(raw) {
		var tier string
		if json.Unmarshal(raw, &tier) == nil && strings.TrimSpace(tier) != "" {
			if s.usage == nil {
				s.usage = &protocol.Usage{}
			}
			s.usage.ServiceTier = strings.TrimSpace(tier)
		}
	}

	rawChoices, ok := chunk["choices"]
	if !ok {
		return
	}
	var choices []json.RawMessage
	if isJSONNull(rawChoices) || json.Unmarshal(rawChoices, &choices) != nil {
		s.fail(invalidChoicesEvent(s.usage))
		return
	}
	if len(choices) == 0 {
		return
	}

	var choice map[string]json.RawMessage
	if isJSONNull(choices[0]) || json.Unmarshal(choices[0], &choice) != nil {
		s.fail(invalidChoicesEvent(s.usage))
		return
	}

	if raw, ok := choice["finish_reason"]; ok && !isJSONNull(raw) {
		var reason string
		if json.Unmarshal(raw, &reason) == nil && reason != "" {
			if reason == "error" {
				s.fail(protocol.Event{Type: protocol.EventError, Message: "OpenAI Chat upstream failed", Usage: s.usage})
				return
			}
			s.finishReason = reason
		}
	}

	if rawDelta, ok := choice["delta"]; ok && !isJSONNull(rawDelta) {
		var delta map[string]json.RawMessage
		if json.Unmarshal(rawDelta, &delta) == nil {
			s.handleDelta(delta)
			if s.terminal {
				return
			}
		}
	}

	if s.finishReason != "" {
		s.flushToolCalls()
	}
}

func (s *Stream) handleDelta(delta map[string]json.RawMessage) {
	skipReasoningText := false
	if raw, ok := delta["reasoning_details"]; ok && !isJSONNull(raw) {
		ok, skip := s.handleReasoningDetails(raw)
		if !ok {
			return
		}
		skipReasoningText = skip
	}
	if !skipReasoningText {
		if reasoning := firstString(delta, "reasoning_content", "reasoning"); reasoning != "" {
			s.pending = append(s.pending, protocol.Event{Type: protocol.EventReasoningRawDelta, Text: reasoning})
		}
	}
	if content := stringField(delta["content"]); content != "" {
		s.sawOutput = true
		s.pending = append(s.pending, protocol.Event{Type: protocol.EventTextDelta, Text: content})
	}

	rawToolCalls, ok := delta["tool_calls"]
	if !ok || isJSONNull(rawToolCalls) {
		return
	}
	var calls []json.RawMessage
	if json.Unmarshal(rawToolCalls, &calls) != nil {
		s.fail(invalidToolCallsEvent(s.usage))
		return
	}
	for _, rawCall := range calls {
		if !s.ingestToolCall(rawCall) {
			return
		}
	}
}

func (s *Stream) ingestToolCall(raw json.RawMessage) bool {
	var call map[string]json.RawMessage
	if isJSONNull(raw) || json.Unmarshal(raw, &call) != nil {
		s.fail(invalidToolCallsEvent(s.usage))
		return false
	}

	id, idPresent, idValid := nullableStringField(call["id"])
	if idPresent && !idValid {
		s.fail(invalidToolCallsEvent(s.usage))
		return false
	}

	name := ""
	arguments := ""
	if rawFunction, ok := call["function"]; ok && !isJSONNull(rawFunction) {
		var function map[string]json.RawMessage
		if json.Unmarshal(rawFunction, &function) != nil {
			s.fail(invalidToolCallsEvent(s.usage))
			return false
		}
		var present, valid bool
		name, present, valid = nullableStringField(function["name"])
		if present && !valid {
			s.fail(invalidToolCallsEvent(s.usage))
			return false
		}
		arguments, present, valid = nullableStringField(function["arguments"])
		if present && !valid {
			s.fail(invalidToolCallsEvent(s.usage))
			return false
		}
	}

	key := numericIndexKey(call["index"])
	if key == "" && id != "" {
		key = "id:" + id
	}
	if key == "" && len(s.toolCalls) > 0 {
		key = s.toolCalls[len(s.toolCalls)-1].key
	}

	current := s.findToolCall(key, id)
	if current == nil {
		if key == "" {
			key = fmt.Sprintf("seq:%d", len(s.toolCalls))
		}
		current = &streamToolCall{key: key}
		s.toolCalls = append(s.toolCalls, current)
	}
	if id != "" && current.id == "" {
		current.id = id
	}
	if name != "" && current.name == "" {
		current.name = name
	}
	if arguments != "" {
		if s.turn != nil && s.toolArgs == nil {
			s.toolArgs = resourcebudget.NewAccumulator(s.turn, resourcebudget.ClassToolArguments)
		}
		if s.toolArgs != nil {
			if err := s.toolArgs.Append([]byte(arguments)); err != nil {
				s.fail(protocol.Event{Type: protocol.EventError, Message: "tool argument budget exceeded", Usage: s.usage})
				return false
			}
		}
		current.arguments += arguments
	}
	return true
}

func (s *Stream) findToolCall(key, id string) *streamToolCall {
	if key != "" {
		for _, call := range s.toolCalls {
			if call.key == key {
				return call
			}
		}
	}
	if id != "" {
		for _, call := range s.toolCalls {
			if call.id == id {
				return call
			}
		}
	}
	return nil
}

func (s *Stream) flushToolCalls() bool {
	if len(s.toolCalls) == 0 {
		return true
	}
	calls := s.toolCalls
	s.toolCalls = nil
	for _, call := range calls {
		if strings.TrimSpace(call.name) == "" {
			s.fail(protocol.Event{
				Type:       protocol.EventError,
				Message:    "upstream streamed a tool call without a function name - cannot dispatch",
				Usage:      s.usage,
				HTTPStatus: 502,
				ErrorType:  "upstream_error",
			})
			return false
		}
		if call.id == "" {
			s.callSeq++
			call.id = fmt.Sprintf("call_%d", s.callSeq)
		}
		s.pending = append(s.pending, protocol.Event{Type: protocol.EventToolCallStart, ID: call.id, Name: call.name})
		if call.arguments != "" {
			s.pending = append(s.pending, protocol.Event{Type: protocol.EventToolCallDelta, ID: call.id, Arguments: call.arguments})
		}
		s.pending = append(s.pending, protocol.Event{Type: protocol.EventToolCallEnd, ID: call.id})
	}
	return true
}

func (s *Stream) handleEOF() {
	if s.terminal {
		return
	}
	if len(s.toolCalls) > 0 && s.finishReason == "" {
		s.fail(protocol.Event{Type: protocol.EventError, Message: "upstream stream ended mid tool call without a terminal signal - possible truncation", Usage: s.usage})
		return
	}
	if s.finishReason == "" {
		s.fail(protocol.Event{Type: protocol.EventError, Message: "upstream stream ended without a terminal signal ([DONE] or finish_reason) - possible truncation", Usage: s.usage})
		return
	}
	if !s.flushToolCalls() {
		return
	}
	s.finish()
}

func (s *Stream) finish() {
	if s.terminal {
		return
	}
	event := protocol.Event{Type: protocol.EventDone, Usage: s.usage}
	switch s.finishReason {
	case "length":
		event.StopReason = "max_tokens"
	case "content_filter":
		event.StopReason = "content_filter"
	}
	s.pending = append(s.pending, event)
	s.terminal = true
}

func (s *Stream) fail(event protocol.Event) {
	if s.terminal {
		return
	}
	s.pending = nil
	s.toolCalls = nil
	s.pending = append(s.pending, event)
	s.terminal = true
}

func (s *Stream) popPending() (protocol.Event, bool) {
	if len(s.pending) == 0 {
		return protocol.Event{}, false
	}
	event := s.pending[0]
	s.pending = s.pending[1:]
	return event, true
}

func usageFromChat(raw json.RawMessage) *protocol.Usage {
	var usage struct {
		PromptTokens     int64 `json:"prompt_tokens"`
		CompletionTokens int64 `json:"completion_tokens"`
		TotalTokens      int64 `json:"total_tokens"`
		PromptDetails    struct {
			CachedTokens int64 `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
		CompletionDetails struct {
			ReasoningTokens int64 `json:"reasoning_tokens"`
		} `json:"completion_tokens_details"`
	}
	if json.Unmarshal(raw, &usage) != nil {
		return nil
	}
	return &protocol.Usage{
		InputTokens:           usage.PromptTokens,
		OutputTokens:          usage.CompletionTokens,
		TotalTokens:           usage.TotalTokens,
		CachedInputTokens:     usage.PromptDetails.CachedTokens,
		ReasoningOutputTokens: usage.CompletionDetails.ReasoningTokens,
	}
}

func invalidToolCallsEvent(usage *protocol.Usage) protocol.Event {
	return protocol.Event{
		Type:       protocol.EventError,
		Message:    "upstream response contained invalid tool calls",
		Usage:      usage,
		HTTPStatus: 502,
		ErrorType:  "upstream_error",
	}
}

func invalidChoicesEvent(usage *protocol.Usage) protocol.Event {
	return protocol.Event{
		Type:       protocol.EventError,
		Message:    "upstream response contained invalid choices",
		Usage:      usage,
		HTTPStatus: 502,
		ErrorType:  "upstream_error",
	}
}

func (s *Stream) handleReasoningDetails(raw json.RawMessage) (ok bool, skipFallback bool) {
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil {
		s.fail(protocol.Event{
			Type:       protocol.EventError,
			Message:    "upstream response contained invalid reasoning_details",
			Usage:      s.usage,
			HTTPStatus: 502,
			ErrorType:  "upstream_error",
		})
		return false, false
	}
	if s.reasoningDetails == nil {
		s.reasoningDetails = make(map[string]*protocol.MiniMaxReasoningDetail)
	}
	for _, itemRaw := range items {
		if isJSONNull(itemRaw) {
			continue
		}
		var item map[string]json.RawMessage
		if json.Unmarshal(itemRaw, &item) != nil {
			continue
		}
		skipFallback = true
		detail := protocol.MiniMaxReasoningDetail{
			Type:   stringField(item["type"]),
			ID:     stringField(item["id"]),
			Format: stringField(item["format"]),
			Text:   stringField(item["text"]),
		}
		if index, ok := intField(item["index"]); ok {
			detail.Index = &index
		}
		key := detail.ID
		if key == "" && detail.Index != nil {
			key = "i:" + strings.TrimSpace(string(item["index"]))
		}
		if key == "" {
			s.detailSeq++
			key = fmt.Sprintf("seq:%d", s.detailSeq)
		}
		current := s.reasoningDetails[key]
		if current == nil {
			copyDetail := detail
			copyDetail.Text = ""
			s.reasoningDetails[key] = &copyDetail
			current = &copyDetail
		} else {
			if detail.Type != "" {
				current.Type = detail.Type
			}
			if detail.ID != "" {
				current.ID = detail.ID
			}
			if detail.Format != "" {
				current.Format = detail.Format
			}
			if detail.Index != nil {
				current.Index = detail.Index
			}
		}
		delta, full := mergeReasoningText(current.Text, detail.Text)
		current.Text = full
		if delta == "" {
			continue
		}
		emitted := *current
		emitted.Text = ""
		s.pending = append(s.pending, protocol.Event{
			Type:             protocol.EventReasoningRawDelta,
			Text:             delta,
			ProviderMetadata: protocol.EncodeMiniMaxReasoningDetail(emitted),
		})
	}
	return true, skipFallback
}

func mergeReasoningText(prev, incoming string) (delta string, full string) {
	if incoming == "" {
		return "", prev
	}
	if prev != "" && strings.HasPrefix(incoming, prev) {
		return incoming[len(prev):], incoming
	}
	return incoming, prev + incoming
}

func intField(raw json.RawMessage) (int, bool) {
	if len(raw) == 0 || isJSONNull(raw) {
		return 0, false
	}
	var number json.Number
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&number) != nil {
		return 0, false
	}
	value, err := number.Int64()
	if err != nil {
		return 0, false
	}
	return int(value), true
}

func firstString(values map[string]json.RawMessage, keys ...string) string {
	for _, key := range keys {
		if value := stringField(values[key]); value != "" {
			return value
		}
	}
	return ""
}

func stringField(raw json.RawMessage) string {
	if len(raw) == 0 || isJSONNull(raw) {
		return ""
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return ""
	}
	return value
}

func nullableStringField(raw json.RawMessage) (value string, present bool, valid bool) {
	if len(raw) == 0 || isJSONNull(raw) {
		return "", false, true
	}
	if json.Unmarshal(raw, &value) != nil {
		return "", true, false
	}
	return value, true, true
}

func numericIndexKey(raw json.RawMessage) string {
	if len(raw) == 0 || isJSONNull(raw) {
		return ""
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil {
		return ""
	}
	number, ok := value.(json.Number)
	if !ok {
		return ""
	}
	return "i:" + number.String()
}

func isJSONNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}
