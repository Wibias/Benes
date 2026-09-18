package request

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Wibias/Benes/internal/protocol"
)

func buildContext(systemRaw json.RawMessage, messages []json.RawMessage, toolsRaw json.RawMessage, now int64) (protocol.Context, error) {
	ctx := protocol.Context{SystemPrompt: systemPrompt(systemRaw)}
	knownNames := make(map[string]string)
	for index, raw := range messages {
		var msg rec
		if err := json.Unmarshal(raw, &msg); err != nil || msg == nil {
			return protocol.Context{}, fmt.Errorf("messages[%d] must be an object", index)
		}
		role, _ := stringField(msg, "role")
		switch role {
		case "system":
			ctx.SystemPrompt = append(ctx.SystemPrompt, contentTextParts(msg["content"])...)
		case "user":
			turns, err := decodeUserTurns(msg["content"], knownNames, now)
			if err != nil {
				return protocol.Context{}, fmt.Errorf("messages[%d]: %w", index, err)
			}
			ctx.Messages = append(ctx.Messages, turns...)
		case "assistant":
			message, ok, err := decodeAssistant(msg["content"], knownNames, now)
			if err != nil {
				return protocol.Context{}, fmt.Errorf("messages[%d]: %w", index, err)
			}
			if ok {
				ctx.Messages = append(ctx.Messages, message)
			}
		default:
			return protocol.Context{}, fmt.Errorf("messages[%d] has unsupported role %q", index, role)
		}
	}
	tools, err := decodeTools(toolsRaw)
	if err != nil {
		return protocol.Context{}, err
	}
	ctx.Tools = tools
	return ctx, nil
}

func systemPrompt(raw json.RawMessage) []string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	var text string
	if json.Unmarshal(trimmed, &text) == nil {
		if text == "" {
			return nil
		}
		return []string{text}
	}
	return contentTextParts(trimmed)
}

func contentTextParts(raw json.RawMessage) []string {
	trimmed := bytes.TrimSpace(raw)
	var direct string
	if json.Unmarshal(trimmed, &direct) == nil {
		if direct == "" {
			return nil
		}
		return []string{direct}
	}
	var blocks []json.RawMessage
	if json.Unmarshal(trimmed, &blocks) != nil {
		return nil
	}
	out := make([]string, 0, len(blocks))
	for _, rawBlock := range blocks {
		var block rec
		if json.Unmarshal(rawBlock, &block) != nil || block == nil {
			continue
		}
		typ, _ := stringField(block, "type")
		if typ != "text" {
			continue
		}
		if text, ok := stringField(block, "text"); ok {
			out = append(out, text)
		}
	}
	return out
}

func decodeUserTurns(raw json.RawMessage, knownNames map[string]string, now int64) ([]protocol.Message, error) {
	trimmed := bytes.TrimSpace(raw)
	var direct string
	if json.Unmarshal(trimmed, &direct) == nil {
		if direct == "" {
			return nil, nil
		}
		return []protocol.Message{{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: direct}}, Timestamp: now}}, nil
	}
	var blocks []json.RawMessage
	if err := json.Unmarshal(trimmed, &blocks); err != nil {
		return nil, fmt.Errorf("user content must be a string or array")
	}
	var out []protocol.Message
	pending := make([]protocol.ContentPart, 0)
	flushPending := func() {
		if len(pending) == 0 {
			return
		}
		out = append(out, protocol.Message{Role: protocol.RoleUser, Content: append([]protocol.ContentPart(nil), pending...), Timestamp: now})
		pending = pending[:0]
	}
	for index, rawBlock := range blocks {
		var block rec
		if err := json.Unmarshal(rawBlock, &block); err != nil || block == nil {
			return nil, fmt.Errorf("user content[%d] must be an object", index)
		}
		typ, _ := stringField(block, "type")
		switch typ {
		case "text":
			text, ok := stringField(block, "text")
			if !ok {
				return nil, fmt.Errorf("user text block requires text")
			}
			pending = append(pending, protocol.ContentPart{Type: protocol.ContentText, Text: text})
		case "image":
			imageURL, ok := anthropicImageURL(block)
			if !ok {
				return nil, fmt.Errorf("user image block has invalid source")
			}
			pending = append(pending, protocol.ContentPart{Type: protocol.ContentImage, ImageURL: imageURL})
		case "document":
			title, _ := stringField(block, "title")
			pending = append(pending, protocol.ContentPart{Type: protocol.ContentText, Text: documentMarker(title)})
		case "tool_result":
			flushPending()
			result, err := decodeToolResult(block, knownNames, now)
			if err != nil {
				return nil, err
			}
			out = append(out, result)
		default:
			return nil, fmt.Errorf("unsupported user content block type %q", typ)
		}
	}
	flushPending()
	return out, nil
}

func decodeAssistant(raw json.RawMessage, knownNames map[string]string, now int64) (protocol.Message, bool, error) {
	trimmed := bytes.TrimSpace(raw)
	var direct string
	if json.Unmarshal(trimmed, &direct) == nil {
		if direct == "" {
			return protocol.Message{}, false, nil
		}
		return protocol.Message{Role: protocol.RoleAssistant, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: direct}}, Timestamp: now}, true, nil
	}
	var blocks []json.RawMessage
	if err := json.Unmarshal(trimmed, &blocks); err != nil {
		return protocol.Message{}, false, fmt.Errorf("assistant content must be a string or array")
	}
	parts := make([]protocol.ContentPart, 0, len(blocks))
	for index, rawBlock := range blocks {
		var block rec
		if err := json.Unmarshal(rawBlock, &block); err != nil || block == nil {
			return protocol.Message{}, false, fmt.Errorf("assistant content[%d] must be an object", index)
		}
		typ, _ := stringField(block, "type")
		switch typ {
		case "thinking", "redacted_thinking":
			continue
		case "text":
			text, ok := stringField(block, "text")
			if !ok {
				return protocol.Message{}, false, fmt.Errorf("assistant text block requires text")
			}
			parts = append(parts, protocol.ContentPart{Type: protocol.ContentText, Text: text})
		case "tool_use":
			id, idOK := stringField(block, "id")
			name, nameOK := stringField(block, "name")
			if !idOK || strings.TrimSpace(id) == "" || !nameOK || strings.TrimSpace(name) == "" {
				return protocol.Message{}, false, fmt.Errorf("tool_use requires nonblank id and name")
			}
			var args map[string]any
			if rawInput, exists := block["input"]; exists {
				if err := json.Unmarshal(rawInput, &args); err != nil || args == nil {
					return protocol.Message{}, false, fmt.Errorf("tool_use %q input must be an object", id)
				}
			} else {
				args = map[string]any{}
			}
			knownNames[id] = name
			parts = append(parts, protocol.ContentPart{Type: protocol.ContentToolCall, ToolCallID: id, ToolName: name, Arguments: args})
		default:
			return protocol.Message{}, false, fmt.Errorf("unsupported assistant content block type %q", typ)
		}
	}
	if len(parts) == 0 {
		return protocol.Message{}, false, nil
	}
	return protocol.Message{Role: protocol.RoleAssistant, Content: parts, Timestamp: now}, true, nil
}

func decodeToolResult(block rec, knownNames map[string]string, now int64) (protocol.Message, error) {
	callID, ok := stringField(block, "tool_use_id")
	if !ok || strings.TrimSpace(callID) == "" {
		return protocol.Message{}, fmt.Errorf("tool_result requires tool_use_id")
	}
	parts, err := toolResultParts(block["content"])
	if err != nil {
		return protocol.Message{}, err
	}
	isError, _, err := optionalBoolField(block, "is_error")
	if err != nil {
		return protocol.Message{}, err
	}
	if len(parts) == 0 {
		parts = []protocol.ContentPart{{Type: protocol.ContentText, Text: ""}}
	}
	return protocol.Message{Role: protocol.RoleToolResult, ToolCallID: callID, ToolName: knownNames[callID], Content: parts, Timestamp: now, IsError: isError}, nil
}

func toolResultParts(raw json.RawMessage) ([]protocol.ContentPart, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	var direct string
	if json.Unmarshal(trimmed, &direct) == nil {
		return []protocol.ContentPart{{Type: protocol.ContentText, Text: direct}}, nil
	}
	var blocks []json.RawMessage
	if err := json.Unmarshal(trimmed, &blocks); err != nil {
		return nil, fmt.Errorf("tool_result content must be a string or array")
	}
	parts := make([]protocol.ContentPart, 0, len(blocks))
	for index, rawBlock := range blocks {
		var item rec
		if err := json.Unmarshal(rawBlock, &item); err != nil || item == nil {
			return nil, fmt.Errorf("tool_result content[%d] must be an object", index)
		}
		typ, _ := stringField(item, "type")
		switch typ {
		case "text":
			text, ok := stringField(item, "text")
			if !ok {
				return nil, fmt.Errorf("tool_result text block requires text")
			}
			parts = append(parts, protocol.ContentPart{Type: protocol.ContentText, Text: text})
		case "image":
			imageURL, ok := anthropicImageURL(item)
			if !ok {
				return nil, fmt.Errorf("tool_result image block has invalid source")
			}
			parts = append(parts, protocol.ContentPart{Type: protocol.ContentImage, ImageURL: imageURL})
		case "document":
			title, _ := stringField(item, "title")
			parts = append(parts, protocol.ContentPart{Type: protocol.ContentText, Text: documentMarker(title)})
		default:
			return nil, fmt.Errorf("unsupported tool_result content block type %q", typ)
		}
	}
	return parts, nil
}

func anthropicImageURL(block rec) (string, bool) {
	var source rec
	if err := json.Unmarshal(block["source"], &source); err != nil || source == nil {
		return "", false
	}
	typ, _ := stringField(source, "type")
	switch typ {
	case "base64":
		media, mediaOK := stringField(source, "media_type")
		data, dataOK := stringField(source, "data")
		if !mediaOK || strings.TrimSpace(media) == "" || !dataOK || strings.TrimSpace(data) == "" {
			return "", false
		}
		return "data:" + media + ";base64," + data, true
	case "url":
		value, ok := stringField(source, "url")
		if !ok || strings.TrimSpace(value) == "" {
			return "", false
		}
		return value, true
	default:
		return "", false
	}
}

func documentMarker(title string) string {
	if title == "" {
		return "[document]"
	}
	return "[document: " + title + "]"
}

func decodeTools(raw json.RawMessage) ([]protocol.Tool, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	var rows []json.RawMessage
	if err := json.Unmarshal(trimmed, &rows); err != nil {
		return nil, fmt.Errorf("tools must be an array")
	}
	out := make([]protocol.Tool, 0, len(rows))
	for index, rawRow := range rows {
		var tool rec
		if err := json.Unmarshal(rawRow, &tool); err != nil || tool == nil {
			return nil, fmt.Errorf("tools[%d] must be an object", index)
		}
		if typ, ok := stringField(tool, "type"); ok && strings.HasPrefix(typ, "web_search") {
			name, _ := stringField(tool, "name")
			if strings.TrimSpace(name) == "" {
				name = typ
			}
			out = append(out, protocol.Tool{Name: name, HostedWebSearch: true})
			continue
		}
		name, ok := stringField(tool, "name")
		if !ok || strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("tools[%d] requires name", index)
		}
		description, _ := stringField(tool, "description")
		params := map[string]any{}
		if rawSchema, exists := tool["input_schema"]; exists {
			if err := json.Unmarshal(rawSchema, &params); err != nil || params == nil {
				return nil, fmt.Errorf("tool %q input_schema must be an object", name)
			}
		}
		out = append(out, protocol.Tool{Name: name, Description: description, Parameters: params})
	}
	return out, nil
}
