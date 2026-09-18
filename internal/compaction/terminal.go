package compaction

import (
	"strings"

	"github.com/Wibias/Benes/internal/protocol"
)

type Class string

const (
	ClassCompleted  Class = "completed"
	ClassIncomplete Class = "incomplete"
	ClassFailed     Class = "failed"
)

func ClassifyEvent(event protocol.Event, openTool bool) Class {
	switch event.Type {
	case protocol.EventError:
		return ClassFailed
	case protocol.EventIncomplete:
		return ClassIncomplete
	case protocol.EventDone:
		if openTool {
			return ClassIncomplete
		}
		return ClassifyStop(event.StopReason)
	default:
		return ""
	}
}

func ClassifyStop(reason string) Class {
	switch normalizeStop(reason) {
	case "", "stop", "end_turn", "tool_calls", "function_call":
		return ClassCompleted
	case "error":
		return ClassFailed
	case "max_tokens", "max_output_tokens", "length",
		"content_filter", "content-filter",
		"model_context_window_exceeded", "pause_turn", "refusal":
		return ClassIncomplete
	default:
		return ClassCompleted
	}
}

func ReplacementAllowed(class Class) bool {
	return class == ClassCompleted
}

func IncompleteReason(reason string) string {
	switch normalizeStop(reason) {
	case "max_tokens", "max_output_tokens", "length":
		return "max_output_tokens"
	case "content_filter", "content-filter":
		return "content_filter"
	case "model_context_window_exceeded":
		return "model_context_window_exceeded"
	case "pause_turn":
		return "pause_turn"
	case "refusal":
		return "refusal"
	default:
		if strings.TrimSpace(reason) == "" {
			return "upstream_incomplete"
		}
		return reason
	}
}

func EffectiveBudget(contextWindow, maxInput, configured int) int {
	if contextWindow <= 0 {
		return 0
	}
	derived := contextWindow * 9 / 10
	if maxInput > 0 && maxInput < derived {
		derived = maxInput
	}
	if configured > 0 && configured < derived {
		derived = configured
	}
	return derived
}

func normalizeStop(reason string) string {
	reason = strings.ToLower(strings.TrimSpace(reason))
	reason = strings.ReplaceAll(reason, " ", "_")
	return reason
}
