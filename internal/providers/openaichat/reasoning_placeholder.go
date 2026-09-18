package openaichat

import (
	"fmt"
	"strings"

	"github.com/Wibias/Benes/internal/protocol"
)

func withRequiredReasoningPlaceholders(request protocol.ParsedRequest) protocol.ParsedRequest {
	messages := request.Context.Messages
	if len(messages) == 0 {
		return request
	}

	seenCallIDs := make(map[string]struct{})
	for _, message := range messages {
		if id := strings.TrimSpace(message.ToolCallID); id != "" {
			seenCallIDs[id] = struct{}{}
		}
		for _, part := range message.Content {
			if part.Type != protocol.ContentToolCall {
				continue
			}
			if id := strings.TrimSpace(part.ToolCallID); id != "" {
				seenCallIDs[id] = struct{}{}
			}
		}
	}

	out := make([]protocol.Message, 0, len(messages)+2)
	pending := make(map[string]struct{})
	mintedOrphanID := 0
	changed := false

	mintOrphanID := func() string {
		for {
			mintedOrphanID++
			id := fmt.Sprintf("call_orphan_placeholder_%d", mintedOrphanID)
			if _, exists := seenCallIDs[id]; exists {
				continue
			}
			seenCallIDs[id] = struct{}{}
			return id
		}
	}

	for _, message := range messages {
		switch message.Role {
		case protocol.RoleAssistant:
			pending = make(map[string]struct{})
			hasToolCall := false
			var reasoning strings.Builder
			for _, part := range message.Content {
				switch part.Type {
				case protocol.ContentThinking:
					reasoning.WriteString(part.Thinking)
				case protocol.ContentToolCall:
					hasToolCall = true
					if id := strings.TrimSpace(part.ToolCallID); id != "" {
						pending[id] = struct{}{}
					}
				}
			}
			if hasToolCall && reasoning.Len() == 0 {
				copyMessage := message
				copyMessage.Content = make([]protocol.ContentPart, 0, len(message.Content)+1)
				copyMessage.Content = append(copyMessage.Content, protocol.ContentPart{
					Type:     protocol.ContentThinking,
					Thinking: " ",
				})
				copyMessage.Content = append(copyMessage.Content, message.Content...)
				message = copyMessage
				changed = true
			}
			out = append(out, message)

		case protocol.RoleToolResult:
			id := strings.TrimSpace(message.ToolCallID)
			if id != "" {
				if _, matches := pending[id]; matches {
					delete(pending, id)
					out = append(out, message)
					continue
				}
			}

			// The compiler flushes any unmatched pending calls before repairing an
			// orphan result. Mirror that boundary, then make the repair explicit in
			// canonical history so the required reasoning placeholder rides the
			// synthetic assistant tool call without changing the compiler itself.
			pending = make(map[string]struct{})
			copyResult := message
			if id == "" {
				id = mintOrphanID()
				copyResult.ToolCallID = id
			}
			name := wireToolName(message.ToolNamespace, message.ToolName, "")
			if strings.TrimSpace(name) == "" {
				name = "tool_result"
			}
			name = safeToolName(name)
			out = append(out, protocol.Message{
				Role: protocol.RoleAssistant,
				Content: []protocol.ContentPart{
					{Type: protocol.ContentThinking, Thinking: " "},
					{
						Type:           protocol.ContentToolCall,
						ToolCallID:     id,
						CustomWireName: name,
						Arguments:      map[string]any{},
					},
				},
			}, copyResult)
			changed = true

		default:
			out = append(out, message)
		}
	}

	if !changed {
		return request
	}
	request.Context.Messages = out
	return request
}
