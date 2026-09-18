package antigravity

import (
	"strings"

	"github.com/Wibias/Benes/internal/protocol"
)

const InterleavedThinkingBeta = "interleaved-thinking-2025-05-14"

type ClaudeTurn struct {
	Role      string
	Text      string
	CallID    string
	Tool      string
	Result    string
	Signature string
	HasCall   bool
	HasOut    bool
}

func RepairClaudeTurns(messages []protocol.Message) []ClaudeTurn {
	out := make([]ClaudeTurn, 0, len(messages))
	open := map[string]int{}
	for _, message := range messages {
		switch message.Role {
		case protocol.RoleAssistant:
			turn := ClaudeTurn{Role: "assistant", Text: joinText(message.Content)}
			for _, part := range message.Content {
				if part.Type != protocol.ContentToolCall {
					continue
				}
				id := strings.TrimSpace(part.ToolCallID)
				if id == "" {
					continue
				}
				turn.HasCall = true
				turn.CallID = id
				turn.Tool = part.ToolName
				if sig := thoughtSignature(part); sig != "" {
					turn.Signature = sig
				}
				open[id] = len(out)
			}
			out = append(out, turn)
		case protocol.RoleToolResult:
			id := strings.TrimSpace(message.ToolCallID)
			if id == "" {
				continue
			}
			idx, ok := open[id]
			if !ok {
				continue
			}
			out[idx].HasOut = true
			out[idx].Result = joinText(message.Content)
			delete(open, id)
		case protocol.RoleUser, protocol.RoleDeveloper:
			out = append(out, ClaudeTurn{Role: "user", Text: joinText(message.Content)})
		}
	}
	kept := out[:0]
	for _, turn := range out {
		if turn.HasCall && !turn.HasOut {
			continue
		}
		kept = append(kept, turn)
	}
	return kept
}

func ContinuationShape(turns []ClaudeTurn) string {
	if len(turns) == 0 {
		return "continue"
	}
	last := turns[len(turns)-1]
	if last.Role == "assistant" && strings.TrimSpace(last.Text) == "" && !last.HasCall {
		return "continue"
	}
	return ""
}

func UseReplacementPreamble(claude bool) bool {
	return claude
}

func joinText(parts []protocol.ContentPart) string {
	var b strings.Builder
	for _, part := range parts {
		if part.Type == protocol.ContentText {
			b.WriteString(part.Text)
		}
	}
	return b.String()
}

func thoughtSignature(part protocol.ContentPart) string {
	sig := strings.TrimSpace(part.ThoughtSignature)
	if sig != "" {
		return sig
	}
	if part.ProviderMetadata != nil && part.ProviderMetadata.Google != nil {
		return strings.TrimSpace(part.ProviderMetadata.Google.ThoughtSignature)
	}
	return ""
}
