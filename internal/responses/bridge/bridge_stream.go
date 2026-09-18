package bridge

import (
	"encoding/json"
	"github.com/Wibias/Benes/internal/compaction"
	"github.com/Wibias/Benes/internal/protocol"
)

func (b *Bridge) handleAssistantBoundary() ([]Frame, error) {
	frames := b.closeMessage(protocol.PhaseCommentary)
	frames = append(frames, b.closeReasoning()...)
	frames = append(frames, b.closeRawReasoning()...)
	frames = append(frames, b.flushHiddenRawReasoning()...)
	if b.tool != nil {
		closed, err := b.closeToolCompleted()
		frames = append(frames, closed...)
		if err != nil {
			frames = append(frames, b.fail("upstream stream produced malformed tool call arguments", nil)...)
			return frames, err
		}
	}
	frames = append(frames, b.flushHiddenReasoningEnvelope()...)
	return frames, nil
}

func (b *Bridge) handleText(event protocol.Event) ([]Frame, error) {
	if b.compactionRequest {
		b.compactionText += event.Text
		return nil, nil
	}
	frames := make([]Frame, 0, 10)
	frames = append(frames, b.closeReasoning()...)
	frames = append(frames, b.closeRawReasoning()...)
	frames = append(frames, b.flushHiddenRawReasoning()...)
	if b.tool != nil {
		closed, err := b.closeToolCompleted()
		frames = append(frames, closed...)
		if err != nil {
			frames = append(frames, b.fail("upstream stream produced malformed tool call arguments", nil)...)
			return frames, err
		}
	}
	if b.message != nil && event.Phase != nil && !samePhase(b.message.phase, event.Phase) {
		frames = append(frames, b.closeMessage(protocol.PhaseCommentary)...)
	}
	if b.message == nil {
		itemID := b.id("msg_")
		item := map[string]any{
			"type": "message", "id": itemID, "status": "in_progress", "role": "assistant",
			"content": []any{},
		}
		if event.Phase != nil {
			item["phase"] = string(*event.Phase)
		}
		frames = append(frames,
			b.emit("response.output_item.added", map[string]any{"output_index": b.outputIndex, "item": item}),
			b.emit("response.content_part.added", map[string]any{
				"item_id": itemID, "output_index": b.outputIndex, "content_index": 0,
				"part": map[string]any{"type": "output_text", "text": "", "annotations": []any{}},
			}),
		)
		b.message = &openMessage{id: itemID, outputIndex: b.outputIndex, phase: clonePhase(event.Phase)}
	}
	b.message.text += event.Text
	frames = append(frames, b.emit("response.output_text.delta", map[string]any{
		"item_id": b.message.id, "output_index": b.message.outputIndex, "content_index": 0, "delta": event.Text,
		"logprobs": []any{},
	}))
	return frames, nil
}

func (b *Bridge) handleThinking(event protocol.Event) ([]Frame, error) {
	if b.hideThinkingSummary {
		frames := b.flushHiddenRawReasoning()
		b.hiddenThinkingText += event.Thinking
		return frames, nil
	}
	frames := b.closeMessage(protocol.PhaseCommentary)
	frames = append(frames, b.closeRawReasoning()...)
	frames = append(frames, b.flushHiddenRawReasoning()...)
	if b.tool != nil {
		closed, err := b.closeToolCompleted()
		frames = append(frames, closed...)
		if err != nil {
			frames = append(frames, b.fail("upstream stream produced malformed tool call arguments", nil)...)
			return frames, err
		}
	}
	if b.reasoning == nil {
		itemID := b.id("rs_")
		item := map[string]any{"type": "reasoning", "id": itemID, "summary": []any{}}
		frames = append(frames,
			b.emit("response.output_item.added", map[string]any{"output_index": b.outputIndex, "item": item}),
			b.emit("response.reasoning_summary_part.added", map[string]any{
				"item_id": itemID, "output_index": b.outputIndex, "summary_index": 0,
				"part": map[string]any{"type": "summary_text", "text": ""},
			}),
		)
		b.reasoning = &openReasoning{id: itemID, outputIndex: b.outputIndex}
	}
	b.reasoning.text += event.Thinking
	frames = append(frames, b.emit("response.reasoning_summary_text.delta", map[string]any{
		"item_id": b.reasoning.id, "output_index": b.reasoning.outputIndex, "summary_index": 0, "delta": event.Thinking,
	}))
	return frames, nil
}

func (b *Bridge) handleRawReasoning(event protocol.Event) ([]Frame, error) {
	if b.hideThinkingSummary {
		b.hiddenRawReasoning += event.Text
		return nil, nil
	}
	frames := b.closeMessage(protocol.PhaseCommentary)
	frames = append(frames, b.closeReasoning()...)
	if b.tool != nil {
		closed, err := b.closeToolCompleted()
		frames = append(frames, closed...)
		if err != nil {
			frames = append(frames, b.fail("upstream stream produced malformed tool call arguments", nil)...)
			return frames, err
		}
	}
	if b.rawReasoning == nil {
		itemID := b.id("rs_")
		item := map[string]any{"type": "reasoning", "id": itemID, "summary": []any{}}
		frames = append(frames,
			b.emit("response.output_item.added", map[string]any{"output_index": b.outputIndex, "item": item}),
			b.emit("response.reasoning_summary_part.added", map[string]any{
				"item_id": itemID, "output_index": b.outputIndex, "summary_index": 0,
				"part": map[string]any{"type": "summary_text", "text": ""},
			}),
		)
		b.rawReasoning = &openReasoning{id: itemID, outputIndex: b.outputIndex}
	}
	b.rawReasoning.text += event.Text
	frames = append(frames, b.emit("response.reasoning_summary_text.delta", map[string]any{
		"item_id": b.rawReasoning.id, "output_index": b.rawReasoning.outputIndex, "summary_index": 0, "delta": event.Text,
	}))
	return frames, nil
}

func (b *Bridge) handleToolStart(event protocol.Event) ([]Frame, error) {
	frames := b.closeMessage(protocol.PhaseCommentary)
	frames = append(frames, b.closeReasoning()...)
	frames = append(frames, b.closeRawReasoning()...)
	frames = append(frames, b.flushHiddenRawReasoning()...)
	if b.tool != nil {
		closed, err := b.closeToolCompleted()
		frames = append(frames, closed...)
		if err != nil {
			frames = append(frames, b.fail("upstream stream produced malformed tool call arguments", nil)...)
			return frames, err
		}
	}
	itemID := b.id("fc_")
	item := map[string]any{
		"type": "function_call", "id": itemID, "call_id": event.ID,
		"name": event.Name, "arguments": "", "status": "in_progress",
	}
	if event.Namespace != "" {
		item["namespace"] = event.Namespace
	}
	frames = append(frames, b.emit("response.output_item.added", map[string]any{
		"output_index": b.outputIndex, "item": item,
	}))
	b.tool = &openTool{id: itemID, outputIndex: b.outputIndex, callID: event.ID, name: event.Name, namespace: event.Namespace, arguments: event.Arguments, providerMetadata: append(json.RawMessage(nil), event.ProviderMetadata...)}
	if event.Arguments != "" {
		frames = append(frames, b.emit("response.function_call_arguments.delta", map[string]any{
			"item_id": itemID, "output_index": b.outputIndex, "delta": event.Arguments,
		}))
	}
	return frames, nil
}

func (b *Bridge) handleWebSearchBegin(event protocol.Event) ([]Frame, error) {
	frames := b.closeMessage(protocol.PhaseCommentary)
	frames = append(frames, b.closeReasoning()...)
	frames = append(frames, b.closeRawReasoning()...)
	frames = append(frames, b.flushHiddenRawReasoning()...)
	if b.tool != nil {
		closed, err := b.closeToolCompleted()
		frames = append(frames, closed...)
		if err != nil {
			frames = append(frames, b.fail("upstream stream produced malformed tool call arguments", nil)...)
			return frames, err
		}
	}
	frames = append(frames, b.closeWebSearch("completed", nil, nil)...)
	itemID := b.id("ws_")
	frames = append(frames, b.emit("response.output_item.added", map[string]any{
		"output_index": b.outputIndex,
		"item":         map[string]any{"type": "web_search_call", "id": itemID, "status": "in_progress"},
	}))
	b.webSearch = &openWebSearch{id: itemID, eventID: event.ID, outputIndex: b.outputIndex}
	return frames, nil
}

func (b *Bridge) handleWebSearchEnd(event protocol.Event) []Frame {
	frames := make([]Frame, 0, 3)
	if b.webSearch == nil || b.webSearch.eventID != event.ID {
		frames = append(frames, b.closeWebSearch("completed", nil, nil)...)
		itemID := b.id("ws_")
		frames = append(frames, b.emit("response.output_item.added", map[string]any{
			"output_index": b.outputIndex,
			"item":         map[string]any{"type": "web_search_call", "id": itemID, "status": "in_progress"},
		}))
		b.webSearch = &openWebSearch{id: itemID, eventID: event.ID, outputIndex: b.outputIndex}
	}
	status := event.Status
	if status == "" {
		status = "completed"
	}
	frames = append(frames, b.closeWebSearch(status, event.Queries, event.Sources)...)
	b.addPendingSources(event.Sources)
	return frames
}

func (b *Bridge) handleDone(event protocol.Event) ([]Frame, error) {
	class := compaction.ClassifyEvent(event, b.tool != nil)
	frames := b.closeMessageIfOpen(event.StopReason == "" && class == compaction.ClassCompleted)
	frames = append(frames, b.closeReasoning()...)
	frames = append(frames, b.closeRawReasoning()...)
	frames = append(frames, b.flushHiddenRawReasoning()...)
	if !compaction.ReplacementAllowed(class) {

		frames = append(frames, b.failTool()...)
		frames = append(frames, b.closeWebSearch("failed", nil, nil)...)
		frames = append(frames, b.flushHiddenReasoningEnvelope()...)
		if class == compaction.ClassFailed {
			frames = append(frames, b.fail(firstNonEmpty(event.Message, "provider failed"), event.Usage)...)
			return frames, nil
		}
		response := b.snapshot("incomplete", responseUsage(b.model, b.requestedTier, event.Usage), event.EndTurn)

		response["incomplete_details"] = map[string]any{"reason": compaction.IncompleteReason(firstNonEmpty(event.StopReason, event.Reason))}
		frames = append(frames, b.emit("response.incomplete", map[string]any{"response": response}))
		b.terminated = true
		return frames, nil
	}
	if b.compactionRequest {
		frames = append(frames, b.closeWebSearch("completed", nil, nil)...)
		frames = append(frames, b.flushHiddenReasoningEnvelope()...)
		frames = append(frames, b.flushKiroRedactedReasoning()...)
		frames = append(frames, b.emitCompactionItem()...)
		response := b.snapshot("completed", responseUsage(b.model, b.requestedTier, event.Usage), event.EndTurn)
		frames = append(frames, b.emit("response.completed", map[string]any{"response": response}))
		b.terminated = true
		return frames, nil
	}
	if b.tool != nil {
		closed, err := b.closeToolCompleted()
		frames = append(frames, closed...)
		if err != nil {
			frames = append(frames, b.fail("upstream stream produced malformed tool call arguments", event.Usage)...)
			return frames, err
		}
	}
	frames = append(frames, b.closeWebSearch("completed", nil, nil)...)
	frames = append(frames, b.flushHiddenReasoningEnvelope()...)
	frames = append(frames, b.flushKiroRedactedReasoning()...)
	response := b.snapshot("completed", responseUsage(b.model, b.requestedTier, event.Usage), event.EndTurn)

	frames = append(frames, b.emit("response.completed", map[string]any{"response": response}))
	b.terminated = true
	return frames, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func (b *Bridge) emitCompactionItem() []Frame {
	return b.emitCompactionOutput(b.id("cmp_"), compaction.EncodeSummary(b.compactionText))
}

func (b *Bridge) handleNativeCompaction(event protocol.Event) ([]Frame, error) {
	if b.compactionRequest {
		return nil, nil
	}
	itemID := event.ID
	if itemID == "" {
		itemID = b.id("cmp_")
	}
	return b.emitCompactionOutput(itemID, event.Data), nil
}

func (b *Bridge) emitCompactionOutput(itemID, encrypted string) []Frame {
	item := map[string]any{
		"type":              "compaction",
		"id":                itemID,
		"encrypted_content": encrypted,
	}
	added := b.emit("response.output_item.added", map[string]any{"output_index": b.outputIndex, "item": item})
	done := b.emit("response.output_item.done", map[string]any{"output_index": b.outputIndex, "item": item})
	b.output = append(b.output, item)
	b.outputIndex++
	return []Frame{added, done}
}
