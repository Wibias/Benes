package openairesponses

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Wibias/Benes/internal/capability"
	"github.com/Wibias/Benes/internal/credentials"
	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/providers/xaicapability"
	"github.com/Wibias/Benes/internal/resourcebudget"
	"github.com/Wibias/Benes/internal/responses/continuation"
	"github.com/Wibias/Benes/internal/responses/sse"
	"github.com/Wibias/Benes/internal/transport"
)

var (
	ErrUnsupportedRequestShape  = errors.New("request shape is not migrated to the Go OpenAI Responses path")
	ErrUnsupportedUpstreamEvent = errors.New("upstream Responses event is not migrated to the canonical event contract")
)

type Config struct {
	Endpoint          string
	APIKey            string
	APIKeyPool        []APIKeySlot
	ProviderID        string
	HTTPClient        *http.Client
	MaxStreamBytes    int64
	InactivityTimeout time.Duration
	MaxSSELineBytes   int
	MaxSSEEventBytes  int
	DestinationPolicy transport.DestinationPolicy
	TransportOptions  transport.ClientOptions
	Continuation      *continuation.Authority
	Credentials       credentials.Store
	CredentialRef     credentials.Ref
	Capability        capability.Policy
	Transient5xx      transport.Transient5xxPolicy
	UserAgent          string
}

type Client struct {
	endpoint          string
	apiKey            string
	keyPool           *apiKeyPool
	providerID        string
	httpClient        *http.Client
	maxStreamBytes    int64
	inactivityTimeout time.Duration
	sseLimits         sse.Limits
	continuation      *continuation.Authority
	capability        capability.Policy
	transient5xx      transport.Transient5xxPolicy
	userAgent          string
}

func New(config Config) (*Client, error) {
	if strings.TrimSpace(config.Endpoint) == "" {
		config.Endpoint = "https://api.openai.com/v1/responses"
	}
	parsed, err := url.Parse(config.Endpoint)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
		return nil, fmt.Errorf("invalid OpenAI Responses endpoint")
	}
	if parsed.User != nil {
		return nil, fmt.Errorf("OpenAI Responses endpoint credentials are not allowed")
	}
	if config.Credentials != nil && (strings.TrimSpace(config.CredentialRef.ID) != "" || config.CredentialRef.Source != "") {
		secret, err := config.Credentials.Get(config.CredentialRef)
		if err != nil {
			return nil, fmt.Errorf("resolve OpenAI credential: %w", err)
		}
		config.APIKey = string(secret)
	}
	if strings.TrimSpace(config.APIKey) == "" && len(config.APIKeyPool) == 0 {
		return nil, fmt.Errorf("OpenAI API key is required")
	}
	if strings.TrimSpace(config.Capability.Endpoint) == "" {
		config.Capability.Endpoint = config.Endpoint
	}
	if strings.TrimSpace(config.Capability.Protocol) == "" {
		config.Capability.Protocol = "openai-responses"
	}
	if strings.TrimSpace(config.Capability.AuthClass) == "" {
		config.Capability.AuthClass = "api-key"
	}
	if config.HTTPClient == nil {
		return nil, fmt.Errorf("hardened HTTP client is required")
	}
	if config.MaxStreamBytes <= 0 {
		config.MaxStreamBytes = 64 << 20
	}
	if config.InactivityTimeout <= 0 {
		config.InactivityTimeout = 5 * time.Minute
	}
	providerID := strings.TrimSpace(config.ProviderID)
	if providerID == "" {
		providerID = "openai-apikey"
	}
	apiKey := strings.TrimSpace(config.APIKey)
	keyPool := newAPIKeyPool(config.Endpoint, config.APIKeyPool)
	if apiKey == "" && keyPool != nil {
		_, apiKey = keyPool.selectKey()
	}
	return &Client{
		endpoint: config.Endpoint, apiKey: apiKey, keyPool: keyPool, providerID: providerID, httpClient: config.HTTPClient,
		maxStreamBytes: config.MaxStreamBytes, inactivityTimeout: config.InactivityTimeout,
		sseLimits:    sse.Limits{MaxLineBytes: config.MaxSSELineBytes, MaxEventBytes: config.MaxSSEEventBytes},
		continuation: config.Continuation,
		capability:   config.Capability,
		transient5xx: config.Transient5xx,
		userAgent:    providers.NormalizeUserAgent(config.UserAgent),
	}, nil
}

func NewHardened(ctx context.Context, config Config) (*Client, error) {
	endpoint := strings.TrimSpace(config.Endpoint)
	if endpoint == "" {
		endpoint = "https://api.openai.com/v1/responses"
		config.Endpoint = endpoint
	}
	target, err := transport.ResolveTarget(ctx, endpoint, config.DestinationPolicy)
	if err != nil {
		return nil, fmt.Errorf("validate OpenAI Responses destination: %w", err)
	}
	config.HTTPClient = transport.NewClient(target, config.TransportOptions)
	return New(config)
}

func ValidateMigratedRequest(request protocol.ParsedRequest) error {
	if request.CompactionRequest {
		if len(bytes.TrimSpace(request.Raw)) == 0 {
			return fmt.Errorf("%w: empty preserved request", ErrUnsupportedRequestShape)
		}
		return nil
	}
	if len(bytes.TrimSpace(request.Raw)) == 0 {
		return fmt.Errorf("%w: empty preserved request", ErrUnsupportedRequestShape)
	}
	var rawFields map[string]json.RawMessage
	if err := json.Unmarshal(request.Raw, &rawFields); err != nil {
		return fmt.Errorf("%w: malformed preserved request", ErrUnsupportedRequestShape)
	}
	if hasNonNullField(rawFields, "conversation") {
		return fmt.Errorf("%w: conversation", ErrUnsupportedRequestShape)
	}

	if request.Options.ParallelToolCalls != nil && *request.Options.ParallelToolCalls {
		return fmt.Errorf("%w: parallel_tool_calls=true", ErrUnsupportedRequestShape)
	}
	if requestsEncryptedReasoning(rawFields["include"]) {
		return fmt.Errorf("%w: reasoning.encrypted_content", ErrUnsupportedRequestShape)
	}
	if hasNonNullField(rawFields, "background") {
		return fmt.Errorf("%w: background", ErrUnsupportedRequestShape)
	}
	if hasNonNullField(rawFields, "prompt") {
		return fmt.Errorf("%w: stored prompt reference", ErrUnsupportedRequestShape)
	}
	if err := validateNativeTools(rawFields["tools"]); err != nil {
		return err
	}
	if err := validateNativeInput(rawFields["input"]); err != nil {
		return err
	}
	return nil
}

func hasNonNullField(fields map[string]json.RawMessage, key string) bool {
	raw, ok := fields[key]
	if !ok {
		return false
	}
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null"))
}

func rawBoolTrue(raw json.RawMessage) bool {
	var value bool
	return json.Unmarshal(raw, &value) == nil && value
}

func validateNativeTools(raw json.RawMessage) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil
	}
	if bytes.Equal(trimmed, []byte("null")) {
		return fmt.Errorf("%w: tools", ErrUnsupportedRequestShape)
	}
	var tools []json.RawMessage
	if err := json.Unmarshal(trimmed, &tools); err != nil {
		return fmt.Errorf("%w: tools", ErrUnsupportedRequestShape)
	}
	for index, rawTool := range tools {
		var tool struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(rawTool, &tool); err != nil || tool.Type != "function" {
			return fmt.Errorf("%w: tools[%d]", ErrUnsupportedRequestShape, index)
		}
	}
	return nil
}

func validateNativeInput(raw json.RawMessage) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) || trimmed[0] == '"' {
		return nil
	}
	if trimmed[0] != '[' {
		return fmt.Errorf("%w: input", ErrUnsupportedRequestShape)
	}
	var items []json.RawMessage
	if err := json.Unmarshal(trimmed, &items); err != nil {
		return fmt.Errorf("%w: input", ErrUnsupportedRequestShape)
	}
	for index, rawItem := range items {
		var item map[string]json.RawMessage
		if err := json.Unmarshal(rawItem, &item); err != nil {
			return fmt.Errorf("%w: input[%d]", ErrUnsupportedRequestShape, index)
		}
		typeName := rawString(item["type"])
		if typeName == "" && rawString(item["role"]) != "" {
			typeName = "message"
		}
		switch typeName {
		case "message", "function_call", "function_call_output", "web_search_call", "compaction_trigger", "compaction", "compaction_summary", "context_compaction":
		default:
			return fmt.Errorf("%w: input[%d].type=%s", ErrUnsupportedRequestShape, index, typeName)
		}
	}
	return nil
}

func requestsEncryptedReasoning(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return false
	}
	var values []string
	if json.Unmarshal(raw, &values) != nil {
		return false
	}
	for _, value := range values {
		if value == "reasoning.encrypted_content" {
			return true
		}
	}
	return false
}

// preferNativePreviousWithoutReplay keeps owned previous_response_id while sending only the
// client continuation input (typically function_call_output). Authority.Bind may expand the
// stored prefix for local compilers; for OpenAI Responses that would duplicate history
// alongside previous_response_id in the serialized request body.
func preferNativePreviousWithoutReplay(bound continuation.Bound, original protocol.ParsedRequest) protocol.ParsedRequest {
	request := bound.Request
	if !bound.Hit || !bound.Expanded {
		return request
	}
	if strings.TrimSpace(request.PreviousResponseID) == "" {
		return request
	}
	if !messagesAreToolResultOnly(original.Context.Messages) {
		return request
	}
	request.Context.Messages = append([]protocol.Message(nil), original.Context.Messages...)
	request.ReplayPrefixLength = 0
	return request
}

func messagesAreToolResultOnly(messages []protocol.Message) bool {
	if len(messages) == 0 {
		return false
	}
	for _, msg := range messages {
		if msg.Role != protocol.RoleToolResult {
			return false
		}
	}
	return true
}

func (c *Client) Protocol() string {
	if c == nil {
		return "openai-responses"
	}
	if p := strings.TrimSpace(c.capability.Protocol); p != "" {
		return p
	}
	return "openai-responses"
}

func (c *Client) Open(ctx context.Context, dispatch providers.DispatchRequest) (providers.EventStream, error) {
	if err := capability.Apply(&dispatch.Parsed, c.capability, dispatch.ConfiguredServiceTier); err != nil {
		return nil, err
	}
	if len(dispatch.Parsed.HostedWebSearchTools) != 0 &&
		!c.SupportsNativeHostedWebSearch(firstNonEmptyThread(dispatch.Parsed.UpstreamModelID, dispatch.Parsed.ModelID)) {
		return nil, fmt.Errorf("%w: hosted web search destination", ErrUnsupportedRequestShape)
	}
	keyRef, apiKey, pinCommitted, err := c.resolveDispatchAPIKey(dispatch)
	if err != nil {
		return nil, err
	}
	bound, err := bindDispatchContinuation(c.continuation, dispatch, continuation.PhysicalIdentity{
		Provider:    c.providerID,
		Destination: c.endpoint,
		Adapter:     "openai-responses",
		Model:       firstNonEmptyThread(dispatch.Parsed.UpstreamModelID, dispatch.Parsed.ModelID),
		AuthClass:   "api-key",
		Secret:      []byte(apiKey),
	}, false)
	if err != nil {
		return nil, err
	}
	request := preferNativePreviousWithoutReplay(bound, dispatch.Parsed)
	body, err := prepareCanonicalBody(request)
	if err != nil {
		bound.Release()
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		bound.Release()
		return nil, fmt.Errorf("build OpenAI Responses request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	providers.ApplyUserAgent(req.Header, c.userAgent, dispatch.ForwardHeaders)
	if xaicapability.IsOAuthProxyEndpoint(c.endpoint) {
		xaicapability.ApplyCLIHeaders(req.Header)
	}
	providers.ApplyOpenCodeGoSessionHeader(req.Header, dispatch)
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}
	req.ContentLength = int64(len(body))
	if err := reserveTurnBytes(dispatch.Turn, resourcebudget.ClassTranslator, int64(len(body))); err != nil {
		bound.Release()
		return nil, err
	}
	if err := reserveRequestBlobs(dispatch.Turn, request.Context); err != nil {
		bound.Release()
		return nil, err
	}

	response, err := transport.DoTransient5xx(ctx, c.httpClient, req, c.transient5xx)
	if err != nil {
		bound.Release()
		return nil, fmt.Errorf("OpenAI Responses request failed: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if response.StatusCode == http.StatusTooManyRequests && !pinCommitted {
			if nextRef, nextKey := c.keyPool.failover(keyRef, false); nextKey != "" && nextKey != apiKey {
				_ = response.Body.Close()
				bound.Release()
				// Pre-commit failover: re-open under the next credential so Authority
				// binds to the key that will actually own any native continuation state.
				retry := providers.CloneDispatch(dispatch)
				retry.PhysicalPin = physicalPinFromKey(c.endpoint, nextRef, nextKey)
				retry.PreferCommitted = false
				return c.Open(ctx, retry)
			}
		}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		bound.Release()
		var errorBody []byte
		if response.StatusCode == http.StatusBadRequest {
			errorBody = readForwardErrorBody(ctx, response)
		}
		_ = response.Body.Close()
		return nil, upstreamHTTPError("OpenAI Responses upstream", response.StatusCode, errorBody)
	}
	mediaType, _, mediaErr := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if mediaErr != nil || mediaType != "text/event-stream" {
		bound.Release()
		_ = response.Body.Close()
		return nil, fmt.Errorf("OpenAI Responses upstream must return text/event-stream")
	}

	pipeReader, pipeWriter := io.Pipe()
	copyCtx, cancel := context.WithCancel(ctx)
	go func() {
		_, copyErr := transport.CopyBounded(copyCtx, pipeWriter, response.Body, transport.StreamLimits{
			MaxBytes: c.maxStreamBytes, InactivityTimeout: c.inactivityTimeout,
		})
		_ = pipeWriter.CloseWithError(copyErr)
	}()
	stream := &Stream{
		decoder:  sse.NewDecoder(pipeReader, c.sseLimits),
		reader:   pipeReader,
		cancel:   cancel,
		turn:     dispatch.Turn,
		physical: physicalPinFromKey(c.endpoint, keyRef, apiKey),
	}
	attachContinuationPersistence(stream, c.continuation, bound, continuationThread(dispatch))
	return wrapContinuationStream(stream, bound), nil
}

func (c *Client) resolveDispatchAPIKey(dispatch providers.DispatchRequest) (ref, key string, committed bool, err error) {
	if c == nil {
		return "", "", false, fmt.Errorf("OpenAI Responses client is required")
	}
	committed = dispatch.PreferCommitted || dispatch.PhysicalPin != nil || strings.TrimSpace(dispatch.Parsed.PreviousResponseID) != ""
	if pin := dispatch.PhysicalPin; pin != nil && strings.TrimSpace(pin.CredentialRef) != "" {
		ref = strings.TrimSpace(pin.CredentialRef)
		if ref == "primary" || c.keyPool == nil {
			key = strings.TrimSpace(c.apiKey)
			if key == "" {
				return "", "", true, fmt.Errorf("OpenAI Responses pinned credential unavailable")
			}
			return ref, key, true, nil
		}
		gotRef, gotKey, ok := c.keyPool.keyByRef(ref)
		if !ok {
			return "", "", true, fmt.Errorf("OpenAI Responses pinned credential unavailable")
		}
		return gotRef, gotKey, true, nil
	}
	if committed && c.continuation != nil && c.keyPool != nil && strings.TrimSpace(dispatch.Parsed.PreviousResponseID) != "" {
		if gotRef, gotKey, ok := c.findKeyOwningPrevious(dispatch); ok {
			return gotRef, gotKey, true, nil
		}
		if dispatch.PreferCommitted {
			return "", "", true, fmt.Errorf("OpenAI Responses continuation credential unavailable")
		}
	}
	ref, key = c.selectAPIKey()
	if strings.TrimSpace(key) == "" {
		return "", "", committed, fmt.Errorf("OpenAI Responses API key is required")
	}
	return ref, key, committed, nil
}

func (c *Client) findKeyOwningPrevious(dispatch providers.DispatchRequest) (string, string, bool) {
	prev := strings.TrimSpace(dispatch.Parsed.PreviousResponseID)
	if c == nil || c.continuation == nil || prev == "" {
		return "", "", false
	}
	model := firstNonEmptyThread(dispatch.Parsed.UpstreamModelID, dispatch.Parsed.ModelID)
	type cand struct{ ref, key string }
	candidates := []cand{{"primary", c.apiKey}}
	if c.keyPool != nil {
		for _, ref := range c.keyPool.eachRef() {
			if _, key, ok := c.keyPool.keyByRef(ref); ok {
				candidates = append(candidates, cand{ref, key})
			}
		}
	}
	seen := map[string]bool{}
	for _, item := range candidates {
		if strings.TrimSpace(item.key) == "" || seen[item.key] {
			continue
		}
		seen[item.key] = true
		owner, _, err := continuation.PhysicalIdentity{
			Provider:    c.providerID,
			Destination: c.endpoint,
			Adapter:     "openai-responses",
			Model:       model,
			AuthClass:   "api-key",
			Secret:      []byte(item.key),
		}.Resolve(c.continuation.InstallationSalt(), c.continuation.ProcessSalt())
		if err != nil {
			continue
		}
		if c.continuation.OwnsPrevious(owner, prev) {
			return item.ref, item.key, true
		}
	}
	return "", "", false
}

func (c *Client) selectAPIKey() (string, string) {
	if c == nil {
		return "", ""
	}
	if ref, key := c.keyPool.selectKey(); key != "" {
		return ref, key
	}
	return "primary", c.apiKey
}

func reserveTurnBytes(turn *resourcebudget.Turn, class resourcebudget.Class, bytes int64) error {
	if turn == nil || bytes <= 0 {
		return nil
	}
	_, err := turn.Reserve(class, bytes)
	return err
}

func reserveRequestBlobs(turn *resourcebudget.Turn, ctx protocol.Context) error {
	if turn == nil {
		return nil
	}
	var n int64
	for _, message := range ctx.Messages {
		for _, part := range message.Content {
			if part.Type == protocol.ContentImage {
				n += int64(len(part.ImageURL) + len(part.Text))
			}
		}
	}
	return reserveTurnBytes(turn, resourcebudget.ClassBlob, n)
}

type Stream struct {
	decoder            *sse.Decoder
	reader             *io.PipeReader
	cancel             context.CancelFunc
	pending            []protocol.Event
	tool               *toolState
	closed             bool
	terminal           bool
	onTerminalResponse func(json.RawMessage)
	release            func()
	turn               *resourcebudget.Turn
	toolArgs           *resourcebudget.Accumulator
	physical           *providers.PhysicalPin
}

func (s *Stream) PhysicalOwnership() *providers.PhysicalPin {
	if s == nil || s.physical == nil {
		return nil
	}
	pin := *s.physical
	return &pin
}

type toolState struct {
	itemID    string
	callID    string
	name      string
	arguments string
	ended     bool
}

func (s *Stream) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	if s.toolArgs != nil {
		_ = s.toolArgs.Close()
	}
	if s.release != nil {
		s.release()
	}
	s.cancel()
	return s.reader.Close()
}

func (s *Stream) noteTerminal(response json.RawMessage) {
	if s == nil || s.onTerminalResponse == nil {
		return
	}
	s.onTerminalResponse(response)
}

func (s *Stream) Next() (protocol.Event, error) {
	if len(s.pending) > 0 {
		event := s.pending[0]
		s.pending = s.pending[1:]
		return event, nil
	}
	if s.terminal {
		return protocol.Event{}, io.EOF
	}
	for {
		event, err := s.decoder.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return protocol.Event{}, io.EOF
			}
			return protocol.Event{}, err
		}
		if err := reserveTurnBytes(s.turn, resourcebudget.ClassStreamPending, int64(len(event.Data))); err != nil {
			return protocol.Event{}, err
		}
		var payload map[string]json.RawMessage
		if err := json.Unmarshal([]byte(event.Data), &payload); err != nil {
			return protocol.Event{}, fmt.Errorf("decode OpenAI Responses event JSON: %w", err)
		}
		typeName := rawString(payload["type"])
		switch typeName {
		case "response.created", "response.in_progress", "response.queued", "response.content_part.added", "response.content_part.done", "response.output_text.done":
			// Lifecycle envelopes are reconstructed by internal/responses/bridge
			// so sparse custom gateways still produce a Codex-committable turn.
			continue
		case "response.output_text.delta":
			return protocol.Event{Type: protocol.EventTextDelta, Text: rawString(payload["delta"])}, nil
		case "response.reasoning_summary_text.delta":
			return protocol.Event{Type: protocol.EventThinkingDelta, Thinking: rawString(payload["delta"])}, nil
		case "response.reasoning_text.delta":
			return protocol.Event{Type: protocol.EventReasoningRawDelta, Text: rawString(payload["delta"])}, nil
		case "response.reasoning_summary_part.added", "response.reasoning_summary_part.done", "response.reasoning_summary_text.done", "response.reasoning_text.done":
			continue
		case "response.output_item.added":
			var item map[string]json.RawMessage
			if err := json.Unmarshal(payload["item"], &item); err != nil {
				return protocol.Event{}, err
			}
			switch rawString(item["type"]) {
			case "message":
				continue
			case "reasoning":
				if hasOpaqueReasoning(item) {
					return protocol.Event{}, fmt.Errorf("%w: encrypted reasoning output", ErrUnsupportedUpstreamEvent)
				}
				continue
			case "compaction", "compaction_summary":
				continue
			case "function_call":
				if s.tool != nil && !s.tool.ended {
					return protocol.Event{}, fmt.Errorf("OpenAI Responses opened a function call before closing the previous call")
				}
				s.tool = &toolState{itemID: rawString(item["id"]), callID: rawString(item["call_id"]), name: rawString(item["name"]), arguments: rawString(item["arguments"])}
				if s.turn != nil {
					s.toolArgs = resourcebudget.NewAccumulator(s.turn, resourcebudget.ClassToolArguments)
					if err := s.toolArgs.Append([]byte(s.tool.arguments)); err != nil {
						return protocol.Event{}, err
					}
				}
				return protocol.Event{Type: protocol.EventToolCallStart, ID: s.tool.callID, Name: s.tool.name}, nil
			default:
				return protocol.Event{}, fmt.Errorf("%w: output item %s", ErrUnsupportedUpstreamEvent, rawString(item["type"]))
			}
		case "response.function_call_arguments.delta":
			if s.tool == nil || s.tool.ended {
				return protocol.Event{}, fmt.Errorf("function arguments delta without open call")
			}
			delta := rawString(payload["delta"])
			s.tool.arguments += delta
			if s.toolArgs != nil {
				if err := s.toolArgs.Append([]byte(delta)); err != nil {
					return protocol.Event{}, err
				}
			}
			return protocol.Event{Type: protocol.EventToolCallDelta, ID: s.tool.callID, Arguments: delta}, nil
		case "response.function_call_arguments.done":
			if s.tool == nil || s.tool.ended {
				continue
			}
			full := rawString(payload["arguments"])
			if full != "" && full != s.tool.arguments {
				if strings.HasPrefix(full, s.tool.arguments) {
					suffix := strings.TrimPrefix(full, s.tool.arguments)
					s.tool.arguments = full
					if suffix != "" {
						s.pending = append(s.pending, protocol.Event{Type: protocol.EventToolCallEnd, ID: s.tool.callID})
						s.tool.ended = true
						return protocol.Event{Type: protocol.EventToolCallDelta, ID: s.tool.callID, Arguments: suffix}, nil
					}
				} else {
					return protocol.Event{}, fmt.Errorf("function argument snapshot did not match streamed prefix")
				}
			}
			s.tool.ended = true
			return protocol.Event{Type: protocol.EventToolCallEnd, ID: s.tool.callID}, nil
		case "response.output_item.done":
			var item map[string]json.RawMessage
			if json.Unmarshal(payload["item"], &item) != nil {
				continue
			}
			switch rawString(item["type"]) {
			case "compaction", "compaction_summary":
				return protocol.Event{Type: protocol.EventCompaction, ID: rawString(item["id"]), Data: rawString(item["encrypted_content"])}, nil
			case "reasoning":
				if hasOpaqueReasoning(item) {
					return protocol.Event{}, fmt.Errorf("%w: encrypted reasoning output", ErrUnsupportedUpstreamEvent)
				}
				continue
			case "function_call":
				if s.tool != nil && !s.tool.ended {
					full := rawString(item["arguments"])
					if full != "" && full != s.tool.arguments {
						if !strings.HasPrefix(full, s.tool.arguments) {
							return protocol.Event{}, fmt.Errorf("function argument item did not match streamed prefix")
						}
						suffix := strings.TrimPrefix(full, s.tool.arguments)
						s.tool.arguments = full
						if suffix != "" {
							s.pending = append(s.pending, protocol.Event{Type: protocol.EventToolCallEnd, ID: s.tool.callID})
							s.tool.ended = true
							return protocol.Event{Type: protocol.EventToolCallDelta, ID: s.tool.callID, Arguments: suffix}, nil
						}
					}
					s.tool.ended = true
					return protocol.Event{Type: protocol.EventToolCallEnd, ID: s.tool.callID}, nil
				}
			}
			continue
		case "response.completed":
			s.noteTerminal(payload["response"])
			if s.tool != nil && !s.tool.ended {
				s.tool.ended = true
				s.pending = append(s.pending, nativeDoneEvent(payload["response"]))
				s.terminal = true
				return protocol.Event{Type: protocol.EventToolCallEnd, ID: s.tool.callID}, nil
			}
			s.terminal = true
			return nativeDoneEvent(payload["response"]), nil
		case "response.incomplete":
			s.noteTerminal(payload["response"])
			s.terminal = true
			return protocol.Event{Type: protocol.EventIncomplete, Reason: incompleteReason(payload["response"]), Usage: usageFromResponse(payload["response"]), ProviderState: nativePreviousProviderState(payload["response"])}, nil
		case "response.failed", "error":
			s.terminal = true
			return protocol.Event{Type: protocol.EventError, Message: upstreamFailureMessage(payload), Usage: usageFromResponse(payload["response"])}, nil
		case "response.output_text.annotation.added", "response.refusal.delta", "response.refusal.done", "response.web_search_call.in_progress", "response.web_search_call.searching", "response.web_search_call.completed":
			return protocol.Event{}, fmt.Errorf("%w: %s", ErrUnsupportedUpstreamEvent, typeName)
		default:
			if strings.HasPrefix(typeName, "response.") {
				return protocol.Event{}, fmt.Errorf("%w: %s", ErrUnsupportedUpstreamEvent, typeName)
			}
			return protocol.Event{}, fmt.Errorf("unknown OpenAI Responses event %q", typeName)
		}
	}
}

func nativeDoneEvent(response json.RawMessage) protocol.Event {
	return protocol.Event{
		Type:          protocol.EventDone,
		Usage:         usageFromResponse(response),
		ProviderState: nativePreviousProviderState(response),
	}
}

func nativePreviousProviderState(response json.RawMessage) map[string]json.RawMessage {
	var body struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(response, &body) != nil {
		return nil
	}
	id := strings.TrimSpace(body.ID)
	if id == "" {
		return nil
	}
	payload, err := json.Marshal(map[string]string{"id": id})
	if err != nil {
		return nil
	}
	return map[string]json.RawMessage{"openai_responses_previous": payload}
}

func hasOpaqueReasoning(item map[string]json.RawMessage) bool {
	raw, ok := item["encrypted_content"]
	if !ok {
		return false
	}
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null")) && rawString(raw) != ""
}

func rawString(raw json.RawMessage) string {
	var value string
	_ = json.Unmarshal(raw, &value)
	return value
}

func usageFromResponse(raw json.RawMessage) *protocol.Usage {
	var response struct {
		ServiceTier string `json:"service_tier"`
		Usage       *struct {
			InputTokens  int64 `json:"input_tokens"`
			OutputTokens int64 `json:"output_tokens"`
			TotalTokens  int64 `json:"total_tokens"`
			InputDetails struct {
				CachedTokens int64 `json:"cached_tokens"`
			} `json:"input_tokens_details"`
			OutputDetails struct {
				ReasoningTokens int64 `json:"reasoning_tokens"`
			} `json:"output_tokens_details"`
		} `json:"usage"`
	}
	if json.Unmarshal(raw, &response) != nil || response.Usage == nil {
		return nil
	}
	return &protocol.Usage{
		InputTokens:           response.Usage.InputTokens,
		OutputTokens:          response.Usage.OutputTokens,
		TotalTokens:           response.Usage.TotalTokens,
		CachedInputTokens:     response.Usage.InputDetails.CachedTokens,
		ReasoningOutputTokens: response.Usage.OutputDetails.ReasoningTokens,
		ServiceTier:           strings.TrimSpace(response.ServiceTier),
	}
}

func incompleteReason(raw json.RawMessage) string {
	var response struct {
		IncompleteDetails struct {
			Reason string `json:"reason"`
		} `json:"incomplete_details"`
	}
	_ = json.Unmarshal(raw, &response)
	if response.IncompleteDetails.Reason == "" {
		return "upstream_incomplete"
	}
	return response.IncompleteDetails.Reason
}

func upstreamFailureMessage(payload map[string]json.RawMessage) string {
	if responseRaw, ok := payload["response"]; ok {
		var response struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(responseRaw, &response) == nil && response.Error.Message != "" {
			return response.Error.Message
		}
	}
	var message string
	if raw, ok := payload["message"]; ok {
		_ = json.Unmarshal(raw, &message)
	}
	if message == "" {
		message = "OpenAI Responses upstream failed"
	}
	return message
}
