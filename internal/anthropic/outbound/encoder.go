package outbound

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/resourcebudget"
	"github.com/Wibias/Benes/internal/timeline"
)

const (
	defaultMaxCallArgumentBytes = 2 * 1024 * 1024
	defaultMaxTurnBytes         = 32 * 1024 * 1024
)

var ErrNotTerminal = errors.New("anthropic message is not terminal")

type Options struct {
	IDGenerator          func() string
	SignatureGenerator   func() string
	MaxCallArgumentBytes int
	MaxTurnBytes         int
	Trace                *timeline.Trace
	Turn                 *resourcebudget.Turn
}

type Frame struct {
	Name    string
	Payload map[string]any
}

type TerminalError struct {
	Status  int
	Type    string
	Code    string
	Message string
}

func (e *TerminalError) Error() string { return e.Message }

type blockKind uint8

const (
	blockText blockKind = iota + 1
	blockThinking
	blockTool
)

type openBlock struct {
	kind             blockKind
	index            int
	text             strings.Builder
	signature        string
	signatureEmitted bool
	toolID           string
	toolName         string
	arguments        strings.Builder
	toolEnded        bool
}

type Encoder struct {
	model                string
	id                   string
	idGenerator          func() string
	signatureGenerator   func() string
	maxCallArgumentBytes int
	maxTurnBytes         int
	retainedBytes        int
	started              bool
	terminal             bool
	failure              *TerminalError
	open                 *openBlock
	nextIndex            int
	content              []any
	toolUsed             bool
	webSearchRequests    int
	stopReason           string
	usage                *protocol.Usage
}

func New(model string, options Options) (*Encoder, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		return nil, fmt.Errorf("anthropic message model is required")
	}
	if options.MaxCallArgumentBytes < 0 || options.MaxTurnBytes < 0 {
		return nil, fmt.Errorf("anthropic outbound byte limits cannot be negative")
	}
	maxCall := options.MaxCallArgumentBytes
	if maxCall == 0 {
		maxCall = defaultMaxCallArgumentBytes
	}
	maxTurn := options.MaxTurnBytes
	if maxTurn == 0 {
		maxTurn = defaultMaxTurnBytes
	}
	idGen := options.IDGenerator
	if idGen == nil {
		idGen = messageID
	}
	id := strings.TrimSpace(idGen())
	if id == "" {
		return nil, fmt.Errorf("anthropic message id generator returned a blank id")
	}
	sigGen := options.SignatureGenerator
	if sigGen == nil {
		sigGen = syntheticSignature
	}
	return &Encoder{model: model, id: id, idGenerator: idGen, signatureGenerator: sigGen, maxCallArgumentBytes: maxCall, maxTurnBytes: maxTurn}, nil
}

func (e *Encoder) Handle(event protocol.Event) ([]Frame, error) {
	if e.terminal {
		return nil, fmt.Errorf("anthropic message already terminated")
	}
	if err := event.Validate(); err != nil {
		return e.fail(502, "invalid canonical provider event", "api_error", "upstream_event_invalid"), nil
	}
	switch event.Type {
	case protocol.EventHeartbeat:
		if !e.started {
			return nil, nil
		}
		return []Frame{frame("ping", map[string]any{"type": "ping"})}, nil
	case protocol.EventTextDelta:
		if event.Text == "" {
			return nil, nil
		}
		if !e.reserve(len(event.Text)) {
			return e.bufferFailure(), nil
		}
		frames, ok := e.ensureBlock(blockText)
		if !ok {
			return frames, nil
		}
		e.open.text.WriteString(event.Text)
		frames = append(frames, frame("content_block_delta", map[string]any{"type": "content_block_delta", "index": e.open.index, "delta": map[string]any{"type": "text_delta", "text": event.Text}}))
		return frames, nil
	case protocol.EventThinkingDelta:
		return e.handleThinking(event.Thinking)
	case protocol.EventReasoningRawDelta:
		return e.handleThinking(event.Text)
	case protocol.EventThinkingSignature:
		frames, ok := e.ensureBlock(blockThinking)
		if !ok {
			return frames, nil
		}
		if e.open.signatureEmitted {
			return e.fail(502, "upstream emitted multiple thinking signatures", "api_error", "upstream_event_invalid"), nil
		}
		sig := strings.TrimSpace(event.Signature)
		if sig == "" {
			sig = strings.TrimSpace(e.signatureGenerator())
		}
		if sig == "" {
			return e.fail(502, "thinking signature generator returned blank signature", "api_error", "upstream_event_invalid"), nil
		}
		if !e.reserve(len(sig)) {
			return e.bufferFailure(), nil
		}
		e.open.signature, e.open.signatureEmitted = sig, true
		frames = append(frames, e.signatureFrame(sig))
		return frames, nil
	case protocol.EventToolCallStart:
		if e.open != nil && e.open.kind == blockTool {
			return e.fail(502, "upstream started a tool call before closing the previous call", "api_error", "upstream_tool_call_invalid"), nil
		}
		if !e.reserve(len(event.ID) + len(event.Name)) {
			return e.bufferFailure(), nil
		}
		frames, ok := e.closeOpen()
		if !ok {
			return frames, nil
		}
		frames = append(frames, e.ensureStarted()...)
		index := e.nextIndex
		e.nextIndex++
		e.open = &openBlock{kind: blockTool, index: index, toolID: event.ID, toolName: event.Name}
		e.toolUsed = true
		frames = append(frames, frame("content_block_start", map[string]any{"type": "content_block_start", "index": index, "content_block": map[string]any{"type": "tool_use", "id": event.ID, "name": event.Name, "input": map[string]any{}}}))
		return frames, nil
	case protocol.EventToolCallDelta:
		if e.open == nil || e.open.kind != blockTool || e.open.toolID != event.ID || e.open.toolEnded {
			return e.fail(502, "upstream streamed tool arguments without an open matching call", "api_error", "upstream_tool_call_invalid"), nil
		}
		if e.open.arguments.Len()+len(event.Arguments) > e.maxCallArgumentBytes || !e.reserve(len(event.Arguments)) {
			return e.bufferFailure(), nil
		}
		e.open.arguments.WriteString(event.Arguments)
		return []Frame{frame("content_block_delta", map[string]any{"type": "content_block_delta", "index": e.open.index, "delta": map[string]any{"type": "input_json_delta", "partial_json": event.Arguments}})}, nil
	case protocol.EventToolCallEnd:
		if e.open == nil || e.open.kind != blockTool || e.open.toolID != event.ID || e.open.toolEnded {
			return e.fail(502, "upstream closed an unknown tool call", "api_error", "upstream_tool_call_invalid"), nil
		}
		e.open.toolEnded = true
		frames, _ := e.closeOpen()
		return frames, nil
	case protocol.EventAssistantBoundary:
		frames, _ := e.closeOpen()
		return frames, nil
	case protocol.EventDone:
		e.usage = cloneUsage(event.Usage)
		return e.finish(stopReason(event.StopReason, e.toolUsed)), nil
	case protocol.EventIncomplete:
		e.usage = cloneUsage(event.Usage)
		switch event.Reason {
		case "max_output_tokens", "max_tokens":
			return e.finish("max_tokens"), nil
		case "content_filter", "refusal":
			return e.finish("refusal"), nil
		default:
			message := strings.TrimSpace(event.Message)
			if message == "" {
				message = "upstream stream ended early (" + event.Reason + ")"
			}
			return e.fail(502, message, "api_error", "upstream_incomplete"), nil
		}
	case protocol.EventError:
		status := event.HTTPStatus
		if status <= 0 {
			status = 502
		}
		typ := event.ErrorType
		if typ == "" {
			typ = anthropicErrorType(status)
		}
		return e.fail(status, event.Message, typ, event.Code), nil
	case protocol.EventWebSearchCallBegin:
		return nil, nil
	case protocol.EventWebSearchCallEnd:
		return e.handleWebSearchEnd(event)
	case protocol.EventRedactedThinking, protocol.EventKiroRedactedReasoning:
		return e.fail(502, "redacted reasoning output is not represented by Anthropic outbound yet", "api_error", "unsupported_upstream_event"), nil
	default:
		return e.fail(502, "unsupported canonical provider event", "api_error", "upstream_event_invalid"), nil
	}
}

func (e *Encoder) handleWebSearchEnd(event protocol.Event) ([]Frame, error) {
	frames, ok := e.closeOpen()
	if !ok {
		return frames, nil
	}
	frames = append(frames, e.ensureStarted()...)
	input := webSearchInput(event.Queries)
	encoded, err := json.Marshal(input)
	if err != nil {
		return e.fail(502, "web search query could not be encoded", "api_error", "upstream_event_invalid"), nil
	}
	if !e.reserve(len(event.ID) + len(encoded)) {
		return e.bufferFailure(), nil
	}
	toolIndex := e.nextIndex
	e.nextIndex++
	frames = append(frames,
		frame("content_block_start", map[string]any{"type": "content_block_start", "index": toolIndex, "content_block": map[string]any{"type": "server_tool_use", "id": event.ID, "name": "web_search"}}),
		frame("content_block_delta", map[string]any{"type": "content_block_delta", "index": toolIndex, "delta": map[string]any{"type": "input_json_delta", "partial_json": string(encoded)}}),
		frame("content_block_stop", map[string]any{"type": "content_block_stop", "index": toolIndex}),
	)
	completed := event.Status != "failed"
	var resultContent any
	if completed {
		hits := make([]any, 0, len(event.Sources))
		for _, source := range event.Sources {
			url := strings.TrimSpace(source.URL)
			if url == "" {
				continue
			}
			hits = append(hits, map[string]any{"type": "web_search_result", "title": source.Title, "url": url})
		}
		resultContent = hits
		e.webSearchRequests++
	} else {
		resultContent = map[string]any{"type": "web_search_tool_result_error", "error_code": "unavailable"}
	}
	resultIndex := e.nextIndex
	e.nextIndex++
	frames = append(frames,
		frame("content_block_start", map[string]any{"type": "content_block_start", "index": resultIndex, "content_block": map[string]any{"type": "web_search_tool_result", "tool_use_id": event.ID, "content": resultContent}}),
		frame("content_block_stop", map[string]any{"type": "content_block_stop", "index": resultIndex}),
	)
	e.content = append(e.content,
		map[string]any{"type": "server_tool_use", "id": event.ID, "name": "web_search", "input": input},
		map[string]any{"type": "web_search_tool_result", "tool_use_id": event.ID, "content": resultContent},
	)
	return frames, nil
}

func webSearchInput(queries []string) map[string]any {
	cleaned := make([]string, 0, len(queries))
	for _, query := range queries {
		if trimmed := strings.TrimSpace(query); trimmed != "" {
			cleaned = append(cleaned, trimmed)
		}
	}
	if len(cleaned) > 1 {
		return map[string]any{"queries": cleaned}
	}
	query := ""
	if len(cleaned) == 1 {
		query = cleaned[0]
	}
	return map[string]any{"query": query}
}

func (e *Encoder) handleThinking(text string) ([]Frame, error) {
	if text == "" {
		return nil, nil
	}
	if !e.reserve(len(text)) {
		return e.bufferFailure(), nil
	}
	frames, ok := e.ensureBlock(blockThinking)
	if !ok {
		return frames, nil
	}
	e.open.text.WriteString(text)
	frames = append(frames, frame("content_block_delta", map[string]any{"type": "content_block_delta", "index": e.open.index, "delta": map[string]any{"type": "thinking_delta", "thinking": text}}))
	return frames, nil
}
