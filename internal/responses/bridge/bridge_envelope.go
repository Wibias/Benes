package bridge

import benesreasoning "github.com/Wibias/Benes/internal/responses/reasoning"

func (b *Bridge) takeReasoningEnvelope(hiddenText string) string {
	if b.pendingSignature == "" && len(b.pendingRedacted) == 0 {
		return ""
	}
	envelope := benesreasoning.Envelope{}
	if b.pendingSignature != "" {
		envelope.Signature = b.pendingSignature
	}
	if len(b.pendingRedacted) > 0 {
		envelope.Redacted = append([]string(nil), b.pendingRedacted...)
	}
	if hiddenText != "" {
		envelope.Text = hiddenText
	}
	encoded, err := benesreasoning.Encode(envelope)
	if err != nil {
		return ""
	}
	b.pendingSignature = ""
	b.pendingRedacted = nil
	return encoded
}

func (b *Bridge) flushHiddenReasoningEnvelope() []Frame {
	encrypted := b.takeReasoningEnvelope(b.hiddenThinkingText)
	b.hiddenThinkingText = ""
	if encrypted == "" {
		return nil
	}
	return b.appendEnvelopeOnlyReasoning(encrypted)
}

func (b *Bridge) flushHiddenRawReasoning() []Frame {
	if b.hiddenRawReasoning == "" {
		return nil
	}
	encrypted, err := benesreasoning.Encode(benesreasoning.Envelope{Text: b.hiddenRawReasoning})
	b.hiddenRawReasoning = ""
	if err != nil {
		return nil
	}
	return b.appendEnvelopeOnlyReasoning(encrypted)
}

func (b *Bridge) flushKiroRedactedReasoning() []Frame {
	if b.pendingKiroRedacted == "" {
		return nil
	}
	encrypted, err := benesreasoning.Encode(benesreasoning.Envelope{KiroRedacted: b.pendingKiroRedacted})
	b.pendingKiroRedacted = ""
	if err != nil {
		return nil
	}
	return b.appendEnvelopeOnlyReasoning(encrypted)
}

func (b *Bridge) appendEnvelopeOnlyReasoning(encrypted string) []Frame {
	itemID := b.id("rs_")
	item := map[string]any{"type": "reasoning", "id": itemID, "summary": []any{}, "encrypted_content": encrypted}
	frames := []Frame{
		b.emit("response.output_item.added", map[string]any{"output_index": b.outputIndex, "item": item}),
		b.emit("response.output_item.done", map[string]any{"output_index": b.outputIndex, "item": item}),
	}
	b.output = append(b.output, item)
	b.outputIndex++
	return frames
}
