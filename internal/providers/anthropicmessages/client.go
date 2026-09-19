package anthropicmessages

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

	"github.com/Wibias/Benes/internal/credentials"
	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/resourcebudget"
	"github.com/Wibias/Benes/internal/responses/sse"
	"github.com/Wibias/Benes/internal/transport"
)

const AnthropicVersion = "2023-06-01"

type KeyTransport string

const (
	KeyTransportXAPIKey KeyTransport = "x-api-key"
	KeyTransportBearer  KeyTransport = "bearer"
)

type Config struct {
	Endpoint          string
	APIKey            string
	APIKeyTransport   KeyTransport
	HTTPClient        *http.Client
	MaxStreamBytes    int64
	InactivityTimeout time.Duration
	MaxSSELineBytes   int
	MaxSSEEventBytes  int
	DestinationPolicy transport.DestinationPolicy
	TransportOptions  transport.ClientOptions
	Credentials       credentials.Store
	CredentialRef     credentials.Ref
	ProviderID        string
	Transient5xx      transport.Transient5xxPolicy
}

type Client struct {
	endpoint          string
	apiKey            string
	apiKeyTransport   KeyTransport
	httpClient        *http.Client
	maxStreamBytes    int64
	inactivityTimeout time.Duration
	sseLimits         sse.Limits
	providerID        string
	transient5xx      transport.Transient5xxPolicy
}

func New(config Config) (*Client, error) {
	if strings.TrimSpace(config.Endpoint) == "" {
		config.Endpoint = "https://api.anthropic.com/v1/messages"
	}
	parsed, err := url.Parse(config.Endpoint)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
		return nil, fmt.Errorf("invalid Anthropic Messages endpoint")
	}
	if parsed.User != nil {
		return nil, fmt.Errorf("Anthropic Messages endpoint credentials are not allowed")
	}
	if config.Credentials != nil && (strings.TrimSpace(config.CredentialRef.ID) != "" || config.CredentialRef.Source != "") {
		secret, err := config.Credentials.Get(config.CredentialRef)
		if err != nil {
			return nil, fmt.Errorf("resolve Anthropic Messages credential: %w", err)
		}
		config.APIKey = string(secret)
	}
	config.APIKey = strings.TrimSpace(config.APIKey)
	if config.APIKey == "" {
		return nil, fmt.Errorf("Anthropic Messages API key is required")
	}
	if config.APIKeyTransport == "" {
		config.APIKeyTransport = KeyTransportXAPIKey
	}
	switch config.APIKeyTransport {
	case KeyTransportXAPIKey, KeyTransportBearer:
	default:
		return nil, fmt.Errorf("unsupported Anthropic Messages API key transport")
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
		endpoint:          config.Endpoint,
		apiKey:            config.APIKey,
		apiKeyTransport:   config.APIKeyTransport,
		httpClient:        config.HTTPClient,
		maxStreamBytes:    config.MaxStreamBytes,
		inactivityTimeout: config.InactivityTimeout,
		sseLimits: sse.Limits{
			MaxLineBytes:  config.MaxSSELineBytes,
			MaxEventBytes: config.MaxSSEEventBytes,
		},
		providerID:   strings.TrimSpace(config.ProviderID),
		transient5xx: config.Transient5xx,
	}, nil
}

func NewHardened(ctx context.Context, config Config) (*Client, error) {
	endpoint := strings.TrimSpace(config.Endpoint)
	if endpoint == "" {
		endpoint = "https://api.anthropic.com/v1/messages"
		config.Endpoint = endpoint
	}
	target, err := transport.ResolveTarget(ctx, endpoint, config.DestinationPolicy)
	if err != nil {
		return nil, fmt.Errorf("validate Anthropic Messages destination: %w", err)
	}
	config.HTTPClient = transport.NewClient(target, config.TransportOptions)
	return New(config)
}

func (c *Client) Open(ctx context.Context, dispatch providers.DispatchRequest) (providers.EventStream, error) {
	request := dispatch.Parsed
	request.Stream = true
	body, err := Compile(request)
	if err != nil {
		return nil, err
	}
	if err := reserveTurnBytes(dispatch.Turn, resourcebudget.ClassTranslator, int64(len(body))); err != nil {
		return nil, err
	}
	if err := reserveRequestBlobs(dispatch.Turn, request.Context); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build Anthropic Messages request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("anthropic-version", AnthropicVersion)
	switch c.apiKeyTransport {
	case KeyTransportBearer:
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	case KeyTransportXAPIKey:
		req.Header.Set("x-api-key", c.apiKey)
	}
	providers.ApplyOpenCodeGoSessionHeader(req.Header, dispatch)
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}
	req.ContentLength = int64(len(body))

	response, err := transport.DoTransient5xxForTurn(ctx, c.httpClient, req, c.transient5xx, dispatch.Turn, "anthropic-messages")
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("Anthropic Messages request failed: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		_ = response.Body.Close()
		return nil, fmt.Errorf("Anthropic Messages upstream returned HTTP %d", response.StatusCode)
	}
	mediaType, _, mediaErr := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if mediaErr != nil || mediaType != "text/event-stream" {
		_ = response.Body.Close()
		return nil, fmt.Errorf("Anthropic Messages upstream must return text/event-stream")
	}

	pipeReader, pipeWriter := io.Pipe()
	copyCtx, cancel := context.WithCancel(ctx)
	go func() {
		defer response.Body.Close()
		_, copyErr := transport.CopyBounded(copyCtx, pipeWriter, response.Body, transport.StreamLimits{
			MaxBytes:          c.maxStreamBytes,
			InactivityTimeout: c.inactivityTimeout,
		})
		_ = pipeWriter.CloseWithError(copyErr)
	}()
	parser := NewStream(pipeReader, c.sseLimits)
	parser.turn = dispatch.Turn
	return &clientStream{parser: parser, reader: pipeReader, cancel: cancel}, nil
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
