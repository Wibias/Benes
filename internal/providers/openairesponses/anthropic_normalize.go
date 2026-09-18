package openairesponses

import (
	"strings"

	"github.com/Wibias/Benes/internal/protocol"
)

const anthropicToolErrorMarker = "[tool error]"

func normalizeAnthropicToolErrors(ctx protocol.Context) protocol.Context {
	out := ctx
	out.Messages = append([]protocol.Message(nil), ctx.Messages...)
	for index := range out.Messages {
		message := out.Messages[index]
		if message.Role != protocol.RoleToolResult || !message.IsError {
			continue
		}
		message.Content = markAnthropicToolError(message.Content)
		out.Messages[index] = message
	}
	return out
}

func markAnthropicToolError(parts []protocol.ContentPart) []protocol.ContentPart {
	hasImage := false
	for _, part := range parts {
		if part.Type == protocol.ContentImage {
			hasImage = true
			break
		}
	}
	if hasImage {
		out := make([]protocol.ContentPart, 0, len(parts)+1)
		out = append(out, protocol.ContentPart{Type: protocol.ContentText, Text: anthropicToolErrorMarker})
		out = append(out, parts...)
		return out
	}

	var text strings.Builder
	for _, part := range parts {
		if part.Type == protocol.ContentText {
			text.WriteString(part.Text)
		}
	}
	value := text.String()
	if value == "" {
		value = anthropicToolErrorMarker
	} else {
		value = anthropicToolErrorMarker + " " + value
	}
	return []protocol.ContentPart{{Type: protocol.ContentText, Text: value}}
}
