package continuation

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/Wibias/Benes/internal/protocol"
)

const PrefixCallID = "previous_response"

func OccurrencesFromMessages(messages []protocol.Message) []Occurrence {
	out := make([]Occurrence, 0, len(messages))
	for _, message := range messages {
		switch message.Role {
		case protocol.RoleUser, protocol.RoleDeveloper:
			out = append(out, Occurrence{
				Kind:    string(message.Role),
				Payload: messagePayload(message),
			})
		case protocol.RoleAssistant:
			text, calls := splitAssistantOccurrences(message)
			if len(text.Payload) > 0 || text.ID != "" {
				out = append(out, text)
			}
			out = append(out, calls...)
		case protocol.RoleToolResult:
			out = append(out, Occurrence{
				Kind:    "function_call_output",
				CallID:  message.ToolCallID,
				Payload: toolResultPayload(message),
			})
		}
	}
	return out
}

func MessagesFromOccurrences(items []Occurrence) []protocol.Message {
	out := make([]protocol.Message, 0, len(items))
	for _, item := range items {
		switch item.Kind {
		case string(protocol.RoleUser):
			out = append(out, protocol.Message{
				Role:    protocol.RoleUser,
				Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: payloadText(item.Payload)}},
			})
		case string(protocol.RoleDeveloper):
			out = append(out, protocol.Message{
				Role:    protocol.RoleDeveloper,
				Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: payloadText(item.Payload)}},
			})
		case string(protocol.RoleAssistant), "message":
			out = append(out, protocol.Message{
				Role: protocol.RoleAssistant,
				Content: []protocol.ContentPart{{
					Type:   protocol.ContentText,
					Text:   payloadText(item.Payload),
					ItemID: item.ID,
				}},
			})
		case "function_call":
			args := payloadObject(item.Payload, "arguments")
			name, _ := payloadString(item.Payload, "name")
			out = append(out, protocol.Message{
				Role: protocol.RoleAssistant,
				Content: []protocol.ContentPart{{
					Type:       protocol.ContentToolCall,
					ItemID:     item.ID,
					ToolCallID: item.CallID,
					ToolName:   name,
					Arguments:  args,
				}},
			})
		case "function_call_output":
			out = append(out, protocol.Message{
				Role:       protocol.RoleToolResult,
				ToolCallID: item.CallID,
				Content:    []protocol.ContentPart{{Type: protocol.ContentText, Text: payloadOutput(item.Payload)}},
			})
		}
	}
	return out
}

func OccurrencesFromRequestInput(raw json.RawMessage) ([]Occurrence, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, false
	}
	var body struct {
		Input json.RawMessage `json:"input"`
	}
	if json.Unmarshal(trimmed, &body) != nil {
		return nil, false
	}
	input := bytes.TrimSpace(body.Input)
	if len(input) == 0 || bytes.Equal(input, []byte("null")) {
		return nil, false
	}
	if input[0] == '"' {
		var text string
		if json.Unmarshal(input, &text) != nil {
			return nil, false
		}
		return []Occurrence{{Kind: "user", Payload: compactJSON(map[string]any{"text": text})}}, true
	}
	if input[0] != '[' {
		return nil, false
	}
	return OccurrencesFromOutput(input), true
}

func OccurrencesFromOutput(raw json.RawMessage) []Occurrence {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) || trimmed[0] != '[' {
		return nil
	}
	var items []json.RawMessage
	if json.Unmarshal(trimmed, &items) != nil {
		return nil
	}
	out := make([]Occurrence, 0, len(items))
	for _, rawItem := range items {
		var item map[string]json.RawMessage
		if json.Unmarshal(rawItem, &item) != nil {
			continue
		}
		typeName := rawJSONString(item["type"])
		id := rawJSONString(item["id"])
		callID := rawJSONString(item["call_id"])
		switch typeName {
		case "message", "":
			role := rawJSONString(item["role"])
			kind := "assistant"
			if role == "user" || role == "developer" {
				kind = role
			}
			out = append(out, Occurrence{
				Kind:    kind,
				ID:      id,
				Payload: compactJSON(map[string]any{"text": outputText(item["content"])}),
			})
		case "function_call":
			out = append(out, Occurrence{
				Kind:    "function_call",
				ID:      id,
				CallID:  callID,
				Payload: compactJSON(map[string]any{"name": rawJSONString(item["name"]), "arguments": rawJSONString(item["arguments"])}),
			})
		case "function_call_output":
			out = append(out, Occurrence{
				Kind:    "function_call_output",
				ID:      id,
				CallID:  callID,
				Payload: compactJSON(map[string]any{"output": rawJSONString(item["output"])}),
			})
		default:
			if id == "" && callID == "" {
				continue
			}
			out = append(out, Occurrence{Kind: typeName, ID: id, CallID: callID, Payload: append(json.RawMessage(nil), rawItem...)})
		}
	}
	return out
}

func splitAssistantOccurrences(message protocol.Message) (Occurrence, []Occurrence) {
	var text strings.Builder
	var id string
	calls := make([]Occurrence, 0)
	for _, part := range message.Content {
		switch part.Type {
		case protocol.ContentText:
			text.WriteString(part.Text)
			if id == "" {
				id = part.ItemID
			}
		case protocol.ContentToolCall:
			calls = append(calls, Occurrence{
				Kind:    "function_call",
				ID:      part.ItemID,
				CallID:  part.ToolCallID,
				Payload: compactJSON(map[string]any{"name": part.ToolName, "arguments": part.Arguments}),
			})
		}
	}
	return Occurrence{Kind: "assistant", ID: id, Payload: compactJSON(map[string]any{"text": text.String()})}, calls
}

func messagePayload(message protocol.Message) json.RawMessage {
	var text strings.Builder
	for _, part := range message.Content {
		if part.Type == protocol.ContentText {
			text.WriteString(part.Text)
		}
	}
	return compactJSON(map[string]any{"text": text.String()})
}

func toolResultPayload(message protocol.Message) json.RawMessage {
	var text strings.Builder
	for _, part := range message.Content {
		if part.Type == protocol.ContentText {
			text.WriteString(part.Text)
		}
	}
	return compactJSON(map[string]any{"output": text.String()})
}

func payloadText(raw json.RawMessage) string {
	if text, ok := payloadString(raw, "text"); ok {
		return text
	}
	return ""
}

func payloadOutput(raw json.RawMessage) string {
	if text, ok := payloadString(raw, "output"); ok {
		return text
	}
	return payloadText(raw)
}

func payloadString(raw json.RawMessage, key string) (string, bool) {
	var object map[string]any
	if json.Unmarshal(raw, &object) != nil {
		return "", false
	}
	value, _ := object[key].(string)
	return value, value != ""
}

func payloadObject(raw json.RawMessage, key string) map[string]any {
	var object map[string]any
	if json.Unmarshal(raw, &object) != nil {
		return map[string]any{}
	}
	if nested, ok := object[key].(map[string]any); ok {
		return nested
	}
	if encoded, ok := object[key].(string); ok && encoded != "" {
		var nested map[string]any
		if json.Unmarshal([]byte(encoded), &nested) == nil {
			return nested
		}
	}
	return map[string]any{}
}

func outputText(raw json.RawMessage) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return ""
	}
	if trimmed[0] == '"' {
		var text string
		_ = json.Unmarshal(trimmed, &text)
		return text
	}
	var parts []map[string]json.RawMessage
	if json.Unmarshal(trimmed, &parts) != nil {
		return ""
	}
	var text strings.Builder
	for _, part := range parts {
		if value := rawJSONString(part["text"]); value != "" {
			text.WriteString(value)
		}
	}
	return text.String()
}

func rawJSONString(raw json.RawMessage) string {
	var value string
	_ = json.Unmarshal(raw, &value)
	return value
}

func compactJSON(value any) json.RawMessage {
	encoded, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return encoded
}

func thoughtSignatureFromPart(part protocol.ContentPart) string {
	if strings.TrimSpace(part.ThoughtSignature) != "" {
		return part.ThoughtSignature
	}
	if part.ProviderMetadata != nil && part.ProviderMetadata.Google != nil {
		return strings.TrimSpace(part.ProviderMetadata.Google.ThoughtSignature)
	}
	return ""
}

func outputThoughtSignature(item map[string]json.RawMessage) string {
	var extra struct {
		Google struct {
			ThoughtSignature string `json:"thought_signature"`
		} `json:"google"`
	}
	if json.Unmarshal(item["extra_content"], &extra) == nil && extra.Google.ThoughtSignature != "" {
		return extra.Google.ThoughtSignature
	}
	return rawJSONString(item["thought_signature"])
}
