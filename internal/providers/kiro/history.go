package kiro

import (
	"fmt"
	"strings"

	"github.com/Wibias/Benes/internal/protocol"
)

const (
	emptyToolResultText = "The tool completed without textual output."
	toolResultCarrier   = "The requested tool result is attached."
)

type ToolUse struct {
	ID    string
	Name  string
	Input map[string]any
}

type ToolResult struct {
	ID     string
	Text   string
	Error  bool
	Images []Image
}

type HistoryEntry struct {
	Role        string
	Text        string
	ToolIDs     []string
	Results     []string
	Images      []Image
	ToolUses    []ToolUse
	ToolResults []ToolResult
	Redacted    string
}

func CompileHistory(parsed protocol.ParsedRequest) ([]HistoryEntry, error) {
	return CompileHistoryWithRegistry(parsed, NewToolNameRegistry())
}

func CompileHistoryWithRegistry(parsed protocol.ParsedRequest, names *ToolNameRegistry) ([]HistoryEntry, error) {
	if names == nil {
		names = NewToolNameRegistry()
	}
	for _, tool := range parsed.Context.Tools {
		if _, err := names.Alias(NamespacedToolName(tool.Namespace, tool.Name)); err != nil {
			return nil, err
		}
	}
	var out []HistoryEntry
	for _, message := range parsed.Context.Messages {
		switch message.Role {
		case protocol.RoleUser, protocol.RoleDeveloper:
			text := joinText(message.Content)
			images, err := ExtractImages(message.Content)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(text) == "" && len(images) == 0 {
				return nil, fmt.Errorf("Kiro user messages must not be empty")
			}
			out = append(out, HistoryEntry{Role: "user", Text: text, Images: images})
		case protocol.RoleAssistant:
			text := joinText(message.Content)
			var uses []ToolUse
			var ids []string
			for _, part := range message.Content {
				if part.Type != protocol.ContentToolCall || part.ToolCallID == "" {
					continue
				}
				id := normalizeToolID(part.ToolCallID)
				alias, err := names.Alias(NamespacedToolName(part.ToolNamespace, part.ToolName))
				if err != nil {
					return nil, err
				}
				ids = append(ids, id)
				uses = append(uses, ToolUse{ID: id, Name: alias, Input: part.Arguments})
			}
			if strings.TrimSpace(text) == "" && len(ids) == 0 {
				if message.KiroRedactedReasoning != "" {
					continue
				}
				return nil, fmt.Errorf("Kiro assistant messages must not be empty")
			}
			out = append(out, HistoryEntry{Role: "assistant", Text: text, ToolIDs: ids, ToolUses: uses, Redacted: message.KiroRedactedReasoning})
		case protocol.RoleToolResult:
			if message.ContainsEncryptedContent {
				return nil, fmt.Errorf("Kiro cannot translate encrypted tool output")
			}
			if message.ToolCallID == "" {
				return nil, fmt.Errorf("Kiro tool result has no matching tool use")
			}
			text := joinText(message.Content)
			if strings.TrimSpace(text) == "" {
				text = emptyToolResultText
			}
			images, err := ExtractImages(message.Content)
			if err != nil {
				return nil, err
			}
			if len(out) == 0 || out[len(out)-1].Role != "user" {
				out = append(out, HistoryEntry{Role: "user"})
			}
			last := &out[len(out)-1]
			id := normalizeToolID(message.ToolCallID)
			last.Results = append(last.Results, id)
			last.ToolResults = append(last.ToolResults, ToolResult{ID: id, Text: text, Error: message.IsError, Images: images})
			last.Images = append(last.Images, images...)
		default:
			return nil, fmt.Errorf("Kiro conversation role %q is unsupported", message.Role)
		}
	}
	for i := range out {
		if out[i].Role == "user" && strings.TrimSpace(out[i].Text) == "" && len(out[i].ToolResults) > 0 {
			out[i].Text = toolResultCarrier
		}
	}
	return out, validateHistory(out)
}

func validateHistory(entries []HistoryEntry) error {
	pending := map[string]struct{}{}
	var prev string
	for _, entry := range entries {
		if entry.Role == prev {
			return fmt.Errorf("Kiro conversation roles must alternate")
		}
		prev = entry.Role
		switch entry.Role {
		case "user":
			if strings.TrimSpace(entry.Text) == "" && len(entry.Results) == 0 {
				return fmt.Errorf("Kiro user messages must not be empty")
			}
			for _, id := range entry.Results {
				if _, ok := pending[id]; !ok {
					return fmt.Errorf("Kiro tool result has no matching tool use %q", id)
				}
				delete(pending, id)
			}
		case "assistant":
			if strings.TrimSpace(entry.Text) == "" && len(entry.ToolIDs) == 0 {
				return fmt.Errorf("Kiro assistant messages must not be empty")
			}
			for _, id := range entry.ToolIDs {
				if _, exists := pending[id]; exists {
					return fmt.Errorf("Kiro conversation contains duplicate tool use %q", id)
				}
				pending[id] = struct{}{}
			}
		default:
			return fmt.Errorf("Kiro conversation entries must contain exactly one message role")
		}
	}
	if len(pending) > 0 {
		return fmt.Errorf("Kiro conversation contains an unanswered tool use")
	}
	return nil
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

func hasImage(message protocol.Message) bool {
	for _, part := range message.Content {
		if part.Type == protocol.ContentImage {
			return true
		}
	}
	return false
}
