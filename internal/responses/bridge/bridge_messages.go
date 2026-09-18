package bridge

import "github.com/Wibias/Benes/internal/protocol"

func (b *Bridge) closeMessageIfOpen(inferFinal bool) []Frame {
	if b.message == nil {
		return nil
	}
	if inferFinal {
		return b.closeMessage(protocol.PhaseFinalAnswer)
	}
	return b.closeMessage("")
}

func (b *Bridge) closeMessage(inferred protocol.MessagePhase) []Frame {
	if b.message == nil {
		return nil
	}
	msg := b.message
	phase := msg.phase
	if phase == nil && inferred != "" {
		phase = &inferred
	}
	annotations := b.takeWebAnnotations()
	item := map[string]any{
		"type": "message", "id": msg.id, "status": "completed", "role": "assistant",
		"content": []any{map[string]any{"type": "output_text", "text": msg.text, "annotations": annotations}},
	}
	if phase != nil {
		item["phase"] = string(*phase)
	}
	frames := []Frame{
		b.emit("response.output_text.done", map[string]any{
			"item_id": msg.id, "output_index": msg.outputIndex, "content_index": 0, "text": msg.text,
			"logprobs": []any{},
		}),
		b.emit("response.content_part.done", map[string]any{
			"item_id": msg.id, "output_index": msg.outputIndex, "content_index": 0,
			"part": map[string]any{"type": "output_text", "text": msg.text, "annotations": annotations},
		}),
		b.emit("response.output_item.done", map[string]any{"output_index": msg.outputIndex, "item": item}),
	}
	b.output = append(b.output, item)
	b.outputIndex++
	b.message = nil
	return frames
}

func (b *Bridge) closeReasoning() []Frame {
	if b.reasoning == nil {
		return nil
	}
	current := b.reasoning
	item := map[string]any{
		"type": "reasoning", "id": current.id,
		"summary": []any{map[string]any{"type": "summary_text", "text": current.text}},
	}
	if encrypted := b.takeReasoningEnvelope(""); encrypted != "" {
		item["encrypted_content"] = encrypted
	}
	frames := []Frame{
		b.emit("response.reasoning_summary_text.done", map[string]any{
			"item_id": current.id, "output_index": current.outputIndex, "summary_index": 0, "text": current.text,
		}),
		b.emit("response.reasoning_summary_part.done", map[string]any{
			"item_id": current.id, "output_index": current.outputIndex, "summary_index": 0,
			"part": map[string]any{"type": "summary_text", "text": current.text},
		}),
		b.emit("response.output_item.done", map[string]any{"output_index": current.outputIndex, "item": item}),
	}
	b.output = append(b.output, item)
	b.outputIndex++
	b.reasoning = nil
	return frames
}

func (b *Bridge) closeRawReasoning() []Frame {
	if b.rawReasoning == nil {
		return nil
	}
	current := b.rawReasoning
	item := map[string]any{
		"type": "reasoning", "id": current.id,
		"summary": []any{map[string]any{"type": "summary_text", "text": current.text}},
	}
	frames := []Frame{
		b.emit("response.reasoning_summary_text.done", map[string]any{
			"item_id": current.id, "output_index": current.outputIndex, "summary_index": 0, "text": current.text,
		}),
		b.emit("response.reasoning_summary_part.done", map[string]any{
			"item_id": current.id, "output_index": current.outputIndex, "summary_index": 0,
			"part": map[string]any{"type": "summary_text", "text": current.text},
		}),
		b.emit("response.output_item.done", map[string]any{"output_index": current.outputIndex, "item": item}),
	}
	b.output = append(b.output, item)
	b.outputIndex++
	b.rawReasoning = nil
	return frames
}
