package bridge

import (
	"bytes"
	cryptorand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Wibias/Benes/internal/protocol"
)

var ErrUnsupportedEvent = errors.New("responses bridge event is not implemented by this migration slice")

type Frame struct {
	Name string
	Data map[string]any
}

type Options struct {
	ResponseID           string
	CreatedAt            int64
	ID                   func(prefix string) string
	HideThinkingSummary  bool
	Tools                []protocol.Tool
	ToolChoice           *protocol.ToolChoice
	ParallelToolCalls    *bool
	RequestedServiceTier *string
	CompactionRequest    bool
}

type Bridge struct {
	model         string
	requestedTier string
	responseID    string
	createdAt     int64
	id            func(prefix string) string

	started     bool
	terminated  bool
	sequence    int64
	output      []map[string]any
	outputIndex int

	message        *openMessage
	tool           *openTool
	reasoning      *openReasoning
	rawReasoning   *openReasoning
	webSearch      *openWebSearch
	pendingSources []protocol.URLCitation
	toolSchemas    map[string]map[string]any

	hideThinkingSummary bool
	parallelToolCalls   bool
	toolChoice          any
	snapshotTools       []any
	pendingSignature    string
	pendingRedacted     []string
	hiddenThinkingText  string
	hiddenRawReasoning  string
	pendingKiroRedacted string
	compactionRequest   bool
	compactionText      string
}

type openMessage struct {
	id          string
	outputIndex int
	text        string
	phase       *protocol.MessagePhase
}

type openTool struct {
	id               string
	outputIndex      int
	callID           string
	name             string
	namespace        string
	arguments        string
	providerMetadata json.RawMessage // opaque provider continuation state (e.g. Google ThoughtSignature)
}

type openReasoning struct {
	id          string
	outputIndex int
	text        string
}

type openWebSearch struct {
	id          string
	eventID     string
	outputIndex int
}

func New(model string, options Options) *Bridge {
	responseID := options.ResponseID
	if responseID == "" {
		responseID = defaultID("resp_")
	}
	createdAt := options.CreatedAt
	if createdAt == 0 {
		createdAt = time.Now().Unix()
	}
	id := options.ID
	if id == nil {
		id = defaultID
	}
	toolSchemas := make(map[string]map[string]any, len(options.Tools))
	for _, tool := range options.Tools {
		name := tool.Name
		if tool.Namespace != "" {
			name = tool.Namespace + "__" + tool.Name
		}
		if name == "" || len(tool.Parameters) == 0 {
			continue
		}
		toolSchemas[name] = tool.Parameters
	}
	requestedTier := ""
	if options.RequestedServiceTier != nil {
		requestedTier = *options.RequestedServiceTier
	}
	return &Bridge{
		model: model, requestedTier: requestedTier, responseID: responseID, createdAt: createdAt, id: id,
		hideThinkingSummary: options.HideThinkingSummary,
		parallelToolCalls:   snapshotParallelToolCalls(options.ParallelToolCalls),
		toolChoice:          snapshotToolChoice(options.ToolChoice),
		snapshotTools:       snapshotTools(options.Tools),
		toolSchemas:         toolSchemas,
		compactionRequest:   options.CompactionRequest,
	}
}

func (b *Bridge) ResponseID() string {
	if b == nil {
		return ""
	}
	return b.responseID
}

var fallbackIDSequence atomic.Uint64

func defaultID(prefix string) string {
	var random [16]byte
	if _, err := cryptorand.Read(random[:]); err == nil {
		return prefix + hex.EncodeToString(random[:])
	}
	return fmt.Sprintf("%s%x%x", prefix, time.Now().UnixNano(), fallbackIDSequence.Add(1))
}

func (b *Bridge) Start() []Frame {
	if b.started || b.terminated {
		return nil
	}
	b.started = true
	return []Frame{b.emit("response.created", map[string]any{
		"response": b.snapshot("in_progress", nil, nil),
	})}
}

func (b *Bridge) Handle(event protocol.Event) ([]Frame, error) {
	if b.terminated {
		return nil, fmt.Errorf("responses bridge already terminated")
	}
	if !b.started {
		return nil, fmt.Errorf("responses bridge must be started before handling events")
	}
	if err := event.Validate(); err != nil {
		return nil, err
	}

	switch event.Type {
	case protocol.EventHeartbeat:
		return nil, nil
	case protocol.EventAssistantBoundary:
		return b.handleAssistantBoundary()
	case protocol.EventTextDelta:
		return b.handleText(event)
	case protocol.EventThinkingDelta:
		return b.handleThinking(event)
	case protocol.EventThinkingSignature:
		b.pendingSignature = event.Signature
		if b.reasoning == nil {
			return b.flushHiddenReasoningEnvelope(), nil
		}
		return nil, nil
	case protocol.EventRedactedThinking:
		b.pendingRedacted = append(b.pendingRedacted, event.Data)
		return nil, nil
	case protocol.EventKiroRedactedReasoning:
		b.pendingKiroRedacted = event.Data
		return nil, nil
	case protocol.EventReasoningRawDelta:
		return b.handleRawReasoning(event)
	case protocol.EventToolCallStart:
		return b.handleToolStart(event)
	case protocol.EventToolCallDelta:
		if b.tool == nil {
			return nil, fmt.Errorf("tool_call_delta without open tool call")
		}
		b.tool.arguments += event.Arguments
		return []Frame{b.emit("response.function_call_arguments.delta", map[string]any{
			"item_id": b.tool.id, "output_index": b.tool.outputIndex, "delta": event.Arguments,
		})}, nil
	case protocol.EventToolCallEnd:
		if b.tool == nil {
			return nil, fmt.Errorf("tool_call_end without open tool call")
		}
		// Providers may attach final arguments on end (Google-style single End,
		// or Antigravity Start-with-args + End-with-signature).
		if event.Arguments != "" && b.tool.arguments == "" {
			b.tool.arguments = event.Arguments
		}
		// Google/Antigravity attach ThoughtSignature on tool_call_end.
		if len(event.ProviderMetadata) > 0 {
			b.tool.providerMetadata = append(json.RawMessage(nil), event.ProviderMetadata...)
		}
		frames, err := b.closeToolCompleted()
		if err != nil {
			frames = append(frames, b.fail("upstream stream produced malformed tool call arguments", nil)...)
			return frames, err
		}
		return frames, nil
	case protocol.EventWebSearchCallBegin:
		return b.handleWebSearchBegin(event)
	case protocol.EventWebSearchCallEnd:
		return b.handleWebSearchEnd(event), nil
	case protocol.EventDone:
		return b.handleDone(event)
	case protocol.EventCompaction:
		return b.handleNativeCompaction(event)
	case protocol.EventIncomplete:
		frames := b.closeMessageIfOpen(false)
		frames = append(frames, b.closeReasoning()...)
		frames = append(frames, b.closeRawReasoning()...)
		frames = append(frames, b.flushHiddenRawReasoning()...)
		frames = append(frames, b.failTool()...)
		frames = append(frames, b.closeWebSearch("failed", nil, nil)...)
		frames = append(frames, b.flushHiddenReasoningEnvelope()...)
		response := b.snapshot("incomplete", responseUsage(b.model, b.requestedTier, event.Usage), event.EndTurn)

		response["incomplete_details"] = map[string]any{"reason": event.Reason}
		frames = append(frames, b.emit("response.incomplete", map[string]any{"response": response}))
		b.terminated = true
		return frames, nil
	case protocol.EventError:
		frames := b.closeMessageIfOpen(false)
		frames = append(frames, b.closeReasoning()...)
		frames = append(frames, b.closeRawReasoning()...)
		frames = append(frames, b.flushHiddenRawReasoning()...)
		frames = append(frames, b.failTool()...)
		frames = append(frames, b.closeWebSearch("failed", nil, nil)...)
		frames = append(frames, b.fail(event.Message, event.Usage)...)
		return frames, nil
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedEvent, event.Type)
	}
}

func (b *Bridge) End() []Frame {
	if b.terminated || !b.started {
		return nil
	}
	frames := b.closeMessageIfOpen(false)
	frames = append(frames, b.closeReasoning()...)
	frames = append(frames, b.closeRawReasoning()...)
	frames = append(frames, b.flushHiddenRawReasoning()...)
	frames = append(frames, b.failTool()...)
	frames = append(frames, b.closeWebSearch("failed", nil, nil)...)
	response := b.snapshot("incomplete", responseUsage(b.model, b.requestedTier, nil), nil)

	response["incomplete_details"] = map[string]any{"reason": "adapter_eof"}
	frames = append(frames, b.emit("response.incomplete", map[string]any{"response": response}))
	b.terminated = true
	return frames
}

// bridgeExtraContentFromProviderMetadata maps provider Event.ProviderMetadata onto the
// established Responses extra_content.google.thought_signature shape used by history.
func bridgeExtraContentFromProviderMetadata(raw json.RawMessage) json.RawMessage {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	var root map[string]json.RawMessage
	if json.Unmarshal(raw, &root) != nil {
		return nil
	}
	googleRaw, ok := root["google"]
	if !ok {
		return nil
	}
	var google map[string]json.RawMessage
	if json.Unmarshal(googleRaw, &google) != nil {
		return nil
	}
	var sig string
	for _, key := range []string{"thought_signature", "thoughtSignature"} {
		sigRaw, ok := google[key]
		if !ok {
			continue
		}
		if json.Unmarshal(sigRaw, &sig) != nil {
			continue
		}
		sig = strings.TrimSpace(sig)
		if sig == "" || strings.HasPrefix(sig, "fc_") || len(sig) > 64*1024 {
			continue
		}
		out, err := json.Marshal(map[string]any{"google": map[string]string{"thought_signature": sig}})
		if err != nil {
			return nil
		}
		return out
	}
	return nil
}
