package outbound

import (
	"encoding/json"
	"strings"
)

func (e *Encoder) ensureStarted() []Frame {
	if e.started {
		return nil
	}
	e.started = true
	return []Frame{
		frame("message_start", map[string]any{"type": "message_start", "message": map[string]any{"id": e.id, "type": "message", "role": "assistant", "content": []any{}, "model": e.model, "stop_reason": nil, "stop_sequence": nil, "usage": map[string]any{"input_tokens": int64(0), "output_tokens": int64(0)}}}),
		frame("ping", map[string]any{"type": "ping"}),
	}
}

func (e *Encoder) ensureBlock(kind blockKind) ([]Frame, bool) {
	if e.open != nil && e.open.kind == kind {
		return nil, true
	}
	if e.open != nil && e.open.kind == blockTool && !e.open.toolEnded {
		return e.fail(502, "upstream changed content while a tool call was still open", "api_error", "upstream_tool_call_invalid"), false
	}
	frames, ok := e.closeOpen()
	if !ok {
		return frames, false
	}
	frames = append(frames, e.ensureStarted()...)
	index := e.nextIndex
	e.nextIndex++
	e.open = &openBlock{kind: kind, index: index}
	block := map[string]any{}
	if kind == blockText {
		block = map[string]any{"type": "text", "text": ""}
	} else {
		block = map[string]any{"type": "thinking", "thinking": "", "signature": ""}
	}
	frames = append(frames, frame("content_block_start", map[string]any{"type": "content_block_start", "index": index, "content_block": block}))
	return frames, true
}

func (e *Encoder) closeOpen() ([]Frame, bool) {
	if e.open == nil {
		return nil, true
	}
	b := e.open
	if b.kind == blockTool && !b.toolEnded {
		return e.fail(502, "upstream ended before tool call closed", "api_error", "upstream_tool_call_invalid"), false
	}
	frames := []Frame{}
	switch b.kind {
	case blockThinking:
		if !b.signatureEmitted {
			sig := strings.TrimSpace(e.signatureGenerator())
			if sig == "" {
				return e.fail(502, "thinking signature generator returned blank signature", "api_error", "upstream_event_invalid"), false
			}
			if !e.reserve(len(sig)) {
				return e.bufferFailure(), false
			}
			b.signature, b.signatureEmitted = sig, true
			frames = append(frames, e.signatureFrame(sig))
		}
		e.content = append(e.content, map[string]any{"type": "thinking", "thinking": b.text.String(), "signature": b.signature})
	case blockText:
		e.content = append(e.content, map[string]any{"type": "text", "text": b.text.String()})
	case blockTool:
		input := map[string]any{}
		raw := b.arguments.String()
		if strings.TrimSpace(raw) != "" {
			if err := json.Unmarshal([]byte(raw), &input); err != nil || input == nil {
				return e.fail(502, "upstream tool arguments were not a JSON object", "api_error", "upstream_tool_call_invalid"), false
			}
		}
		e.content = append(e.content, map[string]any{"type": "tool_use", "id": b.toolID, "name": b.toolName, "input": input})
	}
	frames = append(frames, frame("content_block_stop", map[string]any{"type": "content_block_stop", "index": b.index}))
	e.open = nil
	return frames, true
}

func (e *Encoder) signatureFrame(signature string) Frame {
	return frame("content_block_delta", map[string]any{"type": "content_block_delta", "index": e.open.index, "delta": map[string]any{"type": "signature_delta", "signature": signature}})
}

func (e *Encoder) finish(reason string) []Frame {
	frames, ok := e.closeOpen()
	if !ok {
		return frames
	}
	frames = append(frames, e.ensureStarted()...)
	e.stopReason = reason
	e.terminal = true
	frames = append(frames,
		frame("message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": reason, "stop_sequence": nil}, "usage": e.usagePayload()}),
		frame("message_stop", map[string]any{"type": "message_stop"}),
	)
	return frames
}

func (e *Encoder) Message() (map[string]any, error) {
	if !e.terminal {
		return nil, ErrNotTerminal
	}
	if e.failure != nil {
		return nil, e.failure
	}
	return map[string]any{"id": e.id, "type": "message", "role": "assistant", "content": append([]any(nil), e.content...), "model": e.model, "stop_reason": e.stopReason, "stop_sequence": nil, "usage": e.usagePayload()}, nil
}
