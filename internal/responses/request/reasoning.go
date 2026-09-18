package request

import (
	"encoding/json"
	"fmt"
	"strings"

	benesreasoning "github.com/Wibias/Benes/internal/responses/reasoning"
)

// ReasoningItem is the wire-level view of a Responses reasoning input item.
// It does not decide which assistant turn owns the item; semantic history
// binding happens in a later layer.
type ReasoningItem struct {
	ItemID                string
	VisibleText           string
	EffectiveThinkingText string
	EncryptedContent      string
	HasEnvelope              bool
	Signature             string
	Redacted              []string
	KiroRedacted          string
}

func (item Item) DecodeReasoning() (ReasoningItem, bool, error) {
	if item.Type != "reasoning" {
		return ReasoningItem{}, false, nil
	}

	var wire struct {
		ID               any               `json:"id"`
		Summary          []json.RawMessage `json:"summary"`
		Content          []json.RawMessage `json:"content"`
		EncryptedContent any               `json:"encrypted_content"`
	}
	if err := json.Unmarshal(item.Raw, &wire); err != nil {
		return ReasoningItem{}, true, fmt.Errorf("decode reasoning item: %w", err)
	}

	out := ReasoningItem{}
	if id, ok := wire.ID.(string); ok {
		out.ItemID = id
	}
	fromSummary := joinTextFields(wire.Summary)
	fromContent := joinTextFields(wire.Content)
	out.VisibleText = fromSummary
	if out.VisibleText == "" {
		out.VisibleText = fromContent
	}
	if encrypted, ok := wire.EncryptedContent.(string); ok {
		out.EncryptedContent = encrypted
		if envelope, valid := benesreasoning.Decode(encrypted); valid {
			out.HasEnvelope = true
			out.Signature = envelope.Signature
			out.Redacted = append([]string(nil), envelope.Redacted...)
			out.KiroRedacted = envelope.KiroRedacted
			if envelope.Text != "" {
				out.EffectiveThinkingText = envelope.Text
			}
		}
	}
	if out.EffectiveThinkingText == "" {
		out.EffectiveThinkingText = out.VisibleText
	}
	return out, true, nil
}

func joinTextFields(values []json.RawMessage) string {
	var b strings.Builder
	for _, raw := range values {
		var value map[string]any
		if json.Unmarshal(raw, &value) != nil {
			continue
		}
		if text, ok := value["text"].(string); ok {
			b.WriteString(text)
		}
	}
	return b.String()
}
