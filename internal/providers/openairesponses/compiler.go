package openairesponses

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/responses/websearchcall"
	"github.com/Wibias/Benes/internal/tools"
)

// Compile serializes the canonical request into the currently migrated native
// OpenAI Responses request subset. ParsedRequest.Raw is intentionally not read.
func Compile(req protocol.ParsedRequest) ([]byte, error) {
	functionTools := make([]protocol.Tool, 0, len(req.Context.Tools))
	for _, tool := range req.Context.Tools {
		if tool.HostedWebSearch {
			continue
		}
		functionTools = append(functionTools, tool)
	}
	catalog, err := tools.Build(functionTools)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnsupportedRequestShape, err)
	}
	plan, err := catalog.Materialize(tools.MaterializeOptions{Choice: req.Options.ToolChoice, Messages: req.Context.Messages})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnsupportedRequestShape, err)
	}
	catalog, err = tools.Build(plan.Tools)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnsupportedRequestShape, err)
	}
	if err := validateCanonicalRequest(req, catalog); err != nil {
		return nil, err
	}

	input, err := compileResponseInput(req.Context, catalog)
	if err != nil {
		return nil, err
	}
	body := map[string]any{
		"model":               strings.TrimSpace(req.UpstreamModelID),
		"input":               input,
		"stream":              req.Stream,
		"parallel_tool_calls": false,
	}
	if req.Options.Store != nil {
		body["store"] = *req.Options.Store
	}
	if strings.TrimSpace(req.PreviousResponseID) != "" {
		body["previous_response_id"] = strings.TrimSpace(req.PreviousResponseID)
	}
	if len(req.Context.SystemPrompt) > 0 {
		body["instructions"] = strings.Join(req.Context.SystemPrompt, "\n\n")
	}
	compiledTools := compileResponseTools(catalog, req.HostedWebSearchTools)
	if len(compiledTools) > 0 {
		body["tools"] = compiledTools
	}
	if req.Options.ToolChoice != nil {
		choice, err := compileResponseToolChoice(req.Options.ToolChoice, catalog, req.HostedWebSearchTools)
		if err != nil {
			return nil, err
		}
		if choice != nil {
			body["tool_choice"] = choice
		}
	}
	if req.Options.MaxOutputTokens != nil {
		body["max_output_tokens"] = *req.Options.MaxOutputTokens
	}
	if req.Options.Temperature != nil {
		body["temperature"] = *req.Options.Temperature
	}
	if req.Options.TopP != nil {
		body["top_p"] = *req.Options.TopP
	}
	if req.Options.StopSequences != nil {
		body["stop"] = append([]string(nil), req.Options.StopSequences...)
	}
	if req.Options.PromptCacheKey != nil {
		body["prompt_cache_key"] = *req.Options.PromptCacheKey
	}
	if req.Options.ServiceTier != nil {
		body["service_tier"] = *req.Options.ServiceTier
	}
	if req.Options.PresencePenalty != nil {
		body["presence_penalty"] = *req.Options.PresencePenalty
	}
	if req.Options.FrequencyPenalty != nil {
		body["frequency_penalty"] = *req.Options.FrequencyPenalty
	}
	if req.Options.User != nil {
		body["user"] = *req.Options.User
	}
	if req.Options.Metadata != nil {
		body["metadata"] = cloneMap(req.Options.Metadata)
	}
	if reasoning := compileResponseReasoning(req.Options); reasoning != nil {
		body["reasoning"] = reasoning
	}
	if text := compileResponseText(req.Options.TextFormat); text != nil {
		body["text"] = map[string]any{"format": text}
	}

	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode OpenAI Responses body: %w", err)
	}
	return encoded, nil
}

func validateCanonicalRequest(req protocol.ParsedRequest, catalog *tools.Catalog) error {
	if strings.TrimSpace(req.UpstreamModelID) == "" {
		return fmt.Errorf("%w: upstream model is required", ErrUnsupportedRequestShape)
	}

	if req.Options.ParallelToolCalls != nil && *req.Options.ParallelToolCalls {
		return fmt.Errorf("%w: parallel_tool_calls=true", ErrUnsupportedRequestShape)
	}
	for mi, message := range req.Context.Messages {
		if message.Role == protocol.RoleToolResult && strings.TrimSpace(message.ToolCallID) == "" {
			return fmt.Errorf("%w: messages[%d] tool result is missing call_id", ErrUnsupportedRequestShape, mi)
		}
		if message.KiroRedactedReasoning != "" || message.ContainsEncryptedContent {
			return fmt.Errorf("%w: messages[%d] replay state", ErrUnsupportedRequestShape, mi)
		}
		for pi, part := range message.Content {
			switch part.Type {
			case protocol.ContentText, protocol.ContentImage:
			case protocol.ContentThinking:
				return fmt.Errorf("%w: messages[%d].content[%d] reasoning replay", ErrUnsupportedRequestShape, mi, pi)
			case protocol.ContentToolCall:
				if strings.TrimSpace(part.ToolCallID) == "" || strings.TrimSpace(part.ToolName) == "" {
					return fmt.Errorf("%w: messages[%d].content[%d] function call", ErrUnsupportedRequestShape, mi, pi)
				}
				if websearchcall.IsHosted(part) {
					continue
				}
				if _, err := catalog.WireForCall(part); err != nil {
					return fmt.Errorf("%w: messages[%d].content[%d] %v", ErrUnsupportedRequestShape, mi, pi, err)
				}
			default:
				return fmt.Errorf("%w: messages[%d].content[%d]", ErrUnsupportedRequestShape, mi, pi)
			}
		}
	}
	return nil
}

func compileResponseInput(ctx protocol.Context, catalog *tools.Catalog) ([]any, error) {
	out := make([]any, 0, len(ctx.Messages)*2)
	for _, message := range ctx.Messages {
		switch message.Role {
		case protocol.RoleUser, protocol.RoleDeveloper:
			content, err := compileInputContent(message.Content)
			if err != nil {
				return nil, err
			}
			out = append(out, map[string]any{
				"type":    "message",
				"role":    string(message.Role),
				"content": content,
			})
		case protocol.RoleAssistant:
			textContent, calls, err := splitAssistantContent(message.Content)
			if err != nil {
				return nil, err
			}
			if len(textContent) > 0 {
				item := map[string]any{"type": "message", "role": "assistant", "content": textContent}
				if message.Phase != nil {
					item["phase"] = string(*message.Phase)
				}
				out = append(out, item)
			}
			for _, call := range calls {
				if websearchcall.IsHosted(call) {
					out = append(out, map[string]any{
						"type":   websearchcall.ItemType,
						"id":     call.ToolCallID,
						"status": "completed",
						"action": websearchcall.Action(websearchcall.QueriesFromArguments(call.Arguments)),
					})
					continue
				}
				arguments := call.Arguments
				if arguments == nil {
					arguments = map[string]any{}
				}
				encodedArguments, err := json.Marshal(arguments)
				if err != nil {
					return nil, fmt.Errorf("encode arguments for function %q: %w", call.ToolName, err)
				}
				wireName, err := catalog.WireForCall(call)
				if err != nil {
					return nil, fmt.Errorf("%w: %v", ErrUnsupportedRequestShape, err)
				}
				out = append(out, map[string]any{
					"type":      "function_call",
					"call_id":   call.ToolCallID,
					"name":      wireName,
					"arguments": string(encodedArguments),
				})
			}
		case protocol.RoleToolResult:
			output, err := compileToolResultOutput(message.Content)
			if err != nil {
				return nil, err
			}
			out = append(out, map[string]any{
				"type":    "function_call_output",
				"call_id": message.ToolCallID,
				"output":  output,
			})
		default:
			return nil, fmt.Errorf("%w: unsupported message role %q", ErrUnsupportedRequestShape, message.Role)
		}
	}
	return out, nil
}

func compileInputContent(parts []protocol.ContentPart) ([]any, error) {
	out := make([]any, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case protocol.ContentText:
			out = append(out, map[string]any{"type": "input_text", "text": part.Text})
		case protocol.ContentImage:
			if strings.TrimSpace(part.ImageURL) == "" {
				return nil, fmt.Errorf("%w: input image is missing image_url", ErrUnsupportedRequestShape)
			}
			image := map[string]any{"type": "input_image", "image_url": part.ImageURL}
			if part.Detail != "" {
				image["detail"] = part.Detail
			}
			out = append(out, image)
		default:
			return nil, fmt.Errorf("%w: unsupported human content %q", ErrUnsupportedRequestShape, part.Type)
		}
	}
	return out, nil
}

func splitAssistantContent(parts []protocol.ContentPart) ([]any, []protocol.ContentPart, error) {
	text := make([]any, 0, len(parts))
	calls := make([]protocol.ContentPart, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case protocol.ContentText:
			text = append(text, map[string]any{"type": "output_text", "text": part.Text})
		case protocol.ContentToolCall:
			calls = append(calls, part)
		case protocol.ContentImage, protocol.ContentThinking:
			return nil, nil, fmt.Errorf("%w: unsupported assistant content %q", ErrUnsupportedRequestShape, part.Type)
		default:
			return nil, nil, fmt.Errorf("%w: unsupported assistant content %q", ErrUnsupportedRequestShape, part.Type)
		}
	}
	return text, calls, nil
}

func compileToolResultOutput(parts []protocol.ContentPart) (any, error) {
	hasImage := false
	for _, part := range parts {
		if part.Type == protocol.ContentImage {
			hasImage = true
			break
		}
	}
	if !hasImage {
		var b strings.Builder
		for _, part := range parts {
			if part.Type != protocol.ContentText {
				return nil, fmt.Errorf("%w: unsupported tool output content %q", ErrUnsupportedRequestShape, part.Type)
			}
			b.WriteString(part.Text)
		}
		return b.String(), nil
	}
	out := make([]any, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case protocol.ContentText:
			out = append(out, map[string]any{"type": "input_text", "text": part.Text})
		case protocol.ContentImage:
			if strings.TrimSpace(part.ImageURL) == "" {
				return nil, fmt.Errorf("%w: tool output image is missing image_url", ErrUnsupportedRequestShape)
			}
			image := map[string]any{"type": "input_image", "image_url": part.ImageURL}
			if part.Detail != "" {
				image["detail"] = part.Detail
			}
			out = append(out, image)
		default:
			return nil, fmt.Errorf("%w: unsupported tool output content %q", ErrUnsupportedRequestShape, part.Type)
		}
	}
	return out, nil
}

func compileResponseTools(catalog *tools.Catalog, hosted []protocol.HostedWebSearchTool) []any {
	entries := catalog.Entries()
	out := make([]any, 0, len(entries)+len(hosted))
	for _, entry := range entries {
		tool := entry.Tool
		parameters := normalizeObjectSchema(tool.Parameters)
		wire := map[string]any{
			"type":       "function",
			"name":       entry.WireName,
			"parameters": parameters,
		}
		if tool.Description != "" {
			wire["description"] = tool.Description
		}
		if tool.Strict != nil {
			wire["strict"] = *tool.Strict
		}
		out = append(out, wire)
	}
	for _, tool := range hosted {
		out = append(out, compileHostedWebSearch(tool))
	}
	return out
}

func compileHostedWebSearch(tool protocol.HostedWebSearchTool) map[string]any {
	wire := map[string]any{"type": string(tool.Type)}
	if tool.SearchContextSize != "" {
		wire["search_context_size"] = tool.SearchContextSize
	}
	if tool.SearchContentTypes != nil {
		wire["search_content_types"] = append([]string(nil), tool.SearchContentTypes...)
	}
	if tool.IndexedWebAccess != nil {
		wire["indexed_web_access"] = *tool.IndexedWebAccess
	}
	if tool.ExternalWebAccess != nil {
		wire["external_web_access"] = *tool.ExternalWebAccess
	}
	if tool.UserLocation != nil {
		location := map[string]any{"type": tool.UserLocation.Type}
		if tool.UserLocation.Country != "" {
			location["country"] = tool.UserLocation.Country
		}
		if tool.UserLocation.City != "" {
			location["city"] = tool.UserLocation.City
		}
		if tool.UserLocation.Region != "" {
			location["region"] = tool.UserLocation.Region
		}
		if tool.UserLocation.Timezone != "" {
			location["timezone"] = tool.UserLocation.Timezone
		}
		wire["user_location"] = location
	}
	if tool.Filters != nil {
		filters := map[string]any{}
		if tool.Filters.AllowedDomains != nil {
			filters["allowed_domains"] = append([]string(nil), tool.Filters.AllowedDomains...)
		}
		wire["filters"] = filters
	}
	return wire
}

func normalizeObjectSchema(parameters map[string]any) map[string]any {
	out := make(map[string]any, len(parameters)+1)
	for key, value := range parameters {
		out[key] = value
	}
	if typ, ok := out["type"].(string); !ok || typ != "object" {
		out["type"] = "object"
	}
	return out
}

func compileResponseToolChoice(choice *protocol.ToolChoice, catalog *tools.Catalog, hosted []protocol.HostedWebSearchTool) (any, error) {
	if choice == nil {
		return nil, nil
	}
	hostedTypes := make(map[string]struct{}, len(hosted))
	for _, tool := range hosted {
		hostedTypes[string(tool.Type)] = struct{}{}
	}
	switch choice.Kind {
	case protocol.ToolChoiceAuto:
		return "auto", nil
	case protocol.ToolChoiceNone:
		return "none", nil
	case protocol.ToolChoiceRequired:
		return "required", nil
	case protocol.ToolChoiceNamed:
		if _, ok := hostedTypes[choice.Name]; ok {
			return map[string]any{"type": choice.Name}, nil
		}
		name, err := catalog.ResolveChoice(choice.Name)
		if err != nil {
			return nil, fmt.Errorf("%w: named tool choice %q is not in the canonical tool catalog", ErrUnsupportedRequestShape, choice.Name)
		}
		return map[string]any{"type": "function", "name": name}, nil
	case protocol.ToolChoiceAllowed:
		allowed := make([]any, 0, len(choice.AllowedTools))
		seen := make(map[string]struct{}, len(choice.AllowedTools))
		for _, name := range choice.AllowedTools {
			if _, ok := hostedTypes[name]; ok {
				if _, exists := seen[name]; exists {
					continue
				}
				seen[name] = struct{}{}
				allowed = append(allowed, map[string]any{"type": name})
				continue
			}
			wire, err := catalog.ResolveChoice(name)
			if err != nil {
				continue
			}
			key := "function:" + wire
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			allowed = append(allowed, map[string]any{"type": "function", "name": wire})
		}
		if len(allowed) == 0 {
			return "none", nil
		}
		mode := string(choice.AllowedMode)
		if mode != "required" {
			mode = "auto"
		}
		return map[string]any{"type": "allowed_tools", "mode": mode, "tools": allowed}, nil
	default:
		return nil, fmt.Errorf("%w: unsupported tool choice kind %q", ErrUnsupportedRequestShape, choice.Kind)
	}
}

func compileResponseReasoning(options protocol.RequestOptions) map[string]any {
	if options.Reasoning == "" && options.HideThinkingSummary {
		return nil
	}
	out := map[string]any{}
	if options.Reasoning != "" {
		effort := options.Reasoning
		if effort == "ultra" {
			effort = "max"
		}
		out["effort"] = effort
	}
	if options.HideThinkingSummary {
		out["summary"] = "none"
	} else {
		out["summary"] = "auto"
	}
	return out
}

func compileResponseText(format *protocol.TextFormat) map[string]any {
	if format == nil {
		return nil
	}
	switch format.Type {
	case "json_object":
		return map[string]any{"type": "json_object"}
	case "json_schema":
		out := map[string]any{"type": "json_schema", "name": format.Name}
		if format.Description != "" {
			out["description"] = format.Description
		}
		if format.Schema != nil {
			out["schema"] = format.Schema
		}
		if format.Strict != nil {
			out["strict"] = *format.Strict
		}
		return out
	default:
		return nil
	}
}

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
