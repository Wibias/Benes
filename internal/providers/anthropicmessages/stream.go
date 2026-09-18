package anthropicmessages

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

type streamBlock struct {
	kind string
	id   string
	name string
}

type Stream struct {
	decoder              *sse.Decoder
	pending              []protocol.Event
	blocks               map[int]streamBlock
	usage                *protocol.Usage
	uncachedInputTokens  int64
	hasUncachedInput     bool
	stopReason           string
	started              bool
	terminal             bool
	turn                 *resourcebudget.Turn
	toolArgs             *resourcebudget.Accumulator
}

func NewStream(reader io.Reader, limits sse.Limits) *Stream {
	return &Stream{decoder: sse.NewDecoder(reader, limits), blocks: make(map[int]streamBlock)}
}

func (s *Stream) Next() (protocol.Event, error) {
	if event, ok := s.popPending(); ok {
		return event, nil
	}
	if s.terminal {
		return protocol.Event{}, io.EOF
	}
	for {
		wire, err := s.decoder.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				s.fail("Anthropic Messages upstream stream ended without terminal message_stop")
				return s.popPendingOrEOF()
			}
			return protocol.Event{}, err
		}
		if err := reserveTurnBytes(s.turn, resourcebudget.ClassStreamPending, int64(len(wire.Data))); err != nil {
			return protocol.Event{}, err
		}
		s.handleWire(wire)
		if event, ok := s.popPending(); ok {
			return event, nil
		}
		if s.terminal {
			return protocol.Event{}, io.EOF
		}
	}
}

func (s *Stream) handleWire(wire sse.Event) {
	if s.terminal {
		return
	}
	payload := strings.TrimSpace(wire.Data)
	if payload == "" {
		return
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal([]byte(payload), &root); err != nil || root == nil {
		s.fail("malformed Anthropic Messages upstream SSE frame")
		return
	}
	typ := rawString(root["type"])
	if typ == "" {
		typ = wire.Type
	}
	if wire.Type != "" && wire.Type != "message" && typ != "" && wire.Type != typ {
		s.fail("Anthropic Messages upstream SSE event type mismatch")
		return
	}

	switch typ {
	case "ping":
		s.pending = append(s.pending, protocol.Event{Type: protocol.EventHeartbeat})
	case "message_start":
		s.handleMessageStart(root)
	case "content_block_start":
		s.handleBlockStart(root)
	case "content_block_delta":
		s.handleBlockDelta(root)
	case "content_block_stop":
		s.handleBlockStop(root)
	case "message_delta":
		s.handleMessageDelta(root)
	case "message_stop":
		s.handleMessageStop()
	case "error":
		s.fail("Anthropic Messages upstream failed")
	default:
		s.fail("unsupported Anthropic Messages upstream SSE event")
	}
}

func (s *Stream) handleMessageStart(root map[string]json.RawMessage) {
	if s.started {
		s.fail("Anthropic Messages upstream emitted duplicate message_start")
		return
	}
	var message map[string]json.RawMessage
	if json.Unmarshal(root["message"], &message) != nil || message == nil {
		s.fail("Anthropic Messages upstream message_start is invalid")
		return
	}
	s.started = true
	s.mergeUsage(message["usage"])
}

func (s *Stream) handleBlockStart(root map[string]json.RawMessage) {
	if !s.started {
		s.fail("Anthropic Messages upstream content started before message_start")
		return
	}
	index, ok := rawIndex(root["index"])
	if !ok {
		s.fail("Anthropic Messages upstream content block has invalid index")
		return
	}
	if _, exists := s.blocks[index]; exists {
		s.fail("Anthropic Messages upstream reopened a content block index")
		return
	}
	var block map[string]json.RawMessage
	if json.Unmarshal(root["content_block"], &block) != nil || block == nil {
		s.fail("Anthropic Messages upstream content block is invalid")
		return
	}
	kind := rawString(block["type"])
	switch kind {
	case "text", "thinking":
		s.blocks[index] = streamBlock{kind: kind}
	case "tool_use":
		id, name := rawString(block["id"]), rawString(block["name"])
		if strings.TrimSpace(id) == "" || strings.TrimSpace(name) == "" {
			s.fail("Anthropic Messages upstream tool_use is missing id or name")
			return
		}
		s.blocks[index] = streamBlock{kind: kind, id: id, name: name}
		s.pending = append(s.pending, protocol.Event{Type: protocol.EventToolCallStart, ID: id, Name: name})
		if initial := nonEmptyObject(block["input"]); initial != nil {
			encoded, err := json.Marshal(initial)
			if err != nil || !s.appendToolArguments(id, string(encoded)) {
				return
			}
		}
	case "redacted_thinking":
		s.blocks[index] = streamBlock{kind: kind}
		data := rawString(block["data"])
		s.pending = append(s.pending, protocol.Event{Type: protocol.EventRedactedThinking, Data: data})
	default:
		s.fail("unsupported Anthropic Messages upstream content block")
	}
}

func (s *Stream) handleBlockDelta(root map[string]json.RawMessage) {
	index, ok := rawIndex(root["index"])
	if !ok {
		s.fail("Anthropic Messages upstream delta has invalid index")
		return
	}
	block, exists := s.blocks[index]
	if !exists {
		s.fail("Anthropic Messages upstream delta has no matching content block")
		return
	}
	var delta map[string]json.RawMessage
	if json.Unmarshal(root["delta"], &delta) != nil || delta == nil {
		s.fail("Anthropic Messages upstream content delta is invalid")
		return
	}
	switch rawString(delta["type"]) {
	case "text_delta":
		if block.kind != "text" {
			s.fail("Anthropic Messages upstream text delta has wrong block type")
			return
		}
		if text := rawString(delta["text"]); text != "" {
			s.pending = append(s.pending, protocol.Event{Type: protocol.EventTextDelta, Text: text})
		}
	case "thinking_delta":
		if block.kind != "thinking" {
			s.fail("Anthropic Messages upstream thinking delta has wrong block type")
			return
		}
		if thinking := rawString(delta["thinking"]); thinking != "" {
			s.pending = append(s.pending, protocol.Event{Type: protocol.EventThinkingDelta, Thinking: thinking})
		}
	case "signature_delta":
		if block.kind != "thinking" {
			s.fail("Anthropic Messages upstream signature delta has wrong block type")
			return
		}
		if signature := rawString(delta["signature"]); signature != "" {
			s.pending = append(s.pending, protocol.Event{Type: protocol.EventThinkingSignature, Signature: signature})
		}
	case "input_json_delta":
		if block.kind != "tool_use" {
			s.fail("Anthropic Messages upstream tool input delta has wrong block type")
			return
		}
		s.appendToolArguments(block.id, rawString(delta["partial_json"]))
	default:
		s.fail("unsupported Anthropic Messages upstream content delta")
	}
}

func (s *Stream) handleBlockStop(root map[string]json.RawMessage) {
	index, ok := rawIndex(root["index"])
	if !ok {
		s.fail("Anthropic Messages upstream content stop has invalid index")
		return
	}
	block, exists := s.blocks[index]
	if !exists {
		s.fail("Anthropic Messages upstream closed an unknown content block")
		return
	}
	delete(s.blocks, index)
	if block.kind == "tool_use" {
		s.pending = append(s.pending, protocol.Event{Type: protocol.EventToolCallEnd, ID: block.id})
	}
}

func (s *Stream) handleMessageDelta(root map[string]json.RawMessage) {
	if !s.started {
		s.fail("Anthropic Messages upstream message_delta preceded message_start")
		return
	}
	var delta map[string]json.RawMessage
	if json.Unmarshal(root["delta"], &delta) != nil || delta == nil {
		s.fail("Anthropic Messages upstream message_delta is invalid")
		return
	}
	if reason := rawString(delta["stop_reason"]); reason != "" {
		s.stopReason = reason
	}
	s.mergeUsage(root["usage"])
}

func (s *Stream) handleMessageStop() {
	if !s.started {
		s.fail("Anthropic Messages upstream message_stop preceded message_start")
		return
	}
	if len(s.blocks) != 0 {
		s.fail("Anthropic Messages upstream message_stop arrived with an open content block")
		return
	}
	if strings.TrimSpace(s.stopReason) == "" {
		s.fail("Anthropic Messages upstream message_stop omitted terminal stop reason")
		return
	}
	if s.usage != nil {
		s.usage.TotalTokens = s.usage.InputTokens + s.usage.OutputTokens
	}
	s.pending = append(s.pending, protocol.Event{Type: protocol.EventDone, StopReason: s.stopReason, Usage: cloneUsage(s.usage)})
	s.terminal = true
}

func (s *Stream) mergeUsage(raw json.RawMessage) {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return
	}
	var usage struct {
		InputTokens              *int64 `json:"input_tokens"`
		OutputTokens             *int64 `json:"output_tokens"`
		CacheReadInputTokens     *int64 `json:"cache_read_input_tokens"`
		CacheCreationInputTokens *int64 `json:"cache_creation_input_tokens"`
	}
	if json.Unmarshal(raw, &usage) != nil {
		s.fail("Anthropic Messages upstream usage is invalid")
		return
	}
	for _, value := range []*int64{usage.InputTokens, usage.OutputTokens, usage.CacheReadInputTokens, usage.CacheCreationInputTokens} {
		if value != nil && *value < 0 {
			s.fail("Anthropic Messages upstream usage contains a negative token count")
			return
		}
	}
	if s.usage == nil {
		s.usage = &protocol.Usage{}
	}
	if usage.InputTokens != nil {
		s.uncachedInputTokens = *usage.InputTokens
		s.hasUncachedInput = true
	}
	if usage.OutputTokens != nil {
		s.usage.OutputTokens = *usage.OutputTokens
	}
	if usage.CacheReadInputTokens != nil {
		s.usage.CacheReadInputTokens = *usage.CacheReadInputTokens
		s.usage.CachedInputTokens = *usage.CacheReadInputTokens
	}
	if usage.CacheCreationInputTokens != nil {
		s.usage.CacheCreationInputTokens = *usage.CacheCreationInputTokens
	}
	if s.hasUncachedInput {
		s.usage.InputTokens = s.uncachedInputTokens + s.usage.CacheReadInputTokens + s.usage.CacheCreationInputTokens
	}
	s.usage.TotalTokens = s.usage.InputTokens + s.usage.OutputTokens
}

func (s *Stream) appendToolArguments(id, arguments string) bool {
	if arguments == "" {
		return true
	}
	if s.turn != nil && s.toolArgs == nil {
		s.toolArgs = resourcebudget.NewAccumulator(s.turn, resourcebudget.ClassToolArguments)
	}
	if s.toolArgs != nil {
		if err := s.toolArgs.Append([]byte(arguments)); err != nil {
			s.fail("Anthropic Messages upstream tool argument budget exceeded")
			return false
		}
	}
	s.pending = append(s.pending, protocol.Event{Type: protocol.EventToolCallDelta, ID: id, Arguments: arguments})
	return true
}

func (s *Stream) fail(message string) {
	if s.terminal {
		return
	}
	s.pending = nil
	s.blocks = make(map[int]streamBlock)
	s.pending = append(s.pending, protocol.Event{
		Type:       protocol.EventError,
		Message:    message,
		HTTPStatus: 502,
		ErrorType:  "upstream_error",
		Usage:      cloneUsage(s.usage),
	})
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

func (s *Stream) popPendingOrEOF() (protocol.Event, error) {
	if event, ok := s.popPending(); ok {
		return event, nil
	}
	return protocol.Event{}, io.EOF
}

func rawString(raw json.RawMessage) string {
	var value string
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil {
		return ""
	}
	return value
}

func rawIndex(raw json.RawMessage) (int, bool) {
	var value int
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil || value < 0 {
		return 0, false
	}
	return value, true
}

func nonEmptyObject(raw json.RawMessage) map[string]any {
	var value map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil || len(value) == 0 {
		return nil
	}
	return value
}

func cloneUsage(usage *protocol.Usage) *protocol.Usage {
	if usage == nil {
		return nil
	}
	cloned := *usage
	return &cloned
}

func (s *Stream) String() string {
	return fmt.Sprintf("anthropic messages stream(started=%t terminal=%t)", s.started, s.terminal)
}
