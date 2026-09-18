package cursor

import (
	"fmt"
	"strings"

	"github.com/Wibias/Benes/internal/protocol"
)

type RunRequest struct {
	ModelDetails      map[string]any `json:"modelDetails"`
	ConversationState map[string]any `json:"conversationState"`
	Model             string         `json:"-"`
	Turns             []RunTurn      `json:"-"`
	Images            []string       `json:"-"`
	Digest            string         `json:"-"`
	System            []string       `json:"-"`
}

type RunTurn struct {
	Role       string         `json:"role"`
	Text       string         `json:"text,omitempty"`
	IsError    bool           `json:"isError,omitempty"`
	ToolCallID string         `json:"toolCallId,omitempty"`
	ToolName   string         `json:"toolName,omitempty"`
	Arguments  map[string]any `json:"arguments,omitempty"`
}

func CompileRun(parsed protocol.ParsedRequest) (RunRequest, error) {
	model := strings.TrimSpace(parsed.UpstreamModelID)
	if model == "" {
		model = strings.TrimSpace(parsed.ModelID)
	}
	images, err := PromoteActiveImages(parsed)
	if err != nil {
		return RunRequest{}, err
	}
	turns := make([]RunTurn, 0, len(parsed.Context.Messages))
	covered := make([]string, 0, len(parsed.Context.Messages))
	active := activeMessageIndex(parsed.Context.Messages)
	for i, message := range parsed.Context.Messages {
		if message.ContainsEncryptedContent {
			return RunRequest{}, fmt.Errorf("Cursor conversation contains encrypted content")
		}
		switch message.Role {
		case protocol.RoleUser, protocol.RoleDeveloper:
			text, err := compileUserText(message, i == active)
			if err != nil {
				return RunRequest{}, err
			}
			if text == "" && len(images) == 0 {
				text = "(empty)"
			}
			turns = append(turns, RunTurn{Role: "user", Text: text})
			covered = append(covered, text)
		case protocol.RoleAssistant:
			compiled, texts, err := compileAssistantTurns(message)
			if err != nil {
				return RunRequest{}, err
			}
			turns = append(turns, compiled...)
			covered = append(covered, texts...)
		case protocol.RoleToolResult:
			hasBlob := messageHasImage(message)
			normalized := NormalizeToolResult(joinCursorText(message.Content), nil, message.IsError, hasBlob)
			if hasBlob {
				normalized.Text = toolResultContentText(message)
			}
			turns = append(turns, RunTurn{
				Role:       "tool",
				Text:       normalized.Text,
				IsError:    normalized.IsError,
				ToolCallID: message.ToolCallID,
				ToolName:   namespacedToolName(message.ToolNamespace, message.ToolName),
			})
			covered = append(covered, normalized.Text)
		default:
			return RunRequest{}, fmt.Errorf("Cursor conversation role %q is unsupported", message.Role)
		}
	}
	if model == "" {
		return RunRequest{}, fmt.Errorf("Cursor model is required")
	}
	digest := PrefixDigest(parsed.Context.SystemPrompt, covered)
	wireTurns := make([]map[string]any, 0, len(turns))
	for _, turn := range turns {
		wireTurns = append(wireTurns, map[string]any{
			"role":    turn.Role,
			"text":    turn.Text,
			"isError": turn.IsError,
		})
	}
	return RunRequest{
		Model:  model,
		Turns:  turns,
		Images: images,
		Digest: digest,
		System: append([]string(nil), parsed.Context.SystemPrompt...),
		ModelDetails: map[string]any{
			"modelName": model,
		},
		ConversationState: map[string]any{
			"turns":  wireTurns,
			"images": images,
			"digest": digest,
		},
	}, nil
}

func activeMessageIndex(messages []protocol.Message) int {
	if len(messages) == 0 {
		return -1
	}
	if messages[len(messages)-1].Role == protocol.RoleToolResult {
		return -1
	}
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == protocol.RoleUser || messages[i].Role == protocol.RoleDeveloper {
			return i
		}
		if messages[i].Role == protocol.RoleToolResult {
			continue
		}
	}
	return -1
}

func compileUserText(message protocol.Message, active bool) (string, error) {
	var b strings.Builder
	for _, part := range message.Content {
		switch part.Type {
		case protocol.ContentText:
			b.WriteString(part.Text)
		case protocol.ContentImage:
			if active {
				continue
			}
			if strings.TrimSpace(part.ImageURL) != "" && !strings.HasPrefix(strings.TrimSpace(part.ImageURL), "data:") && !strings.HasPrefix(strings.TrimSpace(part.ImageURL), "[image") {
				return "", fmt.Errorf("Cursor history image is not a data: marker")
			}
			b.WriteString(imageHistoryMarker(part))
		default:
			return "", fmt.Errorf("Cursor user content type %q is unsupported", part.Type)
		}
	}
	return b.String(), nil
}

func compileAssistantTurns(message protocol.Message) ([]RunTurn, []string, error) {
	var turns []RunTurn
	var covered []string
	for _, part := range message.Content {
		switch part.Type {
		case protocol.ContentText:
			if part.Text == "" {
				continue
			}
			turns = append(turns, RunTurn{Role: "assistant", Text: part.Text})
			covered = append(covered, part.Text)
		case protocol.ContentThinking:
			if part.Thinking == "" {
				continue
			}
			turns = append(turns, RunTurn{Role: "thinking", Text: part.Thinking})
			covered = append(covered, part.Thinking)
		case protocol.ContentToolCall:
			if part.ToolCallID == "" && part.ToolName == "" {
				return nil, nil, fmt.Errorf("Cursor tool call is missing identity")
			}
			for _, value := range part.Arguments {
				if _, err := encodeProtoJSONValue(value); err != nil {
					return nil, nil, err
				}
			}
			name := namespacedToolName(part.ToolNamespace, part.ToolName)
			turns = append(turns, RunTurn{
				Role:       "toolCall",
				ToolCallID: part.ToolCallID,
				ToolName:   name,
				Arguments:  part.Arguments,
			})
			covered = append(covered, name)
		case protocol.ContentImage:
			marker := imageHistoryMarker(part)
			turns = append(turns, RunTurn{Role: "assistant", Text: marker})
			covered = append(covered, marker)
		default:
			return nil, nil, fmt.Errorf("Cursor assistant content type %q is unsupported", part.Type)
		}
	}
	return turns, covered, nil
}

func messageHasImage(message protocol.Message) bool {
	for _, part := range message.Content {
		if part.Type == protocol.ContentImage {
			return true
		}
	}
	return false
}

func toolResultContentText(message protocol.Message) string {
	var b strings.Builder
	for _, part := range message.Content {
		switch part.Type {
		case protocol.ContentText:
			b.WriteString(part.Text)
		case protocol.ContentImage:
			b.WriteString(imageHistoryMarker(part))
		}
	}
	return b.String()
}

func imageHistoryMarker(part protocol.ContentPart) string {
	detail := part.Detail
	if detail == "" {
		detail = "auto"
	}
	return "[image input unsupported by Cursor adapter phase 3: " + detail + "]"
}

func namespacedToolName(namespace, name string) string {
	name = strings.TrimSpace(name)
	namespace = strings.TrimSpace(namespace)
	if namespace == "" || namespace == cursorToolProvider {
		return name
	}
	if name == "" {
		return namespace
	}
	return namespace + "__" + name
}

func toolResultHistoryText(turn RunTurn) string {
	prefix := "[Tool Result]"
	if turn.IsError {
		prefix = "[Tool Error]"
	}
	return prefix + "\n" + toolResultToHistoryText(turn)
}

func toolResultToHistoryText(turn RunTurn) string {
	name := turn.ToolName
	if name == "" {
		name = "tool"
	}
	id := turn.ToolCallID
	if id == "" {
		id = "unknown"
	}
	return strings.Join([]string{
		"[tool_result]",
		"call_id: " + id,
		"name: " + name,
		fmt.Sprintf("is_error: %v", turn.IsError),
		"output:",
		turn.Text,
	}, "\n")
}
