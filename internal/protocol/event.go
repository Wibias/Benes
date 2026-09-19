package protocol

import (
	"encoding/json"
	"fmt"
)

type EventType string

const (
	EventHeartbeat             EventType = "heartbeat"
	EventTextDelta             EventType = "text_delta"
	EventThinkingDelta         EventType = "thinking_delta"
	EventThinkingSignature     EventType = "thinking_signature"
	EventRedactedThinking      EventType = "redacted_thinking"
	EventKiroRedactedReasoning EventType = "kiro_redacted_reasoning"
	EventReasoningRawDelta     EventType = "reasoning_raw_delta"
	EventToolCallStart         EventType = "tool_call_start"
	EventToolCallDelta         EventType = "tool_call_delta"
	EventToolCallEnd           EventType = "tool_call_end"
	EventAssistantBoundary     EventType = "assistant_boundary"
	EventWebSearchCallBegin    EventType = "web_search_call_begin"
	EventWebSearchCallEnd      EventType = "web_search_call_end"
	EventDone                  EventType = "done"
	EventIncomplete            EventType = "incomplete"
	EventError                 EventType = "error"
	EventCompaction            EventType = "compaction"
)

type MessagePhase string

const (
	PhaseCommentary  MessagePhase = "commentary"
	PhaseFinalAnswer MessagePhase = "final_answer"
)

type Usage struct {
	InputTokens              int64  `json:"inputTokens"`
	OutputTokens             int64  `json:"outputTokens"`
	ContextTotalTokens       int64  `json:"contextTotalTokens,omitempty"`
	TotalTokens              int64  `json:"totalTokens,omitempty"`
	CachedInputTokens        int64  `json:"cachedInputTokens,omitempty"`
	CacheReadInputTokens     int64  `json:"cacheReadInputTokens,omitempty"`
	CacheCreationInputTokens int64  `json:"cacheCreationInputTokens,omitempty"`
	ReasoningOutputTokens    int64  `json:"reasoningOutputTokens,omitempty"`
	ServiceTier              string `json:"serviceTier,omitempty"`
	Estimated                bool   `json:"estimated,omitempty"`
}

type URLCitation struct {
	URL   string `json:"url"`
	Title string `json:"title,omitempty"`
}

// Event intentionally mirrors the current TypeScript AdapterEvent wire shape.
// A single struct keeps JSON fixtures stable during differential migration while
// Validate enforces the fields required by each discriminated event type.
type Event struct {
	Type EventType `json:"type"`

	Text      string        `json:"text,omitempty"`
	Thinking  string        `json:"thinking,omitempty"`
	Signature string        `json:"signature,omitempty"`
	Data      string        `json:"data,omitempty"`
	Phase     *MessagePhase `json:"phase,omitempty"`

	ID               string          `json:"id,omitempty"`
	Name             string          `json:"name,omitempty"`
	Namespace        string          `json:"namespace,omitempty"`
	Arguments        string          `json:"arguments,omitempty"`
	ProviderMetadata json.RawMessage `json:"providerMetadata,omitempty"`

	Queries []string      `json:"queries,omitempty"`
	Status  string        `json:"status,omitempty"`
	Sources []URLCitation `json:"sources,omitempty"`

	Usage         *Usage                     `json:"usage,omitempty"`
	StopReason    string                     `json:"stopReason,omitempty"`
	EndTurn       *bool                      `json:"endTurn,omitempty"`
	ProviderState map[string]json.RawMessage `json:"providerState,omitempty"`

	Reason    string `json:"reason,omitempty"`
	Message   string `json:"message,omitempty"`
	Retryable *bool  `json:"retryable,omitempty"`

	HTTPStatus int    `json:"statusCode,omitempty"`
	ErrorType  string `json:"errorType,omitempty"`
	Code       string `json:"code,omitempty"`
}

func (e Event) Validate() error {
	switch e.Type {
	case EventHeartbeat,
		EventTextDelta,
		EventThinkingDelta,
		EventThinkingSignature,
		EventRedactedThinking,
		EventReasoningRawDelta,
		EventToolCallDelta,
		EventToolCallEnd,
		EventAssistantBoundary,
		EventDone:
		return nil
	case EventKiroRedactedReasoning:
		hasData := e.Data != ""
		hasSignature := e.Signature != ""
		if hasData == hasSignature {
			return fmt.Errorf("%s requires exactly one opaque reasoning member", e.Type)
		}
		return nil
	case EventToolCallStart:
		if e.ID == "" || e.Name == "" {
			return fmt.Errorf("%s requires id and name", e.Type)
		}
		return nil
	case EventWebSearchCallBegin:
		if e.ID == "" {
			return fmt.Errorf("%s requires id", e.Type)
		}
		return nil
	case EventWebSearchCallEnd:
		if e.ID == "" {
			return fmt.Errorf("%s requires id", e.Type)
		}
		if e.Status != "" && e.Status != "completed" && e.Status != "failed" {
			return fmt.Errorf("%s has invalid status %q", e.Type, e.Status)
		}
		return nil
	case EventIncomplete:
		if e.Reason == "" {
			return fmt.Errorf("%s requires reason", e.Type)
		}
		return nil
	case EventError:
		if e.Message == "" {
			return fmt.Errorf("%s requires message", e.Type)
		}
		return nil
	case EventCompaction:
		return nil
	default:
		return fmt.Errorf("unknown event type %q", e.Type)
	}
}
