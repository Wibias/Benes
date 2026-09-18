package openaichat

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/tools"
)

// CompileOptions controls only wire capabilities that are not part of the
// canonical request itself. Provider-specific compatibility quirks belong in
// later provider layers, not in this universal compiler.
type CompileOptions struct {
	NativeOpenAI                   bool
	PreserveReasoningContent       bool
	BlankAssistantContentWithTools bool
	ReasoningSplit                 bool
}

// Compile serializes a canonical request into an OpenAI Chat Completions body.
// It intentionally does not consult ParsedRequest.Raw: canonical protocol state
// is the only semantic source of truth at this boundary.
func Compile(req protocol.ParsedRequest, options CompileOptions) ([]byte, error) {
	model := strings.TrimSpace(req.UpstreamModelID)
	if model == "" {
		return nil, fmt.Errorf("upstream model is required")
	}

	messages, err := compileMessages(req.Context, options)
	if err != nil {
		return nil, err
	}
	catalog, err := tools.Build(req.Context.Tools)
	if err != nil {
		return nil, err
	}
	plan, err := catalog.Materialize(tools.MaterializeOptions{Choice: req.Options.ToolChoice, Messages: req.Context.Messages})
	if err != nil {
		return nil, err
	}
	compiledTools := compileTools(plan.Tools, req.Options.ToolChoice)

	body := map[string]any{
		"model":    model,
		"messages": messages,
		"stream":   req.Stream,
	}
	if req.Options.MaxOutputTokens != nil {
		body["max_tokens"] = *req.Options.MaxOutputTokens
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
	if req.Options.Reasoning != "" {
		body["reasoning_effort"] = req.Options.Reasoning
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
	if req.Options.PromptCacheKey != nil {
		body["prompt_cache_key"] = *req.Options.PromptCacheKey
	}
	if len(compiledTools) > 0 {
		body["tools"] = compiledTools
		if choice := compileToolChoice(req.Options.ToolChoice, plan.Tools, options.NativeOpenAI); choice != nil {
			body["tool_choice"] = choice
		}
		if req.Options.ParallelToolCalls != nil {
			body["parallel_tool_calls"] = *req.Options.ParallelToolCalls
		}
	}
	if format := compileTextFormat(req.Options.TextFormat); format != nil {
		body["response_format"] = format
	}
	if req.Stream {
		body["stream_options"] = map[string]any{"include_usage": true}
	}
	if options.ReasoningSplit {
		body["reasoning_split"] = true
	}

	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode OpenAI chat body: %w", err)
	}
	return encoded, nil
}

type pendingToolCall struct {
	id   string
	name string
}

type messageCompiler struct {
	options             CompileOptions
	out                 []any
	pending             []pendingToolCall
	deferredBarriers    []any
	pendingResultImages []any
	seenCallIDs         map[string]struct{}
	mintedIDSeq         int
}

func compileReasoningDetails(parts []protocol.ContentPart) []any {
	var out []any
	for _, part := range parts {
		if part.Type != protocol.ContentThinking || part.ProviderMetadata == nil || part.ProviderMetadata.MiniMax == nil {
			continue
		}
		for _, detail := range part.ProviderMetadata.MiniMax.ReasoningDetails {
			item := map[string]any{}
			if detail.Type != "" {
				item["type"] = detail.Type
			}
			if detail.ID != "" {
				item["id"] = detail.ID
			}
			if detail.Format != "" {
				item["format"] = detail.Format
			}
			if detail.Index != nil {
				item["index"] = *detail.Index
			}
			text := detail.Text
			if text == "" {
				text = part.Thinking
			}
			if text != "" {
				item["text"] = text
			}
			if len(item) == 0 {
				continue
			}
			out = append(out, item)
		}
	}
	return out
}

func hasNonEmptyToolCalls(calls []protocol.ContentPart) bool {
	for _, call := range calls {
		if strings.TrimSpace(call.ToolName) != "" || strings.TrimSpace(call.CustomWireName) != "" || len(call.Arguments) > 0 {
			return true
		}
	}
	return false
}

func compileMessages(context protocol.Context, options CompileOptions) ([]any, error) {
	c := &messageCompiler{options: options, seenCallIDs: make(map[string]struct{})}

	systemParts := append([]string(nil), context.SystemPrompt...)
	if !options.NativeOpenAI {
		for _, message := range context.Messages {
			if message.Role != protocol.RoleDeveloper || hasImage(message.Content) {
				continue
			}
			if text := textContent(message.Content); text != "" {
				systemParts = append(systemParts, text)
			}
		}
	}
	if len(systemParts) > 0 {
		c.out = append(c.out, map[string]any{"role": "system", "content": strings.Join(systemParts, "\n\n")})
	}

	for _, message := range context.Messages {
		switch message.Role {
		case protocol.RoleUser, protocol.RoleDeveloper:
			wire, ok := compileHumanMessage(message, options.NativeOpenAI)
			if !ok {
				continue
			}
			if len(c.pending) > 0 {
				c.deferredBarriers = append(c.deferredBarriers, wire)
			} else {
				c.out = append(c.out, wire)
			}
		case protocol.RoleAssistant:
			c.flushPendingToolCalls()
			wire, pending, err := c.compileAssistant(message)
			if err != nil {
				return nil, err
			}
			if wire != nil {
				c.out = append(c.out, wire)
			}
			c.pending = pending
		case protocol.RoleToolResult:
			c.compileToolResult(message)
		default:
			return nil, fmt.Errorf("unsupported canonical message role %q", message.Role)
		}
	}

	c.flushPendingToolCalls()
	c.releaseDeferredBarriers()
	return c.out, nil
}

func compileHumanMessage(message protocol.Message, nativeOpenAI bool) (map[string]any, bool) {
	images := hasImage(message.Content)
	if message.Role == protocol.RoleDeveloper && !images {
		if !nativeOpenAI {
			return nil, false
		}
		return map[string]any{"role": "developer", "content": textContent(message.Content)}, true
	}

	role := "user"
	if !images {
		return map[string]any{"role": role, "content": textContent(message.Content)}, true
	}
	parts := make([]any, 0, len(message.Content))
	for _, part := range message.Content {
		switch part.Type {
		case protocol.ContentText:
			parts = append(parts, map[string]any{"type": "text", "text": part.Text})
		case protocol.ContentImage:
			if strings.TrimSpace(part.ImageURL) == "" {
				parts = append(parts, map[string]any{"type": "text", "text": "[image]"})
				continue
			}
			image := map[string]any{"url": part.ImageURL}
			if part.Detail != "" {
				image["detail"] = part.Detail
			}
			parts = append(parts, map[string]any{"type": "image_url", "image_url": image})
		}
	}
	return map[string]any{"role": role, "content": parts}, true
}

func (c *messageCompiler) compileAssistant(message protocol.Message) (map[string]any, []pendingToolCall, error) {
	var textParts []string
	var thinkingParts []string
	var calls []protocol.ContentPart
	for _, part := range message.Content {
		switch part.Type {
		case protocol.ContentText:
			textParts = append(textParts, part.Text)
		case protocol.ContentThinking:
			thinkingParts = append(thinkingParts, part.Thinking)
		case protocol.ContentToolCall:
			calls = append(calls, part)
		}
	}
	if len(textParts) == 0 && len(thinkingParts) == 0 && len(calls) == 0 {
		return nil, nil, nil
	}

	wire := map[string]any{"role": "assistant"}
	if len(textParts) > 0 {
		wire["content"] = strings.Join(textParts, "")
	}
	if c.options.PreserveReasoningContent && len(thinkingParts) > 0 {
		wire["reasoning_content"] = strings.Join(thinkingParts, "")
	}
	if c.options.ReasoningSplit {
		if details := compileReasoningDetails(message.Content); len(details) > 0 {
			wire["reasoning_details"] = details
		}
	}

	pending := make([]pendingToolCall, 0, len(calls))
	if len(calls) > 0 {
		wireCalls := make([]any, 0, len(calls))
		for _, call := range calls {
			id := strings.TrimSpace(call.ToolCallID)
			if id == "" {
				id = c.mintCallID()
			} else {
				c.seenCallIDs[id] = struct{}{}
			}
			name := wireToolName(call.ToolNamespace, call.ToolName, call.CustomWireName)
			if strings.TrimSpace(name) == "" {
				return nil, nil, fmt.Errorf("assistant tool call is missing a tool name")
			}
			arguments := call.Arguments
			if arguments == nil {
				arguments = map[string]any{}
			}
			encodedArguments, err := json.Marshal(arguments)
			if err != nil {
				return nil, nil, fmt.Errorf("encode arguments for tool %q: %w", name, err)
			}
			wireCalls = append(wireCalls, map[string]any{
				"id":   id,
				"type": "function",
				"function": map[string]any{
					"name":      name,
					"arguments": string(encodedArguments),
				},
			})
			pending = append(pending, pendingToolCall{id: id, name: name})
		}
		wire["tool_calls"] = wireCalls
		if _, exists := wire["content"]; !exists {
			wire["content"] = ""
		}
		if c.options.BlankAssistantContentWithTools && hasNonEmptyToolCalls(calls) {
			wire["content"] = ""
		}
	}
	if _, hasContent := wire["content"]; !hasContent {
		if _, hasReasoning := wire["reasoning_content"]; hasReasoning {
			wire["content"] = ""
		}
	}
	return wire, pending, nil
}

func (c *messageCompiler) compileToolResult(message protocol.Message) {
	id := strings.TrimSpace(message.ToolCallID)
	match := -1
	for i, call := range c.pending {
		if id != "" && call.id == id {
			match = i
			break
		}
	}

	if match >= 0 {
		c.out = append(c.out, map[string]any{
			"role":         "tool",
			"tool_call_id": id,
			"content":      toolResultText(message.Content),
		})
		c.pendingResultImages = append(c.pendingResultImages, toolResultImages(message.Content)...)
		c.pending = append(c.pending[:match], c.pending[match+1:]...)
		if len(c.pending) == 0 {
			c.flushResultImages()
			c.releaseDeferredBarriers()
		}
		return
	}

	c.flushPendingToolCalls()
	if id == "" {
		id = fmt.Sprintf("call_orphan_%d", len(c.out))
	}
	name := wireToolName(message.ToolNamespace, message.ToolName, "")
	if strings.TrimSpace(name) == "" {
		name = "tool_result"
	}
	name = safeToolName(name)
	c.out = append(c.out, map[string]any{
		"role":    "assistant",
		"content": "",
		"tool_calls": []any{map[string]any{
			"id":   id,
			"type": "function",
			"function": map[string]any{
				"name":      name,
				"arguments": "{}",
			},
		}},
	})
	c.seenCallIDs[id] = struct{}{}
	c.out = append(c.out, map[string]any{
		"role":         "tool",
		"tool_call_id": id,
		"content":      toolResultText(message.Content),
	})
	c.pendingResultImages = append(c.pendingResultImages, toolResultImages(message.Content)...)
	c.flushResultImages()
}

func (c *messageCompiler) flushPendingToolCalls() {
	if len(c.pending) == 0 {
		return
	}
	for _, call := range c.pending {
		c.out = append(c.out, map[string]any{
			"role":         "tool",
			"tool_call_id": call.id,
			"content": fmt.Sprintf(
				"[benes] no tool result was recorded for %q; execution status unknown - do not treat this as success, failure, or user-provided input.",
				call.name,
			),
		})
	}
	c.pending = nil
	c.flushResultImages()
	c.releaseDeferredBarriers()
}

func (c *messageCompiler) releaseDeferredBarriers() {
	if len(c.deferredBarriers) == 0 {
		return
	}
	c.out = append(c.out, c.deferredBarriers...)
	c.deferredBarriers = nil
}

func (c *messageCompiler) flushResultImages() {
	if len(c.pendingResultImages) == 0 {
		return
	}
	parts := []any{map[string]any{"type": "text", "text": "[benes] image output from the preceding tool result(s):"}}
	parts = append(parts, c.pendingResultImages...)
	c.out = append(c.out, map[string]any{"role": "user", "content": parts})
	c.pendingResultImages = nil
}

func (c *messageCompiler) mintCallID() string {
	for {
		c.mintedIDSeq++
		id := fmt.Sprintf("call_benes_minted_%d", c.mintedIDSeq)
		if _, exists := c.seenCallIDs[id]; exists {
			continue
		}
		c.seenCallIDs[id] = struct{}{}
		return id
	}
}

func compileTools(tools []protocol.Tool, choice *protocol.ToolChoice) []any {
	allowed := allowedToolSet(choice)
	out := make([]any, 0, len(tools))
	for _, tool := range tools {
		if allowed != nil && !toolAllowed(tool, allowed) {
			continue
		}
		name := wireToolName(tool.Namespace, tool.Name, "")
		if strings.TrimSpace(name) == "" {
			continue
		}
		parameters := ensureObjectSchema(tool.Parameters)
		function := map[string]any{
			"name":       name,
			"parameters": parameters,
		}
		if tool.Description != "" {
			function["description"] = tool.Description
		}
		if tool.Strict != nil {
			function["strict"] = *tool.Strict
		}
		out = append(out, map[string]any{"type": "function", "function": function})
	}
	return out
}

func compileToolChoice(choice *protocol.ToolChoice, tools []protocol.Tool, nativeOpenAI bool) any {
	if choice == nil {
		return nil
	}
	switch choice.Kind {
	case protocol.ToolChoiceAuto:
		return "auto"
	case protocol.ToolChoiceNone:
		return "none"
	case protocol.ToolChoiceRequired:
		return "required"
	case protocol.ToolChoiceNamed:
		name := resolveToolChoiceWireName(tools, choice.Name)
		if name == "" {
			return nil
		}
		return map[string]any{"type": "function", "function": map[string]any{"name": name}}
	case protocol.ToolChoiceAllowed:
		if choice.AllowedMode == protocol.ToolChoiceModeRequired && len(choice.AllowedTools) == 1 && nativeOpenAI {
			name := resolveToolChoiceWireName(tools, choice.AllowedTools[0])
			if name != "" {
				return map[string]any{"type": "function", "function": map[string]any{"name": name}}
			}
		}
		if choice.AllowedMode == protocol.ToolChoiceModeRequired {
			return "required"
		}
		return "auto"
	default:
		return nil
	}
}

func compileTextFormat(format *protocol.TextFormat) any {
	if format == nil {
		return nil
	}
	switch format.Type {
	case "json_object":
		return map[string]any{"type": "json_object"}
	case "json_schema":
		schema := map[string]any{}
		if format.Name != "" {
			schema["name"] = format.Name
		} else {
			schema["name"] = "response"
		}
		if format.Description != "" {
			schema["description"] = format.Description
		}
		if format.Schema != nil {
			schema["schema"] = format.Schema
		}
		if format.Strict != nil {
			schema["strict"] = *format.Strict
		}
		return map[string]any{"type": "json_schema", "json_schema": schema}
	default:
		return nil
	}
}

func allowedToolSet(choice *protocol.ToolChoice) map[string]struct{} {
	if choice == nil || choice.Kind != protocol.ToolChoiceAllowed {
		return nil
	}
	allowed := make(map[string]struct{}, len(choice.AllowedTools))
	for _, name := range choice.AllowedTools {
		allowed[name] = struct{}{}
	}
	return allowed
}

func toolAllowed(tool protocol.Tool, allowed map[string]struct{}) bool {
	candidates := []string{tool.Name, wireToolName(tool.Namespace, tool.Name, "")}
	if tool.Namespace != "" {
		candidates = append(candidates, tool.Namespace+"."+tool.Name)
	}
	for _, candidate := range candidates {
		if _, ok := allowed[candidate]; ok {
			return true
		}
	}
	return false
}

func resolveToolChoiceWireName(tools []protocol.Tool, requested string) string {
	for _, tool := range tools {
		if toolAllowed(tool, map[string]struct{}{requested: {}}) {
			return wireToolName(tool.Namespace, tool.Name, "")
		}
	}
	if strings.Contains(requested, ".") {
		parts := strings.SplitN(requested, ".", 2)
		return wireToolName(parts[0], parts[1], "")
	}
	return safeToolName(requested)
}

func wireToolName(namespace, name, custom string) string {
	if strings.TrimSpace(custom) != "" {
		return safeToolName(custom)
	}
	name = safeToolName(name)
	if strings.TrimSpace(namespace) == "" {
		return name
	}
	return safeToolName(namespace) + "__" + name
}

func safeToolName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	var builder strings.Builder
	builder.Grow(len(name))
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			builder.WriteRune(r)
		default:
			builder.WriteByte('_')
		}
	}
	return builder.String()
}

func ensureObjectSchema(parameters map[string]any) map[string]any {
	if parameters == nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	out := make(map[string]any, len(parameters)+1)
	for key, value := range parameters {
		out[key] = value
	}
	if out["type"] != "object" {
		out["type"] = "object"
	}
	return out
}

func hasImage(parts []protocol.ContentPart) bool {
	for _, part := range parts {
		if part.Type == protocol.ContentImage {
			return true
		}
	}
	return false
}

func textContent(parts []protocol.ContentPart) string {
	var builder strings.Builder
	for _, part := range parts {
		if part.Type == protocol.ContentText {
			builder.WriteString(part.Text)
		}
	}
	return builder.String()
}

func toolResultText(parts []protocol.ContentPart) string {
	var builder strings.Builder
	for _, part := range parts {
		switch part.Type {
		case protocol.ContentText:
			builder.WriteString(part.Text)
		case protocol.ContentImage:
			if strings.TrimSpace(part.ImageURL) == "" {
				builder.WriteString("[image]")
			}
		}
	}
	return builder.String()
}

func toolResultImages(parts []protocol.ContentPart) []any {
	var out []any
	for _, part := range parts {
		if part.Type != protocol.ContentImage || strings.TrimSpace(part.ImageURL) == "" {
			continue
		}
		image := map[string]any{"url": part.ImageURL}
		if part.Detail != "" {
			image["detail"] = part.Detail
		}
		out = append(out, map[string]any{"type": "image_url", "image_url": image})
	}
	return out
}
