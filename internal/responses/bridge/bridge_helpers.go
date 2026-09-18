package bridge

import (
	"encoding/json"
	"strings"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/responses/websearchcall"
	"github.com/Wibias/Benes/internal/usage"
)

func (b *Bridge) closeWebSearch(status string, queries []string, sources []protocol.URLCitation) []Frame {
	if b.webSearch == nil {
		return nil
	}
	search := b.webSearch
	item := map[string]any{
		"type": "web_search_call", "id": search.id, "status": status,
		"action": websearchcall.Action(queries),
	}
	if len(sources) > 0 {
		item["sources"] = append([]protocol.URLCitation(nil), sources...)
	}
	frame := b.emit("response.output_item.done", map[string]any{"output_index": search.outputIndex, "item": item})
	b.output = append(b.output, item)
	b.outputIndex++
	b.webSearch = nil
	return []Frame{frame}
}

func (b *Bridge) addPendingSources(sources []protocol.URLCitation) {
	if len(sources) == 0 {
		return
	}
	seen := make(map[string]struct{}, len(b.pendingSources)+len(sources))
	for _, source := range b.pendingSources {
		seen[source.URL] = struct{}{}
	}
	for _, source := range sources {
		if _, exists := seen[source.URL]; exists {
			continue
		}
		seen[source.URL] = struct{}{}
		b.pendingSources = append(b.pendingSources, source)
	}
}

func (b *Bridge) takeWebAnnotations() []any {
	if len(b.pendingSources) == 0 {
		return []any{}
	}
	annotations := make([]any, 0, len(b.pendingSources))
	for _, source := range b.pendingSources {
		annotation := map[string]any{
			"type": "url_citation", "url": source.URL, "start_index": 0, "end_index": 0,
		}
		if source.Title != "" {
			annotation["title"] = source.Title
		}
		annotations = append(annotations, annotation)
	}
	b.pendingSources = nil
	return annotations
}

func (b *Bridge) fail(message string, eventUsage *protocol.Usage) []Frame {
	failure := map[string]any{"type": "upstream_error", "message": message}
	response := b.snapshot("failed", nil, nil)
	if eventUsage != nil {
		response["usage"] = responseUsage(b.model, b.requestedTier, eventUsage)

	}
	response["error"] = failure
	response["last_error"] = failure
	frame := b.emit("response.failed", map[string]any{"response": response})
	b.terminated = true
	return []Frame{frame}
}

func (b *Bridge) snapshot(status string, responseUsage map[string]any, endTurn *bool) map[string]any {
	response := map[string]any{
		"id": b.responseID, "object": "response", "created_at": b.createdAt,
		"status": status, "model": b.model, "output": append([]map[string]any(nil), b.output...),
		"usage":               responseUsage,
		"parallel_tool_calls": b.parallelToolCalls,
		"tool_choice":         b.toolChoice,
		"tools":               append([]any(nil), b.snapshotTools...),
	}
	if endTurn != nil {
		response["end_turn"] = *endTurn
	}
	return response
}

func (b *Bridge) emit(name string, fields map[string]any) Frame {
	data := make(map[string]any)
	data["type"] = name
	data["sequence_number"] = b.sequence
	b.sequence++
	for key, value := range fields {
		data[key] = value
	}
	return Frame{Name: name, Data: data}
}

func responseUsage(model, requestedTier string, value *protocol.Usage) map[string]any {
	if value == nil {
		return attachXAIUsage(model, "", requestedTier, 0, map[string]any{
			"input_tokens":          0,
			"output_tokens":         0,
			"total_tokens":          0,
			"input_tokens_details":  map[string]any{"cached_tokens": 0},
			"output_tokens_details": map[string]any{"reasoning_tokens": 0},
		})
	}
	inputTokens := value.InputTokens
	if value.ContextTotalTokens > 0 {
		inputTokens = value.ContextTotalTokens - value.OutputTokens
		if inputTokens < 0 {
			inputTokens = 0
		}
	}
	total := value.TotalTokens
	if value.ContextTotalTokens > 0 {
		total = value.ContextTotalTokens
	} else if total == 0 {
		total = inputTokens + value.OutputTokens
	}
	cached := value.CachedInputTokens
	if cached > inputTokens {
		cached = inputTokens
	}
	inputDetails := map[string]any{"cached_tokens": cached}
	if value.CacheCreationInputTokens > 0 {
		remaining := inputTokens - cached
		if remaining < 0 {
			remaining = 0
		}
		cacheWrite := value.CacheCreationInputTokens
		if cacheWrite > remaining {
			cacheWrite = remaining
		}
		inputDetails["cache_write_tokens"] = cacheWrite
	}
	return attachXAIUsage(model, value.ServiceTier, requestedTier, inputTokens, map[string]any{
		"input_tokens": inputTokens, "output_tokens": value.OutputTokens, "total_tokens": total,
		"input_tokens_details":  inputDetails,
		"output_tokens_details": map[string]any{"reasoning_tokens": value.ReasoningOutputTokens},
	})
}

func attachXAIUsage(model, confirmedTier, requestedTier string, inputTokens int64, payload map[string]any) map[string]any {
	if estimate, ok := usage.ForRoutedModel(model, confirmedTier, requestedTier, inputTokens, usage.Cost4{}, 0); ok {
		payload["xai_priority_applied"] = estimate.PriorityApplied
		payload["xai_lower_bound"] = estimate.LowerBound
	}
	return payload
}

func validArguments(value string) bool {
	var decoded any
	return json.Unmarshal([]byte(value), &decoded) == nil
}

func samePhase(left, right *protocol.MessagePhase) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func snapshotParallelToolCalls(value *bool) bool {
	if value == nil {
		return true
	}
	return *value
}

func snapshotToolChoice(choice *protocol.ToolChoice) any {
	if choice == nil {
		return "auto"
	}
	switch choice.Kind {
	case protocol.ToolChoiceNone:
		return "none"
	case protocol.ToolChoiceRequired:
		return "required"
	case protocol.ToolChoiceNamed:
		name := strings.TrimSpace(choice.Name)
		if name == "" {
			return "auto"
		}
		return map[string]any{"type": "function", "name": name}
	case protocol.ToolChoiceAllowed:
		mode := string(choice.AllowedMode)
		if mode == "" {
			mode = "auto"
		}
		tools := make([]any, 0, len(choice.AllowedTools))
		for _, name := range choice.AllowedTools {
			tools = append(tools, map[string]any{"type": "function", "name": name})
		}
		return map[string]any{"type": "allowed_tools", "mode": mode, "tools": tools}
	default:
		return "auto"
	}
}

func snapshotTools(tools []protocol.Tool) []any {
	out := make([]any, 0, len(tools))
	for _, tool := range tools {
		name := strings.TrimSpace(tool.Name)
		if name == "" || tool.HostedWebSearch || tool.ToolSearch {
			continue
		}
		if tool.Namespace != "" {
			name = tool.Namespace + "__" + name
		}
		entry := map[string]any{"type": "function", "name": name}
		if tool.Description != "" {
			entry["description"] = tool.Description
		}
		if tool.Parameters == nil {
			entry["parameters"] = map[string]any{"type": "object"}
		} else {
			entry["parameters"] = tool.Parameters
		}
		if tool.Strict != nil {
			entry["strict"] = *tool.Strict
		}
		out = append(out, entry)
	}
	return out
}

func clonePhase(value *protocol.MessagePhase) *protocol.MessagePhase {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
