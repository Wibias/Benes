package history

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/responses/websearchcall"
)

const maxOpaqueSignatureBytes = 64 * 1024

type toolOutput struct {
	callID            string
	content           []protocol.ContentPart
	containsEncrypted bool
}

func decodeWebSearchCall(raw json.RawMessage) (protocol.ContentPart, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return protocol.ContentPart{}, err
	}
	callID, _ := optionalString(fields["id"])
	if callID == "" {
		callID, _ = optionalString(fields["call_id"])
	}
	if strings.TrimSpace(callID) == "" {
		return protocol.ContentPart{}, fmt.Errorf("web_search_call requires id: %w", websearchcall.ErrMalformedAction)
	}
	action, ok := objectValue(fields["action"])
	if !ok {
		return protocol.ContentPart{}, websearchcall.ErrMalformedAction
	}
	query, queries, err := websearchcall.Heal(action)
	if err != nil {
		return protocol.ContentPart{}, err
	}
	return websearchcall.HostedPart(callID, query, queries), nil
}

func decodeCustomToolCall(raw json.RawMessage) (protocol.ContentPart, error) {
	var w struct {
		CallID string `json:"call_id"`
		Name   string `json:"name"`
		Input  any    `json:"input"`
	}
	if err := json.Unmarshal(raw, &w); err != nil {
		return protocol.ContentPart{}, err
	}
	if w.CallID == "" || w.Name == "" {
		return protocol.ContentPart{}, fmt.Errorf("custom_tool_call requires call_id and name")
	}
	input := w.Input
	if input == nil {
		input = ""
	}
	return protocol.ContentPart{
		Type:           protocol.ContentToolCall,
		ToolCallID:     w.CallID,
		ToolName:       w.Name,
		Arguments:      map[string]any{"input": input},
		CustomWireName: w.Name,
	}, nil
}

func decodeLocalShellCall(raw json.RawMessage) (protocol.ContentPart, bool, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return protocol.ContentPart{}, false, err
	}
	callID, _ := optionalString(fields["call_id"])
	if callID == "" {
		callID, _ = optionalString(fields["id"])
	}
	if callID == "" {
		return protocol.ContentPart{}, false, nil
	}
	args := map[string]any{}
	if actionRaw := bytes.TrimSpace(fields["action"]); len(actionRaw) > 0 && !bytes.Equal(actionRaw, []byte("null")) {
		var action map[string]json.RawMessage
		if json.Unmarshal(actionRaw, &action) == nil {
			commandRaw := bytes.TrimSpace(action["command"])
			if len(commandRaw) > 0 && commandRaw[0] == '[' {
				var command []any
				if json.Unmarshal(commandRaw, &command) == nil {
					args["command"] = command
				}
			}
		}
	}
	return protocol.ContentPart{Type: protocol.ContentToolCall, ToolCallID: callID, ToolName: "shell", Arguments: args}, true, nil
}

func decodeFunctionCall(raw json.RawMessage) (protocol.ContentPart, error) {
	var w struct {
		CallID    string          `json:"call_id"`
		Name      string          `json:"name"`
		Namespace any             `json:"namespace"`
		Arguments any             `json:"arguments"`
		Extra     json.RawMessage `json:"extra_content"`
	}
	if err := json.Unmarshal(raw, &w); err != nil {
		return protocol.ContentPart{}, err
	}
	if w.CallID == "" || w.Name == "" {
		return protocol.ContentPart{}, fmt.Errorf("function_call requires call_id and name")
	}
	ns := ""
	if w.Namespace != nil {
		var ok bool
		ns, ok = w.Namespace.(string)
		if !ok {
			return protocol.ContentPart{}, fmt.Errorf("function_call namespace must be string")
		}
	}
	args := map[string]any{}
	if w.Arguments != nil {
		rawArg, ok := w.Arguments.(string)
		if !ok {
			return protocol.ContentPart{}, fmt.Errorf("function_call arguments must be string")
		}
		rawArg = strings.TrimSpace(rawArg)
		if rawArg != "" {
			var candidate any
			if json.Unmarshal([]byte(rawArg), &candidate) == nil {
				if obj, ok := candidate.(map[string]any); ok {
					args = obj
				}
			}
		}
	}
	part := protocol.ContentPart{Type: protocol.ContentToolCall, ToolCallID: w.CallID, ToolName: w.Name, ToolNamespace: ns, Arguments: args}
	if len(bytes.TrimSpace(w.Extra)) > 0 && !bytes.Equal(bytes.TrimSpace(w.Extra), []byte("null")) {
		part.ProviderMetadata = decodeProviderMetadata(w.Extra)
	}
	return part, nil
}

// Provider metadata is optional replay state. Malformed or oversized metadata
// must not poison otherwise valid Responses history.
func decodeProviderMetadata(raw json.RawMessage) *protocol.ProviderOpaqueMetadata {
	var extra map[string]json.RawMessage
	if json.Unmarshal(raw, &extra) != nil {
		return nil
	}
	googleRaw, ok := extra["google"]
	if !ok {
		return nil
	}
	var google map[string]json.RawMessage
	if json.Unmarshal(googleRaw, &google) != nil {
		return nil
	}
	sigRaw, ok := google["thought_signature"]
	if !ok {
		return nil
	}
	var sig string
	if json.Unmarshal(sigRaw, &sig) != nil || sig == "" || len(sig) > maxOpaqueSignatureBytes {
		return nil
	}
	return &protocol.ProviderOpaqueMetadata{Google: &protocol.GoogleOpaqueMetadata{ThoughtSignature: sig}}
}

func decodeToolOutput(raw json.RawMessage) (toolOutput, error) {
	var w struct {
		CallID string          `json:"call_id"`
		Output json.RawMessage `json:"output"`
	}
	if err := json.Unmarshal(raw, &w); err != nil {
		return toolOutput{}, err
	}
	if w.CallID == "" {
		return toolOutput{}, fmt.Errorf("tool output requires call_id")
	}
	return normalizeToolOutput(w.CallID, w.Output), nil
}

func normalizeToolOutput(callID string, raw json.RawMessage) toolOutput {
	t := bytes.TrimSpace(raw)
	if len(t) == 0 || bytes.Equal(t, []byte("null")) {
		return toolOutput{callID: callID, content: []protocol.ContentPart{{Type: protocol.ContentText}}}
	}
	if t[0] == '"' {
		var text string
		if json.Unmarshal(t, &text) == nil {
			return toolOutput{callID: callID, content: []protocol.ContentPart{{Type: protocol.ContentText, Text: text}}}
		}
		return toolOutput{callID: callID, content: []protocol.ContentPart{{Type: protocol.ContentText}}}
	}
	if t[0] != '[' {
		return toolOutput{callID: callID, content: []protocol.ContentPart{{Type: protocol.ContentText}}}
	}
	var blocks []json.RawMessage
	if json.Unmarshal(t, &blocks) != nil {
		return toolOutput{callID: callID, content: []protocol.ContentPart{{Type: protocol.ContentText}}}
	}
	parts := make([]protocol.ContentPart, 0, len(blocks))
	hasImage := false
	containsEncrypted := false
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
		case "output_text", "text", "input_text":
			if text, ok := optionalString(block["text"]); ok {
				parts = append(parts, protocol.ContentPart{Type: protocol.ContentText, Text: text})
			}
		case "refusal":
			if refusal, ok := optionalString(block["refusal"]); ok {
				parts = append(parts, protocol.ContentPart{Type: protocol.ContentText, Text: "[refusal: " + refusal + "]"})
			}
		case "input_image":
			if imageURL, ok := optionalString(block["image_url"]); ok {
				part := protocol.ContentPart{Type: protocol.ContentImage, ImageURL: imageURL}
				if detail, ok := optionalString(block["detail"]); ok {
					part.Detail = normalizeImageDetail(detail)
				}
				parts = append(parts, part)
				hasImage = true
			}
		case "encrypted_content":
			containsEncrypted = true
			parts = append(parts, protocol.ContentPart{Type: protocol.ContentText, Text: "[encrypted content omitted]"})
		}
	}
	if !hasImage {
		var b strings.Builder
		for _, part := range parts {
			if part.Type == protocol.ContentText {
				b.WriteString(part.Text)
			}
		}
		parts = []protocol.ContentPart{{Type: protocol.ContentText, Text: b.String()}}
	}
	return toolOutput{callID: callID, content: parts, containsEncrypted: containsEncrypted}
}
