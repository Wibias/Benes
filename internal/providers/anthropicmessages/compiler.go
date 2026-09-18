package anthropicmessages

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strings"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/tools"
)

const DefaultMaxOutputTokens = 8192

var anthropicEfforts = map[string]struct{}{
	"low":    {},
	"medium": {},
	"high":   {},
	"xhigh":  {},
	"max":    {},
}

// Compile serializes canonical Benes state into an Anthropic Messages request.
// ParsedRequest.Raw is intentionally never consulted at this boundary.
func Compile(req protocol.ParsedRequest) ([]byte, error) {
	model := strings.TrimSpace(req.UpstreamModelID)
	if model == "" {
		return nil, fmt.Errorf("upstream model is required")
	}

	catalog, err := tools.Build(req.Context.Tools)
	if err != nil {
		return nil, err
	}
	plan, err := catalog.Materialize(tools.MaterializeOptions{
		Choice:   req.Options.ToolChoice,
		Messages: req.Context.Messages,
	})
	if err != nil {
		return nil, err
	}
	wireTools, err := compileTools(plan.Tools)
	if err != nil {
		return nil, err
	}
	messages, system, err := compileMessages(req.Context, catalog)
	if err != nil {
		return nil, err
	}
	if len(messages) == 0 {
		return nil, fmt.Errorf("anthropic messages request requires at least one message")
	}

	maxTokens, err := compileMaxTokens(req.Options.MaxOutputTokens)
	if err != nil {
		return nil, err
	}
	body := map[string]any{
		"model":      model,
		"messages":   messages,
		"max_tokens": maxTokens,
		"stream":     req.Stream,
	}
	if system != "" {
		body["system"] = system
	}
	if req.Options.Temperature != nil {
		body["temperature"] = *req.Options.Temperature
	}
	if req.Options.TopP != nil {
		body["top_p"] = *req.Options.TopP
	}
	if req.Options.StopSequences != nil {
		body["stop_sequences"] = append([]string(nil), req.Options.StopSequences...)
	}
	if len(wireTools) > 0 {
		body["tools"] = wireTools
		choice, err := compileToolChoice(catalog, req.Options.ToolChoice, req.Options.ParallelToolCalls)
		if err != nil {
			return nil, err
		}
		if choice != nil {
			body["tool_choice"] = choice
		}
	} else if req.Options.ToolChoice != nil || req.Options.ParallelToolCalls != nil {
		return nil, fmt.Errorf("tool choice requires at least one materialized tool")
	}
	if output := compileOutputConfig(req.Options); len(output) > 0 {
		body["output_config"] = output
	}

	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode Anthropic Messages body: %w", err)
	}
	return encoded, nil
}

func compileMaxTokens(value *float64) (int64, error) {
	if value == nil {
		return DefaultMaxOutputTokens, nil
	}
	if *value < 0 || math.Trunc(*value) != *value || *value > math.MaxInt64 {
		return 0, fmt.Errorf("max output tokens must be a non-negative integer")
	}
	return int64(*value), nil
}

func compileTools(declared []protocol.Tool) ([]any, error) {
	out := make([]any, 0, len(declared))
	for _, tool := range declared {
		if tool.HostedWebSearch || tool.Freeform || tool.ToolSearch {
			return nil, fmt.Errorf("tool %q is not representable by the Anthropic Messages function-tool contract", tool.Name)
		}
		name := tools.WireName(tool)
		if name == "" {
			return nil, fmt.Errorf("Anthropic Messages tool has a blank wire name")
		}
		schema := tool.Parameters
		if schema == nil {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		row := map[string]any{
			"name":         name,
			"input_schema": schema,
		}
		if tool.Description != "" {
			row["description"] = tool.Description
		}
		if tool.Strict != nil {
			row["strict"] = *tool.Strict
		}
		out = append(out, row)
	}
	return out, nil
}

func compileToolChoice(catalog *tools.Catalog, choice *protocol.ToolChoice, parallel *bool) (map[string]any, error) {
	if choice == nil && parallel == nil {
		return nil, nil
	}
	out := map[string]any{}
	if choice == nil {
		out["type"] = "auto"
	} else {
		switch choice.Kind {
		case protocol.ToolChoiceAuto:
			out["type"] = "auto"
		case protocol.ToolChoiceRequired:
			out["type"] = "any"
		case protocol.ToolChoiceNone:
			out["type"] = "none"
		case protocol.ToolChoiceNamed:
			name, err := catalog.ResolveChoice(choice.Name)
			if err != nil {
				return nil, err
			}
			out["type"] = "tool"
			out["name"] = name
		case protocol.ToolChoiceAllowed:
			return nil, fmt.Errorf("allowed-tool choice is not representable on Anthropic Messages")
		default:
			return nil, fmt.Errorf("unsupported Anthropic Messages tool choice %q", choice.Kind)
		}
	}
	if parallel != nil && out["type"] != "none" {
		out["disable_parallel_tool_use"] = !*parallel
	}
	return out, nil
}

func compileMessages(ctx protocol.Context, catalog *tools.Catalog) ([]any, string, error) {
	system := append([]string(nil), ctx.SystemPrompt...)
	messages := make([]any, 0, len(ctx.Messages))
	for _, message := range ctx.Messages {
		switch message.Role {
		case protocol.RoleDeveloper:
			text, err := developerText(message)
			if err != nil {
				return nil, "", err
			}
			if text != "" {
				system = append(system, text)
			}
		case protocol.RoleUser:
			content, err := compileContent(message.Content, catalog, false)
			if err != nil {
				return nil, "", err
			}
			if len(content) > 0 {
				messages = append(messages, map[string]any{"role": "user", "content": content})
			}
		case protocol.RoleAssistant:
			content, err := compileContent(message.Content, catalog, true)
			if err != nil {
				return nil, "", err
			}
			if len(content) > 0 {
				messages = append(messages, map[string]any{"role": "assistant", "content": content})
			}
		case protocol.RoleToolResult:
			wire, err := compileToolResult(message)
			if err != nil {
				return nil, "", err
			}
			messages = append(messages, map[string]any{"role": "user", "content": []any{wire}})
		default:
			return nil, "", fmt.Errorf("unsupported canonical message role %q", message.Role)
		}
	}
	return messages, strings.Join(nonEmptyStrings(system), "\n\n"), nil
}

func developerText(message protocol.Message) (string, error) {
	parts := make([]string, 0, len(message.Content))
	for _, part := range message.Content {
		if part.Type != protocol.ContentText {
			return "", fmt.Errorf("non-text developer content is not representable on Anthropic Messages")
		}
		parts = append(parts, part.Text)
	}
	return strings.Join(parts, ""), nil
}

func compileContent(parts []protocol.ContentPart, catalog *tools.Catalog, assistant bool) ([]any, error) {
	out := make([]any, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case protocol.ContentText:
			out = append(out, map[string]any{"type": "text", "text": part.Text})
		case protocol.ContentImage:
			if assistant {
				return nil, fmt.Errorf("assistant image content is not representable on Anthropic Messages")
			}
			image, err := compileImage(part.ImageURL)
			if err != nil {
				return nil, err
			}
			out = append(out, image)
		case protocol.ContentThinking:
			if !assistant {
				return nil, fmt.Errorf("user thinking content is not representable on Anthropic Messages")
			}
			if strings.TrimSpace(part.Signature) == "" {
				return nil, fmt.Errorf("assistant thinking block requires an Anthropic signature")
			}
			out = append(out, map[string]any{"type": "thinking", "thinking": part.Thinking, "signature": part.Signature})
		case protocol.ContentToolCall:
			if !assistant {
				return nil, fmt.Errorf("user tool call is not representable on Anthropic Messages")
			}
			if strings.TrimSpace(part.ToolCallID) == "" {
				return nil, fmt.Errorf("assistant tool call requires an id")
			}
			name, err := catalog.WireForCall(part)
			if err != nil {
				return nil, err
			}
			input := part.Arguments
			if input == nil {
				input = map[string]any{}
			}
			out = append(out, map[string]any{"type": "tool_use", "id": part.ToolCallID, "name": name, "input": input})
		default:
			return nil, fmt.Errorf("unsupported canonical content type %q", part.Type)
		}
	}
	return out, nil
}

func compileToolResult(message protocol.Message) (map[string]any, error) {
	id := strings.TrimSpace(message.ToolCallID)
	if id == "" {
		return nil, fmt.Errorf("tool result requires a tool call id")
	}
	content, err := compileToolResultContent(message.Content)
	if err != nil {
		return nil, err
	}
	wire := map[string]any{"type": "tool_result", "tool_use_id": id, "content": content}
	if message.IsError {
		wire["is_error"] = true
	}
	return wire, nil
}

func compileToolResultContent(parts []protocol.ContentPart) (any, error) {
	if len(parts) == 0 {
		return "", nil
	}
	if len(parts) == 1 && parts[0].Type == protocol.ContentText {
		return parts[0].Text, nil
	}
	out := make([]any, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case protocol.ContentText:
			out = append(out, map[string]any{"type": "text", "text": part.Text})
		case protocol.ContentImage:
			image, err := compileImage(part.ImageURL)
			if err != nil {
				return nil, err
			}
			out = append(out, image)
		default:
			return nil, fmt.Errorf("unsupported tool result content type %q", part.Type)
		}
	}
	return out, nil
}

func compileImage(raw string) (map[string]any, error) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "data:") {
		header, data, ok := strings.Cut(strings.TrimPrefix(raw, "data:"), ",")
		if !ok || !strings.HasSuffix(header, ";base64") {
			return nil, fmt.Errorf("invalid data image source")
		}
		mediaType := strings.TrimSuffix(header, ";base64")
		switch mediaType {
		case "image/jpeg", "image/png", "image/gif", "image/webp":
		default:
			return nil, fmt.Errorf("unsupported Anthropic base64 image media type")
		}
		if data == "" {
			return nil, fmt.Errorf("invalid data image source")
		}
		if _, err := base64.StdEncoding.DecodeString(data); err != nil {
			return nil, fmt.Errorf("invalid base64 image source")
		}
		return map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": mediaType, "data": data}}, nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("unsupported Anthropic image source")
	}
	return map[string]any{"type": "image", "source": map[string]any{"type": "url", "url": raw}}, nil
}

func compileOutputConfig(options protocol.RequestOptions) map[string]any {
	out := map[string]any{}
	if effort := strings.TrimSpace(options.Reasoning); effort != "" && effort != "none" {
		if _, ok := anthropicEfforts[effort]; ok {
			out["effort"] = effort
		}
	}
	if format := options.TextFormat; format != nil && format.Type == "json_schema" && format.Schema != nil {
		out["format"] = map[string]any{"type": "json_schema", "schema": format.Schema}
	}
	return out
}

func nonEmptyStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, value)
		}
	}
	return out
}
