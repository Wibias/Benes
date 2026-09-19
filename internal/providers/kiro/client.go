package kiro

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Wibias/Benes/internal/credentialpool"
	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/resourcebudget"
	"github.com/Wibias/Benes/internal/responses/continuation"
	"github.com/Wibias/Benes/internal/transport"
)

const generateTarget = "AmazonCodeWhispererStreamingService.GenerateAssistantResponse"

type Config struct {
	Account           AccountSnapshot
	Accounts          []AccountSnapshot
	Evidence          map[string]credentialpool.Evidence
	Now               time.Time
	Endpoint          string
	HTTPClient        *http.Client
	DestinationPolicy transport.DestinationPolicy
	TransportOptions  transport.ClientOptions
	Continuation      *continuation.Authority
}

type Client struct {
	account      AccountSnapshot
	accounts     []AccountSnapshot
	evidence     map[string]credentialpool.Evidence
	now          time.Time
	pool         *credentialpool.Pool
	endpoint     string
	httpClient   *http.Client
	continuation *continuation.Authority
}

func NewHardened(ctx context.Context, config Config) (*Client, error) {
	if err := config.Account.Validate(); err != nil {
		return nil, err
	}
	endpoint, err := ResolveDestination(config.Endpoint, config.Account.EffectiveRegion())
	if err != nil {
		return nil, err
	}
	if config.HTTPClient == nil {
		target, err := transport.ResolveTarget(ctx, endpoint, config.DestinationPolicy)
		if err != nil {
			return nil, fmt.Errorf("validate Kiro destination: %w", err)
		}
		config.HTTPClient = transport.NewClient(target, config.TransportOptions)
	}
	accounts := append([]AccountSnapshot(nil), config.Accounts...)
	if ref := config.Account.Ref(); ref != "" {
		found := false
		for _, account := range accounts {
			if account.Ref() == ref {
				found = true
				break
			}
		}
		if !found {
			accounts = append([]AccountSnapshot{config.Account}, accounts...)
		}
	}
	client := &Client{
		account:      config.Account,
		accounts:     accounts,
		evidence:     config.Evidence,
		now:          config.Now,
		endpoint:     endpoint,
		httpClient:   config.HTTPClient,
		continuation: config.Continuation,
	}
	client.pool = client.buildCredentialPool()
	return client, nil
}

func (c *Client) Open(ctx context.Context, dispatch providers.DispatchRequest) (providers.EventStream, error) {
	if c == nil {
		return nil, fmt.Errorf("kiro client is required")
	}
	if err := ValidateCapabilities(dispatch.Parsed); err != nil {
		return nil, err
	}
	bound, err := c.bindContinuation(dispatch)
	if err != nil {
		return nil, err
	}
	if bound != nil {
		defer bound.Release()
	}
	parsed := dispatch.Parsed
	if bound != nil {
		parsed = bound.Request
	}
	names := NewToolNameRegistry()
	history, err := CompileHistoryWithRegistry(parsed, names)
	if err != nil {
		return nil, err
	}
	model := NormalizeModelID(parsed.UpstreamModelID)
	if model == "" {
		model = NormalizeModelID(parsed.ModelID)
	}
	payload := buildGeneratePayload(history, c.account.ProfileARN, model)
	if err := ApplyNativeEffort(payload, model, parsed.Options.Reasoning); err != nil {
		return nil, err
	}
	emitted, err := materializeRequestTools(parsed)
	if err != nil {
		return nil, err
	}
	catalog, err := compileToolCatalog(emitted, names)
	if err != nil {
		return nil, err
	}
	if err := attachToolCatalog(payload, catalog); err != nil {
		return nil, err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	if dispatch.Turn != nil {
		if _, err := dispatch.Turn.Reserve(resourcebudget.ClassTranslator, int64(len(body))); err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-amz-json-1.0")
	req.Header.Set("Accept", "application/vnd.amazon.eventstream")
	req.Header.Set("x-amz-target", generateTarget)
	resp, err := c.postGenerate(ctx, req, payload, body, dispatch.Turn)
	if err != nil {
		return nil, err
	}
	out := &stream{body: resp.Body, authority: c.continuation, open: map[string]struct{}{}, names: names, window: ContextWindow(model), turn: dispatch.Turn}
	if bound != nil {
		out.owner = bound.Owner
		out.durable = bound.Durable
	}
	return out, nil
}

func (c *Client) bindContinuation(dispatch providers.DispatchRequest) (*continuation.Bound, error) {
	if c == nil || c.continuation == nil {
		return nil, nil
	}
	model := strings.TrimSpace(dispatch.Parsed.UpstreamModelID)
	if model == "" {
		model = strings.TrimSpace(dispatch.Parsed.ModelID)
	}
	if model == "" {
		model = "kiro"
	}
	bound, err := c.continuation.Bind(continuation.BindRequest{
		Identity: continuation.PhysicalIdentity{
			Provider:      "kiro",
			Destination:   c.continuationDestination(),
			Adapter:       "kiro",
			Model:         model,
			AuthClass:     "oauth",
			Secret:        []byte(c.account.AccessToken),
			AccountHandle: strings.TrimSpace(c.account.ProfileARN),
		},
		Thread:              strings.TrimSpace(dispatch.Parsed.PreviousResponseID),
		Turn:                dispatch.Turn,
		Request:             dispatch.Parsed,
		StripPreviousOnMiss: true,
	})
	if err != nil {
		return nil, err
	}
	return &bound, nil
}

func (c *Client) continuationDestination() string {
	if c == nil {
		return ""
	}
	endpoint := strings.TrimSpace(c.endpoint)
	if endpoint == "" {
		endpoint = RuntimeURL(c.account.EffectiveRegion())
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return endpoint
	}
	query := parsed.Query()
	if profile := strings.TrimSpace(c.account.ProfileARN); profile != "" {
		query.Set("profile", profile)
	}
	if region := strings.TrimSpace(c.account.EffectiveRegion()); region != "" {
		query.Set("region", region)
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

type stream struct {
	body      io.ReadCloser
	buf       []byte
	turn      *resourcebudget.Turn
	toolArgs  *resourcebudget.Accumulator
	authority *continuation.Authority
	owner     continuation.Owner
	durable   bool
	open      map[string]struct{}
	names     *ToolNameRegistry
	window    int64
	contextPct float64
	hasPct     bool
}

func (s *stream) Next() (protocol.Event, error) {
	for {
		if ev, ok, err := s.consume(); err != nil || ok {
			return ev, err
		}
		chunk := make([]byte, 32<<10)
		n, err := s.body.Read(chunk)
		if n > 0 {
			if s.turn != nil {
				if _, err := s.turn.Reserve(resourcebudget.ClassStreamPending, int64(n)); err != nil {
					return protocol.Event{}, err
				}
			}
			s.buf = append(s.buf, chunk[:n]...)
			continue
		}
		if err != nil {
			if err == io.EOF && len(s.open) > 0 {
				return protocol.Event{Type: protocol.EventError, Message: "Kiro stream ended with incomplete tool call"}, nil
			}
			return protocol.Event{}, err
		}
	}
}

func (s *stream) consume() (protocol.Event, bool, error) {
	if len(s.buf) < minMessageLen {
		return protocol.Event{}, false, nil
	}
	total := int(binary.BigEndian.Uint32(s.buf[:4]))
	if total > maxMessageLen {
		return protocol.Event{}, true, fmt.Errorf("Kiro event-stream frame exceeds byte cap")
	}
	if len(s.buf) < total {
		return protocol.Event{}, false, nil
	}
	msg, err := DecodeEventStreamMessage(s.buf[:total])
	s.buf = s.buf[total:]
	if err != nil {
		return protocol.Event{}, true, err
	}
	switch msg.Headers[":event-type"] {
	case "assistantResponseEvent":
		var payload struct {
			Content string `json:"content"`
		}
		if json.Unmarshal(msg.Payload, &payload) == nil && payload.Content != "" {
			return protocol.Event{Type: protocol.EventTextDelta, Text: payload.Content}, true, nil
		}
	case "reasoningContentEvent":
		var payload struct {
			Content         string `json:"content"`
			RedactedContent string `json:"redactedContent"`
			Signature       string `json:"signature"`
		}
		if json.Unmarshal(msg.Payload, &payload) != nil {
			break
		}
		if payload.RedactedContent != "" && payload.Signature != "" {
			return protocol.Event{}, true, fmt.Errorf("Kiro reasoningContentEvent contains conflicting opaque members")
		}
		if payload.Signature != "" {
			return protocol.Event{Type: protocol.EventKiroRedactedReasoning, Signature: payload.Signature}, true, nil
		}
		if payload.RedactedContent != "" {
			return protocol.Event{Type: protocol.EventKiroRedactedReasoning, Data: payload.RedactedContent}, true, nil
		}
		if payload.Content != "" {
			return protocol.Event{Type: protocol.EventThinkingDelta, Thinking: payload.Content}, true, nil
		}
	case "toolUseEvent":
		var payload struct {
			Name      string `json:"name"`
			ToolUseID string `json:"toolUseId"`
			Input     string `json:"input"`
			Stop      bool   `json:"stop"`
		}
		if json.Unmarshal(msg.Payload, &payload) != nil || payload.ToolUseID == "" {
			return protocol.Event{}, true, fmt.Errorf("Kiro toolUseEvent is malformed")
		}
		name := payload.Name
		if s.names != nil {
			name = s.names.Restore(name)
		}
		if payload.Stop {
			if s.toolArgs == nil {
				if err := s.reserveToolArgs(payload.Input); err != nil {
					return protocol.Event{}, true, err
				}
			}
			delete(s.open, payload.ToolUseID)
			return protocol.Event{Type: protocol.EventToolCallEnd, ID: payload.ToolUseID, Name: name, Arguments: payload.Input}, true, nil
		}
		if s.open == nil {
			s.open = map[string]struct{}{}
		}
		s.open[payload.ToolUseID] = struct{}{}
		if payload.Input != "" {
			if err := s.reserveToolArgs(payload.Input); err != nil {
				return protocol.Event{}, true, err
			}
			return protocol.Event{Type: protocol.EventToolCallDelta, ID: payload.ToolUseID, Name: name, Arguments: payload.Input}, true, nil
		}
		return protocol.Event{Type: protocol.EventToolCallStart, ID: payload.ToolUseID, Name: name}, true, nil
	case "contextUsageEvent":
		var payload struct {
			ContextUsagePercentage float64 `json:"contextUsagePercentage"`
		}
		if json.Unmarshal(msg.Payload, &payload) == nil {
			s.contextPct = payload.ContextUsagePercentage
			s.hasPct = true
		}
		return s.consume()
	case "messageMetadataEvent":
		var payload struct {
			ConversationID string `json:"conversationId"`
		}
		if json.Unmarshal(msg.Payload, &payload) == nil && payload.ConversationID != "" && s.authority != nil && s.owner.Credential != "" {
			raw, _ := json.Marshal(map[string]string{"conversationId": payload.ConversationID})
			_ = s.authority.RememberProviderState(s.owner, s.durable, payload.ConversationID, raw)
		}
	case "metadataEvent":
		var payload struct {
			Usage struct {
				InputTokens  int64 `json:"inputTokens"`
				OutputTokens int64 `json:"outputTokens"`
			} `json:"usage"`
			ContextUsagePercentage *float64 `json:"contextUsagePercentage"`
		}
		if json.Unmarshal(msg.Payload, &payload) == nil && (payload.Usage.InputTokens > 0 || payload.Usage.OutputTokens > 0) {
			if payload.ContextUsagePercentage != nil {
				s.contextPct = *payload.ContextUsagePercentage
				s.hasPct = true
			}
			usage := NormalizeKiroUsage(0, payload.Usage.InputTokens, payload.Usage.OutputTokens, s.window)
			if s.hasPct {
				usage = ApplyKiroOccupancy(usage, s.contextPct, s.window)
			}
			return protocol.Event{Type: protocol.EventDone, Usage: &usage}, true, nil
		}
	}
	return protocol.Event{}, false, nil
}

func (s *stream) reserveToolArgs(input string) error {
	if s == nil || s.turn == nil || input == "" {
		return nil
	}
	if s.toolArgs == nil {
		s.toolArgs = resourcebudget.NewAccumulator(s.turn, resourcebudget.ClassToolArguments)
	}
	return s.toolArgs.Append([]byte(input))
}

func (s *stream) Close() error {
	if s == nil {
		return nil
	}
	_ = s.toolArgs.Close()
	if s.body == nil {
		return nil
	}
	return s.body.Close()
}

func buildGeneratePayload(history []HistoryEntry, profileARN, model string) map[string]any {
	var current map[string]any
	var prior []map[string]any
	for i, entry := range history {
		item := map[string]any{}
		if entry.Role == "user" {
			uim := map[string]any{"content": entry.Text}
			if len(entry.Images) > 0 {
				images := make([]map[string]any, 0, len(entry.Images))
				for _, img := range entry.Images {
					images = append(images, map[string]any{"format": img.Format, "source": map[string]any{"bytes": img.Bytes}})
				}
				uim["images"] = images
			}
			if len(entry.ToolResults) > 0 {
				results := make([]map[string]any, 0, len(entry.ToolResults))
				for _, result := range entry.ToolResults {
					status := "success"
					if result.Error {
						status = "error"
					}
					results = append(results, map[string]any{
						"content":   []map[string]any{{"text": result.Text}},
						"status":    status,
						"toolUseId": result.ID,
					})
				}
				uim["userInputMessageContext"] = map[string]any{"toolResults": results}
			}
			item["userInputMessage"] = uim
		} else {
			arm := map[string]any{"content": entry.Text}
			if len(entry.ToolUses) > 0 {
				uses := make([]map[string]any, 0, len(entry.ToolUses))
				for _, use := range entry.ToolUses {
					input := use.Input
					if input == nil {
						input = map[string]any{}
					}
					uses = append(uses, map[string]any{"name": use.Name, "input": input, "toolUseId": use.ID})
				}
				arm["toolUses"] = uses
			}
			if entry.Reasoning.Value != "" {
				arm["reasoningContent"] = map[string]any{string(entry.Reasoning.Member): entry.Reasoning.Value}
			}
			item["assistantResponseMessage"] = arm
		}
		if i == len(history)-1 && entry.Role == "user" {
			current = item
			continue
		}
		prior = append(prior, item)
	}
	if current == nil {
		current = map[string]any{"userInputMessage": map[string]any{"content": ""}}
	}
	state := map[string]any{
		"chatTriggerType": "MANUAL",
		"currentMessage":  current,
	}
	if model = strings.TrimSpace(model); model != "" {
		if uim, ok := current["userInputMessage"].(map[string]any); ok {
			uim["modelId"] = model
		}
	}
	if len(prior) > 0 {
		state["history"] = prior
	}
	payload := map[string]any{"conversationState": state}
	if profileARN != "" {
		payload["profileArn"] = profileARN
	}
	return payload
}
