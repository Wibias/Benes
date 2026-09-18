package history

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Wibias/Benes/internal/protocol"
)

func decodeMessage(raw json.RawMessage, roleHint string) (string, []protocol.ContentPart, *protocol.MessagePhase, error) {
	var w struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
		Phase   json.RawMessage `json:"phase"`
	}
	if err := json.Unmarshal(raw, &w); err != nil {
		return "", nil, nil, err
	}
	if w.Role == "" {
		w.Role = roleHint
	}
	switch w.Role {
	case "system", "user", "developer", "assistant":
	default:
		return "", nil, nil, fmt.Errorf("unsupported message role %q", w.Role)
	}
	parts, err := decodeMessageContent(w.Role, w.Content)
	if err != nil {
		return "", nil, nil, err
	}
	var phase *protocol.MessagePhase
	if len(bytes.TrimSpace(w.Phase)) > 0 {
		if w.Role != "assistant" {
			return "", nil, nil, fmt.Errorf("phase only valid on assistant message")
		}
		var p string
		if err := json.Unmarshal(w.Phase, &p); err != nil {
			return "", nil, nil, fmt.Errorf("assistant phase: %w", err)
		}
		if p != "commentary" && p != "final_answer" {
			return "", nil, nil, fmt.Errorf("invalid assistant phase %q", p)
		}
		x := protocol.MessagePhase(p)
		phase = &x
	}
	return w.Role, parts, phase, nil
}
func decodeMessageContent(role string, raw json.RawMessage) ([]protocol.ContentPart, error) {
	if role == "assistant" {
		return decodeAssistantContent(raw), nil
	}
	return decodeInputContent(raw), nil
}

func decodeInputContentFromItem(raw json.RawMessage) ([]protocol.ContentPart, error) {
	var w struct {
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(raw, &w); err != nil {
		return nil, err
	}
	return decodeInputContent(w.Content), nil
}

func decodeInputContent(raw json.RawMessage) []protocol.ContentPart {
	t := bytes.TrimSpace(raw)
	if len(t) == 0 || bytes.Equal(t, []byte("null")) {
		return nil
	}
	if t[0] == '"' {
		var text string
		if json.Unmarshal(t, &text) == nil {
			return []protocol.ContentPart{{Type: protocol.ContentText, Text: text}}
		}
		return nil
	}
	if t[0] != '[' {
		return nil
	}
	var blocks []json.RawMessage
	if json.Unmarshal(t, &blocks) != nil {
		return nil
	}
	parts := make([]protocol.ContentPart, 0, len(blocks))
	for _, rawBlock := range blocks {
		var block map[string]json.RawMessage
		if json.Unmarshal(rawBlock, &block) != nil {
			continue
		}
		typ, ok := optionalString(block["type"])
		if !ok {
			continue
		}
		switch typ {
		case "input_text", "text":
			if text, ok := optionalString(block["text"]); ok {
				parts = append(parts, protocol.ContentPart{Type: protocol.ContentText, Text: text})
			}
		case "input_image":
			imageURL, hasURL := nonEmptyString(block["image_url"])
			fileID, hasFile := nonEmptyString(block["file_id"])
			detail, hasDetail := nonEmptyString(block["detail"])
			if hasURL || hasFile {
				part := protocol.ContentPart{Type: protocol.ContentImage, ImageURL: imageURL, FileID: fileID}
				if hasDetail {
					part.Detail = normalizeImageDetail(detail)
				}
				parts = append(parts, part)
			}
		case "input_file":
			fileID, hasFile := nonEmptyString(block["file_id"])
			fileData, hasData := nonEmptyString(block["file_data"])
			filename, hasName := nonEmptyString(block["filename"])
			if hasFile || hasData || hasName {
				parts = append(parts, protocol.ContentPart{
					Type: protocol.ContentFile, FileID: fileID, FileData: fileData, Filename: filename,
				})
			}
		}
	}
	return parts
}

func decodeAssistantContent(raw json.RawMessage) []protocol.ContentPart {
	t := bytes.TrimSpace(raw)
	if len(t) == 0 || bytes.Equal(t, []byte("null")) {
		return nil
	}
	if t[0] == '"' {
		var text string
		if json.Unmarshal(t, &text) == nil && text != "" {
			return []protocol.ContentPart{{Type: protocol.ContentText, Text: text}}
		}
		return nil
	}
	if t[0] != '[' {
		return nil
	}
	var blocks []json.RawMessage
	if json.Unmarshal(t, &blocks) != nil {
		return nil
	}
	parts := make([]protocol.ContentPart, 0, len(blocks))
	for _, rawBlock := range blocks {
		var block map[string]json.RawMessage
		if json.Unmarshal(rawBlock, &block) != nil {
			continue
		}
		typ, ok := optionalString(block["type"])
		if !ok {
			continue
		}
		switch typ {
		case "output_text", "text":
			if text, ok := optionalString(block["text"]); ok {
				parts = append(parts, protocol.ContentPart{Type: protocol.ContentText, Text: text})
			}
		case "refusal":
			if refusal, ok := optionalString(block["refusal"]); ok {
				parts = append(parts, protocol.ContentPart{Type: protocol.ContentText, Text: "[refusal: " + refusal + "]"})
			}
		}
	}
	return parts
}

func hasAgentContent(parts []protocol.ContentPart) bool {
	if len(parts) == 0 {
		return false
	}
	if len(parts) == 1 && parts[0].Type == protocol.ContentText {
		return strings.TrimSpace(parts[0].Text) != ""
	}
	return true
}

func normalizeImageDetail(detail string) string {
	if detail == "original" {
		return "high"
	}
	return detail
}

func optionalString(raw json.RawMessage) (string, bool) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return "", false
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return "", false
	}
	return value, true
}

func nonEmptyString(raw json.RawMessage) (string, bool) {
	value, ok := optionalString(raw)
	return value, ok && value != ""
}

func joinText(parts []protocol.ContentPart) string {
	var b strings.Builder
	for _, p := range parts {
		if p.Type == protocol.ContentText {
			b.WriteString(p.Text)
		}
	}
	return b.String()
}
