package history

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Wibias/Benes/internal/compaction"
	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/responses/request"
	"github.com/Wibias/Benes/internal/responses/websearchcall"
)

var ErrUnsupportedItem = errors.New("responses history item is not implemented by this migration slice")

type pendingThinking struct {
	part           protocol.ContentPart
	envelopeSigned bool
}

func Build(req *request.Request, now int64) (protocol.Context, error) {
	if req == nil {
		return protocol.Context{}, fmt.Errorf("request is nil")
	}
	input := req.Input
	model := req.Model
	ctx := protocol.Context{}
	if raw := bytes.TrimSpace(req.Instructions); len(raw) > 0 && !bytes.Equal(raw, []byte("null")) {
		var instruction string
		if err := json.Unmarshal(raw, &instruction); err != nil {
			return protocol.Context{}, fmt.Errorf("instructions: %w", err)
		}
		if instruction != "" {
			ctx.SystemPrompt = append(ctx.SystemPrompt, instruction)
		}
	}
	var pending []pendingThinking
	var loadedToolSpecs []json.RawMessage
	if input.Text != nil {
		ctx.Messages = append(ctx.Messages, userMessage(*input.Text, now))
	} else {
		for i, item := range input.Items {
			effective := item.Type
			if effective == "" && item.Role != "" {
				effective = "message"
			}
			switch effective {
			case "additional_tools":
				loadedToolSpecs = append(loadedToolSpecs, toolSpecsFromItem(item.Raw)...)
			case "tool_search_call":
				call := decodeToolSearchCall(item.Raw)
				holder := assistantHolder(&ctx, model, now)
				for _, p := range pending {
					holder.Content = append(holder.Content, p.part)
				}
				pending = nil
				holder.Content = append(holder.Content, call)
			case "tool_search_output":
				out := decodeToolSearchOutput(item.Raw)
				pending = nil
				loadedToolSpecs = append(loadedToolSpecs, out.specs...)
				ctx.Messages = append(ctx.Messages, toolSearchResultMessage(out, now))
			case "agent_message":
				content, err := decodeInputContentFromItem(item.Raw)
				if err != nil {
					return protocol.Context{}, fmt.Errorf("input[%d]: %w", i, err)
				}
				pending = nil
				if !hasAgentContent(content) {
					content = []protocol.ContentPart{{Type: protocol.ContentText, Text: "(sub-agent message received)"}}
				}
				ctx.Messages = append(ctx.Messages, protocol.Message{Role: protocol.RoleUser, Content: content, Timestamp: now})
			case "message":
				role, content, phase, err := decodeMessage(item.Raw, item.Role)
				if err != nil {
					return protocol.Context{}, fmt.Errorf("input[%d]: %w", i, err)
				}
				switch role {
				case "system":
					pending = nil
					flat := joinText(content)
					if flat != "" {
						ctx.SystemPrompt = append(ctx.SystemPrompt, flat)
					}
				case "user", "developer":
					pending = nil
					m := protocol.Message{Role: protocol.MessageRole(role), Content: content, Timestamp: now}
					ctx.Messages = append(ctx.Messages, m)
				case "assistant":
					m := protocol.Message{Role: protocol.RoleAssistant, Model: model, Timestamp: now, Phase: phase}
					for _, p := range pending {
						m.Content = append(m.Content, p.part)
					}
					pending = nil
					m.Content = append(m.Content, content...)
					ctx.Messages = append(ctx.Messages, m)
				default:
					return protocol.Context{}, fmt.Errorf("input[%d]: unsupported message role %q", i, role)
				}
			case "reasoning":
				canonical, err := canonicalReasoningSignature(item.Raw)
				if err != nil {
					return protocol.Context{}, fmt.Errorf("input[%d]: %w", i, err)
				}
				r, ok, err := item.DecodeReasoning()
				if err != nil {
					return protocol.Context{}, fmt.Errorf("input[%d]: %w", i, err)
				}
				if !ok {
					return protocol.Context{}, fmt.Errorf("input[%d]: reasoning item not recognized", i)
				}
				if r.KiroReasoning.Value != "" && r.EffectiveThinkingText == "" {
					if len(ctx.Messages) > 0 && ctx.Messages[len(ctx.Messages)-1].Role == protocol.RoleAssistant {
						ctx.Messages[len(ctx.Messages)-1].KiroReasoning = r.KiroReasoning
					}
					continue
				}
				if r.EffectiveThinkingText == "" {
					continue
				}
				sig := r.Signature
				if sig == "" {
					sig = canonical
				}
				p := protocol.ContentPart{Type: protocol.ContentThinking, Thinking: r.EffectiveThinkingText, Signature: sig, ItemID: r.ItemID, Redacted: append([]string(nil), r.Redacted...)}
				signed := r.Signature != ""
				if !signed && len(pending) > 0 && !pending[len(pending)-1].envelopeSigned {
					p.Thinking = pending[len(pending)-1].part.Thinking + "\n" + p.Thinking
					pending[len(pending)-1] = pendingThinking{part: p, envelopeSigned: false}
				} else {
					pending = append(pending, pendingThinking{part: p, envelopeSigned: signed})
				}
			case websearchcall.ItemType:
				call, err := decodeWebSearchCall(item.Raw)
				if err != nil {
					return protocol.Context{}, fmt.Errorf("input[%d]: %w", i, err)
				}
				holder := assistantHolder(&ctx, model, now)
				for _, p := range pending {
					holder.Content = append(holder.Content, p.part)
				}
				pending = nil
				holder.Content = append(holder.Content, call)
			case "function_call":
				call, err := decodeFunctionCall(item.Raw)
				if err != nil {
					return protocol.Context{}, fmt.Errorf("input[%d]: %w", i, err)
				}
				holder := assistantHolder(&ctx, model, now)
				for _, p := range pending {
					holder.Content = append(holder.Content, p.part)
				}
				pending = nil
				holder.Content = append(holder.Content, call)
			case "custom_tool_call":
				call, err := decodeCustomToolCall(item.Raw)
				if err != nil {
					return protocol.Context{}, fmt.Errorf("input[%d]: %w", i, err)
				}
				holder := assistantHolder(&ctx, model, now)
				for _, p := range pending {
					holder.Content = append(holder.Content, p.part)
				}
				pending = nil
				holder.Content = append(holder.Content, call)
			case "local_shell_call":
				call, ok, err := decodeLocalShellCall(item.Raw)
				if err != nil {
					return protocol.Context{}, fmt.Errorf("input[%d]: %w", i, err)
				}
				if !ok {
					continue
				}
				holder := assistantHolder(&ctx, model, now)
				for _, p := range pending {
					holder.Content = append(holder.Content, p.part)
				}
				pending = nil
				holder.Content = append(holder.Content, call)
			case "compaction_trigger":
				pending = nil
			case "compaction", "compaction_summary", "context_compaction":
				pending = nil
				encrypted, present, err := encryptedContent(item.Raw)
				if err != nil {
					return protocol.Context{}, fmt.Errorf("input[%d]: %w", i, err)
				}
				if effective == "context_compaction" && !present {
					continue
				}
				ctx.Messages = append(ctx.Messages, userMessage(compaction.ItemToText(encrypted), now))
			case "function_call_output", "custom_tool_call_output":
				out, err := decodeToolOutput(item.Raw)
				if err != nil {
					return protocol.Context{}, fmt.Errorf("input[%d]: %w", i, err)
				}
				attachPendingToOwner(&ctx, out.callID, pending)
				pending = nil
				name, namespace := findTool(&ctx, out.callID)
				ctx.Messages = append(ctx.Messages, protocol.Message{Role: protocol.RoleToolResult, ToolCallID: out.callID, ToolName: name, ToolNamespace: namespace, Content: out.content, ContainsEncryptedContent: out.containsEncrypted, Timestamp: now})
			default:
				return protocol.Context{}, fmt.Errorf("input[%d] type %q: %w", i, effective, ErrUnsupportedItem)
			}
		}
	}
	ctx.Tools = buildToolCatalog(req.Tools, loadedToolSpecs)
	return ctx, nil
}

func userMessage(text string, now int64) protocol.Message {
	return protocol.Message{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: text}}, Timestamp: now}
}

func encryptedContent(raw json.RawMessage) (string, bool, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return "", false, err
	}
	value, ok := fields["encrypted_content"]
	if !ok || len(bytes.TrimSpace(value)) == 0 || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return "", false, nil
	}
	var encrypted string
	if err := json.Unmarshal(value, &encrypted); err != nil {
		return "", false, fmt.Errorf("encrypted_content must be string")
	}
	return encrypted, true, nil
}
func assistantHolder(ctx *protocol.Context, model string, now int64) *protocol.Message {
	if n := len(ctx.Messages); n > 0 && ctx.Messages[n-1].Role == protocol.RoleAssistant {
		return &ctx.Messages[n-1]
	}
	ctx.Messages = append(ctx.Messages, protocol.Message{Role: protocol.RoleAssistant, Model: model, Timestamp: now})
	return &ctx.Messages[len(ctx.Messages)-1]
}
func attachPendingToOwner(ctx *protocol.Context, id string, pending []pendingThinking) {
	if len(pending) == 0 || id == "" {
		return
	}
	for i := len(ctx.Messages) - 1; i >= 0; i-- {
		m := &ctx.Messages[i]
		if m.Role != protocol.RoleAssistant {
			continue
		}
		for _, p := range m.Content {
			if p.Type == protocol.ContentToolCall && p.ToolCallID == id {
				prefix := make([]protocol.ContentPart, 0, len(pending)+len(m.Content))
				for _, x := range pending {
					prefix = append(prefix, x.part)
				}
				m.Content = append(prefix, m.Content...)
				return
			}
		}
	}
}
func findTool(ctx *protocol.Context, id string) (string, string) {
	for i := len(ctx.Messages) - 1; i >= 0; i-- {
		m := ctx.Messages[i]
		if m.Role != protocol.RoleAssistant {
			continue
		}
		for _, p := range m.Content {
			if p.Type == protocol.ContentToolCall && p.ToolCallID == id {
				return p.ToolName, p.ToolNamespace
			}
		}
	}
	return "", ""
}
