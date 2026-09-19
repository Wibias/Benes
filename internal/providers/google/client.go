package google

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/Wibias/Benes/internal/capability"
	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/resourcebudget"
	"github.com/Wibias/Benes/internal/responses/continuation"
	"github.com/Wibias/Benes/internal/transport"
)

type Config struct {
	Kind              Kind
	APIKey            string
	AccessToken       string
	Project           string
	Location          string
	Endpoint          string
	HTTPClient        *http.Client
	Rename            WireRenamePolicy
	Continuation      *continuation.Authority
	CatalogEfforts    map[string][]string
	Keys              []KeySlot
	DestinationPolicy transport.DestinationPolicy
	TransportOptions  transport.ClientOptions
	ADC               ADCEnv
	Capability        capability.Policy
}

type Client struct {
	kind           Kind
	apiKey         string
	accessToken    string
	project        string
	location       string
	endpoint       string
	httpClient     *http.Client
	rename         WireRenamePolicy
	testOrigin     string
	continuation   *continuation.Authority
	catalogEfforts map[string][]string
	keys           *KeyPool
	adc            ADCEnv
	capability     capability.Policy
}

func New(ctx context.Context, config Config) (*Client, error) {
	kind := config.Kind
	if kind == "" {
		kind = KindAIStudio
	}
	switch kind {
	case KindAIStudio:
		if strings.TrimSpace(config.APIKey) == "" && len(config.Keys) > 0 {
			config.APIKey = config.Keys[0].Key
		}
		if strings.TrimSpace(config.APIKey) == "" {
			return nil, fmt.Errorf("google (AI Studio) requires a non-empty API key")
		}
	case KindVertex:
		if strings.TrimSpace(config.APIKey) == "" && (strings.TrimSpace(config.Project) == "" || strings.TrimSpace(config.Location) == "") {
			return nil, fmt.Errorf("Vertex AI requires an API key or project and location")
		}
		if strings.TrimSpace(config.APIKey) == "" {
			if err := ValidateLocation(config.Location); err != nil {
				return nil, err
			}
		}
	default:
		return nil, fmt.Errorf("Google kind %q is unsupported", kind)
	}
	if strings.TrimSpace(config.Capability.Protocol) == "" {
		if kind == KindVertex {
			config.Capability.Protocol = "google-vertex"
		} else {
			config.Capability.Protocol = "google"
		}
	}
	if config.HTTPClient == nil {
		dest := AIStudioAPI
		if kind == KindVertex {
			resolved, err := ResolveVertexDestination(config.Location)
			if err != nil {
				return nil, err
			}
			dest = resolved
		} else if strings.TrimSpace(config.Endpoint) != "" {
			resolved, err := ResolveAIStudioDestination(config.Endpoint)
			if err != nil {
				return nil, err
			}
			dest = resolved
		}
		if ctx == nil {
			ctx = context.Background()
		}
		target, err := transport.ResolveTarget(ctx, dest, config.DestinationPolicy)
		if err != nil {
			return nil, fmt.Errorf("validate Google destination: %w", err)
		}
		config.HTTPClient = transport.NewClient(target, config.TransportOptions)
	}
	return &Client{
		kind:           kind,
		apiKey:         config.APIKey,
		accessToken:    config.AccessToken,
		project:        config.Project,
		location:       config.Location,
		endpoint:       config.Endpoint,
		httpClient:     config.HTTPClient,
		rename:         config.Rename,
		continuation:   config.Continuation,
		catalogEfforts: config.CatalogEfforts,
		keys:           NewKeyPool(AIStudioAPI, config.Keys),
		adc:            config.ADC,
		capability:     config.Capability,
	}, nil
}

func (c *Client) Open(ctx context.Context, dispatch providers.DispatchRequest) (providers.EventStream, error) {
	if c == nil {
		return nil, fmt.Errorf("google client is required")
	}
	identity, err := ResolveIdentity(c.kind, firstNonEmpty(dispatch.Parsed.UpstreamModelID, dispatch.Parsed.ModelID), c.rename)
	if err != nil {
		return nil, err
	}
	if err := capability.RequireStructuredOutput(&dispatch.Parsed, c.capability, identity.PublicID); err != nil {
		return nil, err
	}
	if err := c.resolveAccessToken(ctx); err != nil {
		return nil, err
	}
	keyID, key, pinCommitted, err := c.resolveDispatchAPIKey(dispatch)
	if err != nil {
		return nil, err
	}
	bound, err := c.bindContinuation(dispatch, identity, key)
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
	body, err := CompileRequest(parsed)
	if err != nil {
		return nil, err
	}
	ApplyThinking(body, parsed.Options.Reasoning, c.catalogEfforts[identity.PublicID])
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	if dispatch.Turn != nil {
		if _, err := dispatch.Turn.Reserve(resourcebudget.ClassTranslator, int64(len(raw))); err != nil {
			return nil, err
		}
	}
	ep := Endpoint{Kind: c.kind, BaseURL: c.endpoint, Project: c.project, Location: c.location}
	if c.kind == KindVertex && strings.TrimSpace(c.apiKey) != "" && ep.Project == "" {
		ep.Project = "api-key"
	}
	target, err := GenerateURL(ep, identity, true)
	if err != nil {
		return nil, err
	}
	if !strings.Contains(target, "?") {
		target += "?alt=sse"
	}
	if rewrite := strings.TrimSpace(c.testOrigin); rewrite != "" {
		official, err := url.Parse(target)
		if err != nil {
			return nil, err
		}
		base, err := url.Parse(rewrite)
		if err != nil {
			return nil, err
		}
		base.Path = official.Path
		base.RawQuery = official.RawQuery
		target = base.String()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(raw)), nil }
	req.Header.Set("Content-Type", "application/json")
	client := c.httpClient
	if client == nil {
		client = transport.DefaultUnpinnedClient()
	}
	if strings.TrimSpace(key) != "" {
		req.Header.Set("x-goog-api-key", key)
	} else if token := strings.TrimSpace(c.accessToken); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	attempts := 0
	if c.keys != nil {
		attempts = 1
	}
	resp, err := DoForTurn(ctx, client, req, RetryPolicy{Attempts: attempts}, dispatch.Turn, "google")
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusTooManyRequests && c.keys != nil {
		peek, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		_ = resp.Body.Close()
		if !QuotaExhausted(peek) && !pinCommitted {
			nextID, nextKey := c.keys.Rotate(keyID, false)
			if nextKey != "" && nextKey != key {
				retry := providers.CloneDispatch(dispatch)
				retry.PhysicalPin = physicalPinFromKey(continuationDestination(c), nextID)
				retry.PreferCommitted = false
				return c.Open(ctx, retry)
			}
		}
		resp.Body = io.NopCloser(bytes.NewReader(peek))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("Google GenerateContent returned HTTP %d", resp.StatusCode)
	}
	out := &stream{
		body:     resp.Body,
		public:   identity.PublicID,
		turn:     dispatch.Turn,
		physical: physicalPinFromKey(continuationDestination(c), keyID),
	}
	if bound != nil && c.continuation != nil {
		out.continuation = c.continuation
		out.owner = bound.Owner
		out.durable = bound.Durable
		out.thread = firstNonEmpty(dispatch.Parsed.PreviousResponseID)
	}
	return out, nil
}

func (c *Client) resolveDispatchAPIKey(dispatch providers.DispatchRequest) (ref, key string, committed bool, err error) {
	committed = dispatch.PreferCommitted || dispatch.PhysicalPin != nil || googleDispatchOwnsContinuation(dispatch)
	if pin := dispatch.PhysicalPin; pin != nil && strings.TrimSpace(pin.CredentialRef) != "" {
		ref = strings.TrimSpace(pin.CredentialRef)
		if ref == "primary" || c.keys == nil {
			key = strings.TrimSpace(c.apiKey)
			if key == "" && strings.TrimSpace(c.accessToken) == "" {
				return "", "", true, fmt.Errorf("Google pinned credential unavailable")
			}
			return ref, key, true, nil
		}
		gotRef, gotKey, ok := c.keys.keyByRef(ref)
		if !ok {
			return "", "", true, fmt.Errorf("Google pinned credential unavailable")
		}
		return gotRef, gotKey, true, nil
	}
	ref, key = "", c.apiKey
	if c.keys != nil {
		ref, key = c.keys.Select()
	}
	if ref == "" {
		ref = "primary"
	}
	return ref, key, committed, nil
}

func googleDispatchOwnsContinuation(dispatch providers.DispatchRequest) bool {
	if strings.TrimSpace(dispatch.Parsed.PreviousResponseID) != "" {
		return true
	}
	for _, msg := range dispatch.Parsed.Context.Messages {
		for _, part := range msg.Content {
			if strings.TrimSpace(part.ThoughtSignature) != "" {
				return true
			}
			if part.ProviderMetadata != nil && part.ProviderMetadata.Google != nil && strings.TrimSpace(part.ProviderMetadata.Google.ThoughtSignature) != "" {
				return true
			}
		}
	}
	return false
}

type stream struct {
	body            io.ReadCloser
	buf             string
	turn            *resourcebudget.Turn
	public          string
	finish          string
	tools           int
	sawFrame        bool
	emittedTerminal bool
	usage           *protocol.Usage
	continuation    *continuation.Authority
	owner           continuation.Owner
	durable         bool
	thread          string
	physical        *providers.PhysicalPin
}

func (s *stream) PhysicalOwnership() *providers.PhysicalPin {
	if s == nil || s.physical == nil {
		return nil
	}
	pin := *s.physical
	return &pin
}

func (s *stream) Next() (protocol.Event, error) {
	for {
		if ev, ok, err := s.consume(); err != nil || ok {
			return ev, err
		}
		if s.body == nil {
			return s.terminal()
		}
		chunk := make([]byte, 32<<10)
		n, err := s.body.Read(chunk)
		if n > 0 {
			if s.turn != nil {
				if _, err := s.turn.Reserve(resourcebudget.ClassStreamPending, int64(n)); err != nil {
					return protocol.Event{}, err
				}
			}
			s.buf += string(chunk[:n])
			continue
		}
		if err != nil {
			if err == io.EOF {
				return s.terminal()
			}
			return protocol.Event{}, err
		}
	}
}

func (s *stream) consume() (protocol.Event, bool, error) {
	for {
		idx := strings.Index(s.buf, "\n")
		if idx < 0 {
			return protocol.Event{}, false, nil
		}
		line := strings.TrimRight(s.buf[:idx], "\r")
		s.buf = s.buf[idx+1:]
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var probe map[string]json.RawMessage
		if json.Unmarshal([]byte(payload), &probe) != nil {
			continue
		}
		var frame struct {
			UsageMetadata struct {
				PromptTokenCount        int64 `json:"promptTokenCount"`
				CandidatesTokenCount    int64 `json:"candidatesTokenCount"`
				TotalTokenCount         int64 `json:"totalTokenCount"`
				CachedContentTokenCount int64 `json:"cachedContentTokenCount"`
			} `json:"usageMetadata"`
			Candidates []struct {
				FinishReason string `json:"finishReason"`
				Content      struct {
					Parts []struct {
						Text         string `json:"text"`
						Thought      bool   `json:"thought"`
						FunctionCall *struct {
							Name string         `json:"name"`
							Args map[string]any `json:"args"`
							ID   string         `json:"id"`
						} `json:"functionCall"`
						ThoughtSignature string `json:"thoughtSignature"`
					} `json:"parts"`
				} `json:"content"`
			} `json:"candidates"`
		}
		if err := json.Unmarshal([]byte(payload), &frame); err != nil {
			if claimedModelContent(probe["candidates"]) {
				return protocol.Event{Type: protocol.EventError, Message: "malformed Google model-content SSE frame", Usage: s.usage}, true, nil
			}
			continue
		}
		if frame.UsageMetadata.PromptTokenCount > 0 || frame.UsageMetadata.CandidatesTokenCount > 0 {
			u := NormalizeUsage(0, frame.UsageMetadata.PromptTokenCount, frame.UsageMetadata.CandidatesTokenCount, frame.UsageMetadata.CachedContentTokenCount, 0)
			s.usage = &u
		}
		if len(frame.Candidates) == 0 {
			continue
		}
		s.sawFrame = true
		if frame.Candidates[0].FinishReason != "" {
			s.finish = frame.Candidates[0].FinishReason
		}
		for _, part := range frame.Candidates[0].Content.Parts {
			if part.FunctionCall != nil && part.FunctionCall.Name != "" {
				s.tools++
				args, _ := json.Marshal(part.FunctionCall.Args)
				if s.turn != nil {
					if _, err := s.turn.Reserve(resourcebudget.ClassToolArguments, int64(len(args))); err != nil {
						return protocol.Event{}, true, err
					}
				}
				ev := protocol.Event{Type: protocol.EventToolCallEnd, ID: part.FunctionCall.ID, Name: part.FunctionCall.Name, Arguments: string(args)}
				if part.ThoughtSignature != "" {
					raw, _ := json.Marshal(map[string]any{"google": map[string]string{"thoughtSignature": part.ThoughtSignature}})
					ev.ProviderMetadata = raw
					if s.continuation != nil && part.FunctionCall.ID != "" {
						_ = s.continuation.RememberSignature(s.owner, s.durable, s.thread, part.FunctionCall.ID, part.ThoughtSignature)
					}
				}
				return ev, true, nil
			}
			if part.Text == "" {
				continue
			}
			if part.Thought {
				return protocol.Event{Type: protocol.EventReasoningRawDelta, Text: part.Text}, true, nil
			}
			return protocol.Event{Type: protocol.EventTextDelta, Text: part.Text}, true, nil
		}
	}
}

func claimedModelContent(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null"))
}

func (s *stream) terminal() (protocol.Event, error) {
	if s.emittedTerminal {
		return protocol.Event{}, io.EOF
	}
	s.emittedTerminal = true
	if TruncatedTurn(s.finish, s.tools) {
		return protocol.Event{Type: protocol.EventError, Message: TruncationMessage(s.finish)}, nil
	}
	if !s.sawFrame {
		return protocol.Event{Type: protocol.EventError, Message: "upstream stream ended without a terminal signal — possible truncation"}, nil
	}
	return protocol.Event{Type: protocol.EventDone, StopReason: StopReason(s.finish), Usage: s.usage}, nil
}

func (s *stream) Close() error {
	if s == nil || s.body == nil {
		return nil
	}
	return s.body.Close()
}

func (c *Client) bindContinuation(dispatch providers.DispatchRequest, identity Identity, apiKey string) (*continuation.Bound, error) {
	if c == nil || c.continuation == nil {
		return nil, nil
	}
	bound, err := c.continuation.Bind(continuation.BindRequest{
		Identity: continuation.PhysicalIdentity{
			Provider:    continuationProvider(c.kind),
			Destination: continuationDestination(c),
			Adapter:     continuationProvider(c.kind),
			Model:       identity.PublicID,
			AuthClass:   continuationAuthClass(c),
			Secret:      continuationSecretForKey(c, apiKey),
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

func (c *Client) resolveAccessToken(ctx context.Context) error {
	if c == nil || c.kind != KindVertex || strings.TrimSpace(c.apiKey) != "" || strings.TrimSpace(c.accessToken) != "" {
		return nil
	}
	tok, err := AccessToken(ctx, c.adc)
	if err != nil {
		return err
	}
	c.accessToken = tok
	return nil
}

func (c *Client) Protocol() string {
	if c == nil {
		return "google"
	}
	if c.kind == KindVertex {
		return "google-vertex"
	}
	return "google"
}

func continuationProvider(kind Kind) string {
	if kind == KindVertex {
		return "google-vertex"
	}
	return "google"
}

func continuationAuthClass(c *Client) string {
	if c != nil && strings.TrimSpace(c.apiKey) == "" && strings.TrimSpace(c.accessToken) != "" {
		return "bearer"
	}
	return "api-key"
}

func continuationDestination(c *Client) string {
	if c.kind == KindVertex {
		host, err := ResolveVertexDestination(c.location)
		if err == nil {
			return host
		}
		return VertexAPI
	}
	return AIStudioAPI
}

func continuationSecret(c *Client) []byte {
	return continuationSecretForKey(c, "")
}

func continuationSecretForKey(c *Client, apiKey string) []byte {
	key := strings.TrimSpace(apiKey)
	if key == "" && c != nil {
		key = c.apiKey
	}
	if c != nil && c.kind == KindVertex {
		return []byte(key + "\x00" + c.accessToken + "\x00" + c.project + "\x00" + c.location)
	}
	return []byte(key)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
