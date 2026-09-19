package openaichat

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Wibias/Benes/internal/capability"
	"github.com/Wibias/Benes/internal/credentials"
	"github.com/Wibias/Benes/internal/gatewayrouting"
	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/providers/xaicapability"
	"github.com/Wibias/Benes/internal/resourcebudget"
	"github.com/Wibias/Benes/internal/responses/sse"
	"github.com/Wibias/Benes/internal/transport"
)

type Config struct {
	Endpoint                           string
	APIKey                             string
	HTTPClient                         *http.Client
	CompileOptions                     CompileOptions
	PreserveReasoningContentModels     []string
	RequiresReasoningPlaceholderModels []string
	ReasoningSplitModels               []string
	MaxStreamBytes                     int64
	InactivityTimeout                  time.Duration
	MaxSSELineBytes                    int
	MaxSSEEventBytes                   int
	DestinationPolicy                  transport.DestinationPolicy
	TransportOptions                   transport.ClientOptions
	Credentials                        credentials.Store
	CredentialRef                      credentials.Ref
	Capability                         capability.Policy
	ProviderID                         string
	GatewayRouting                     gatewayrouting.Settings
	Transient5xx                       transport.Transient5xxPolicy
	UserAgent                          string
}

type Client struct {
	endpoint                           string
	apiKey                             string
	httpClient                         *http.Client
	compileOptions                     CompileOptions
	preserveReasoningContentModels     []string
	requiresReasoningPlaceholderModels []string
	reasoningSplitModels               []string
	maxStreamBytes                     int64
	inactivityTimeout                  time.Duration
	sseLimits                          sse.Limits
	capability                         capability.Policy
	providerID                         string
	gatewayRouting                     gatewayrouting.Settings
	transient5xx                       transport.Transient5xxPolicy
	userAgent                          string
}

func New(config Config) (*Client, error) {
	if strings.TrimSpace(config.Endpoint) == "" {
		config.Endpoint = "https://api.openai.com/v1/chat/completions"
	}
	parsed, err := url.Parse(config.Endpoint)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
		return nil, fmt.Errorf("invalid OpenAI Chat endpoint")
	}
	if parsed.User != nil {
		return nil, fmt.Errorf("OpenAI Chat endpoint credentials are not allowed")
	}
	if config.Credentials != nil && (strings.TrimSpace(config.CredentialRef.ID) != "" || config.CredentialRef.Source != "") {
		secret, err := config.Credentials.Get(config.CredentialRef)
		if err != nil {
			return nil, fmt.Errorf("resolve OpenAI Chat credential: %w", err)
		}
		config.APIKey = string(secret)
	}
	if strings.TrimSpace(config.APIKey) == "" {
		return nil, fmt.Errorf("OpenAI Chat API key is required")
	}
	if strings.TrimSpace(config.Capability.Endpoint) == "" {
		config.Capability.Endpoint = config.Endpoint
	}
	if strings.TrimSpace(config.Capability.Protocol) == "" {
		config.Capability.Protocol = "openai-chat"
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
	return &Client{
		endpoint:                           config.Endpoint,
		apiKey:                             config.APIKey,
		httpClient:                         config.HTTPClient,
		compileOptions:                     config.CompileOptions,
		preserveReasoningContentModels:     cloneOptionalStrings(config.PreserveReasoningContentModels),
		requiresReasoningPlaceholderModels: cloneOptionalStrings(config.RequiresReasoningPlaceholderModels),
		reasoningSplitModels:               cloneOptionalStrings(config.ReasoningSplitModels),
		maxStreamBytes:                     config.MaxStreamBytes,
		inactivityTimeout:                  config.InactivityTimeout,
		sseLimits: sse.Limits{
			MaxLineBytes:  config.MaxSSELineBytes,
			MaxEventBytes: config.MaxSSEEventBytes,
		},
		capability:     config.Capability,
		providerID:     config.ProviderID,
		gatewayRouting: config.GatewayRouting.Clone(),
		transient5xx:   config.Transient5xx,
		userAgent:      providers.NormalizeUserAgent(config.UserAgent),
	}, nil
}

func NewHardened(ctx context.Context, config Config) (*Client, error) {
	endpoint := strings.TrimSpace(config.Endpoint)
	if endpoint == "" {
		endpoint = "https://api.openai.com/v1/chat/completions"
		config.Endpoint = endpoint
	}
	target, err := transport.ResolveTarget(ctx, endpoint, config.DestinationPolicy)
	if err != nil {
		return nil, fmt.Errorf("validate OpenAI Chat destination: %w", err)
	}
	config.HTTPClient = transport.NewClient(target, config.TransportOptions)
	return New(config)
}

func (c *Client) Open(ctx context.Context, dispatch providers.DispatchRequest) (providers.EventStream, error) {
	request := dispatch.Parsed
	if err := capability.Apply(&request, c.capability, dispatch.ConfiguredServiceTier); err != nil {
		return nil, err
	}
	upstreamRequest := request
	upstreamRequest.Stream = true
	compileOptions := c.compileOptions
	compileOptions.BlankAssistantContentWithTools = clinePassDeepSeekV4(c.providerID, upstreamRequest.UpstreamModelID)
	if c.preserveReasoningContentModels != nil {
		compileOptions.PreserveReasoningContent = modelMatchesConfiguredFamily(c.preserveReasoningContentModels, upstreamRequest.UpstreamModelID)
	}
	if c.reasoningSplitModels != nil {
		compileOptions.ReasoningSplit = modelMatchesConfiguredFamily(c.reasoningSplitModels, upstreamRequest.UpstreamModelID)
	}
	placeholderModels := c.requiresReasoningPlaceholderModels
	if placeholderModels == nil {
		placeholderModels = c.preserveReasoningContentModels
	}
	if compileOptions.PreserveReasoningContent && modelMatchesConfiguredFamily(placeholderModels, upstreamRequest.UpstreamModelID) {
		upstreamRequest = withRequiredReasoningPlaceholders(upstreamRequest)
	}
	body, err := Compile(upstreamRequest, compileOptions)
	if err != nil {
		return nil, err
	}
	policy := c.gatewayRouting.Resolve(upstreamRequest.UpstreamModelID)
	body, err = gatewayrouting.ApplyVercelChat(body, c.endpoint, policy)
	if err != nil {
		return nil, err
	}
	applied := !policy.Empty() && gatewayrouting.CanonicalVercel(c.endpoint)
	gatewayrouting.RecordRequested(ctx, policy, applied)
	if err := reserveTurnBytes(dispatch.Turn, resourcebudget.ClassTranslator, int64(len(body))); err != nil {
		return nil, err
	}
	if err := reserveRequestBlobs(dispatch.Turn, request.Context); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build OpenAI Chat request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	providers.ApplyUserAgent(req.Header, c.userAgent, dispatch.ForwardHeaders)
	if xaicapability.IsOAuthProxyEndpoint(c.endpoint) {
		xaicapability.ApplyCLIHeaders(req.Header)
	}
	providers.ApplyOpenCodeGoSessionHeader(req.Header, dispatch)
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}
	req.ContentLength = int64(len(body))

	response, err := transport.DoTransient5xxForTurn(ctx, c.httpClient, req, c.transient5xx, dispatch.Turn, "openai-chat")
	if err != nil {
		return nil, fmt.Errorf("OpenAI Chat request failed: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_ = response.Body.Close()
		return nil, fmt.Errorf("OpenAI Chat upstream returned HTTP %d", response.StatusCode)
	}
	mediaType, _, mediaErr := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if mediaErr != nil || mediaType != "text/event-stream" {
		_ = response.Body.Close()
		return nil, fmt.Errorf("OpenAI Chat upstream must return text/event-stream")
	}

	pipeReader, pipeWriter := io.Pipe()
	copyCtx, cancel := context.WithCancel(ctx)
	go func() {
		_, copyErr := transport.CopyBounded(copyCtx, pipeWriter, response.Body, transport.StreamLimits{
			MaxBytes:          c.maxStreamBytes,
			InactivityTimeout: c.inactivityTimeout,
		})
		_ = pipeWriter.CloseWithError(copyErr)
	}()
	parser := NewStream(pipeReader, c.sseLimits)
	parser.turn = dispatch.Turn
	return &clientStream{
		parser: parser,
		reader: pipeReader,
		cancel: cancel,
	}, nil
}

func clinePassDeepSeekV4(providerID, modelID string) bool {
	if strings.TrimSpace(providerID) != "cline-pass" {
		return false
	}
	switch strings.TrimSpace(modelID) {
	case "deepseek-v4-flash", "deepseek-v4-pro":
		return true
	default:
		return false
	}
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

func cloneOptionalStrings(values []string) []string {
	if values == nil {
		return nil
	}
	cloned := make([]string, len(values))
	copy(cloned, values)
	return cloned
}

type clientStream struct {
	parser *Stream
	reader *io.PipeReader
	cancel context.CancelFunc
	once   sync.Once
}

func (s *clientStream) Next() (protocol.Event, error) {
	return s.parser.Next()
}

func (s *clientStream) Close() error {
	var closeErr error
	s.once.Do(func() {
		if s.parser != nil && s.parser.toolArgs != nil {
			_ = s.parser.toolArgs.Close()
		}
		s.cancel()
		closeErr = s.reader.Close()
	})
	return closeErr
}
