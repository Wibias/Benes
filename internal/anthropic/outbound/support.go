package outbound

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/usage"
)

var fallbackIDSeq atomic.Uint64

func (e *Encoder) fail(status int, message, typ, code string) []Frame {
	if e.terminal {
		return nil
	}
	message = strings.TrimSpace(message)
	if message == "" {
		message = "upstream request failed"
	}
	if typ == "" {
		typ = anthropicErrorType(status)
	}
	e.terminal = true
	e.failure = &TerminalError{Status: status, Type: typ, Code: code, Message: message}
	errBody := map[string]any{"type": typ, "message": message}
	if code != "" {
		errBody["code"] = code
	}
	return []Frame{frame("error", map[string]any{"type": "error", "error": errBody})}
}
func (e *Encoder) bufferFailure() []Frame {
	return e.fail(413, "translator buffer exceeded the safe limit", "request_too_large", "translation_buffer_limit")
}
func (e *Encoder) reserve(delta int) bool {
	if delta < 0 || e.retainedBytes+delta > e.maxTurnBytes {
		return false
	}
	e.retainedBytes += delta
	return true
}

func (e *Encoder) usagePayload() map[string]any {
	payload := anthropicUsage(e.usage)
	if e.webSearchRequests > 0 {
		payload["server_tool_use"] = map[string]any{"web_search_requests": e.webSearchRequests}
	}
	var input int64
	confirmed := ""
	if e.usage != nil {
		input = e.usage.InputTokens
		confirmed = e.usage.ServiceTier
	}
	if estimate, ok := usage.ForRoutedModel(e.model, confirmed, "", input, usage.Cost4{}, 0); ok {
		payload["xai_priority_applied"] = estimate.PriorityApplied
		payload["xai_lower_bound"] = estimate.LowerBound
	}
	return payload
}

func anthropicUsage(u *protocol.Usage) map[string]any {
	var input, output, cacheRead, cacheCreate int64
	if u != nil {
		input = u.InputTokens
		output = u.OutputTokens
		cacheRead = u.CacheReadInputTokens
		if cacheRead == 0 {
			cacheRead = u.CachedInputTokens
		}
		cacheCreate = u.CacheCreationInputTokens
	}
	nonCached := input - cacheRead - cacheCreate
	if nonCached < 0 {
		nonCached = 0
	}
	return map[string]any{"input_tokens": nonCached, "output_tokens": output, "cache_read_input_tokens": cacheRead, "cache_creation_input_tokens": cacheCreate}
}
func stopReason(raw string, toolUsed bool) string {
	if toolUsed {
		return "tool_use"
	}
	switch raw {
	case "max_tokens", "max_output_tokens":
		return "max_tokens"
	case "stop_sequence":
		return "stop_sequence"
	case "content_filter", "refusal":
		return "refusal"
	default:
		return "end_turn"
	}
}
func anthropicErrorType(status int) string {
	switch status {
	case 400:
		return "invalid_request_error"
	case 401:
		return "authentication_error"
	case 402:
		return "billing_error"
	case 403:
		return "permission_error"
	case 404:
		return "not_found_error"
	case 409:
		return "conflict_error"
	case 413:
		return "request_too_large"
	case 429:
		return "rate_limit_error"
	case 504:
		return "timeout_error"
	case 529:
		return "overloaded_error"
	default:
		if status >= 500 {
			return "api_error"
		}
		return "invalid_request_error"
	}
}
func cloneUsage(u *protocol.Usage) *protocol.Usage {
	if u == nil {
		return nil
	}
	c := *u
	return &c
}
func frame(name string, payload map[string]any) Frame { return Frame{Name: name, Payload: payload} }
func messageID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err == nil {
		return "msg_" + hex.EncodeToString(b[:])
	}
	return fmt.Sprintf("msg_%016x%08x", uint64(time.Now().UnixNano()), fallbackIDSeq.Add(1))
}
func syntheticSignature() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err == nil {
		return "benes_" + hex.EncodeToString(b[:])
	}
	return fmt.Sprintf("benes_%016x%08x", uint64(time.Now().UnixNano()), fallbackIDSeq.Add(1))
}
