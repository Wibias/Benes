package cursor

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/resourcebudget"
	"github.com/Wibias/Benes/internal/responses/continuation"
	"github.com/Wibias/Benes/internal/transport"
)

const runPath = "/agent.v1.AgentService/Run"

type Config struct {
	Endpoint          string
	APIKey            string
	HTTPVersion       HTTPVersion
	HTTPClient        *http.Client
	DestinationPolicy transport.DestinationPolicy
	TransportOptions  transport.ClientOptions
	Continuation      *continuation.Authority
}

type Client struct {
	endpoint     string
	apiKey       string
	httpVersion  HTTPVersion
	httpClient   *http.Client
	checkpoints  *CheckpointStore
	session      *Session
	blobs        *BlobStore
	continuation *continuation.Authority
}

func NewHardened(ctx context.Context, config Config) (*Client, error) {
	version := config.HTTPVersion
	if version == "" {
		version = HTTPVersion2
	}
	endpoint, err := ResolveDestination(config.Endpoint, version)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(config.APIKey) == "" {
		return nil, fmt.Errorf("Cursor access token is required")
	}
	if config.HTTPClient == nil {
		target, err := transport.ResolveTarget(ctx, endpoint, config.DestinationPolicy)
		if err != nil {
			return nil, fmt.Errorf("validate Cursor destination: %w", err)
		}
		options := config.TransportOptions
		if version == HTTPVersion1Dot1 && options.TLSConfig == nil {
			options.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
		}
		config.HTTPClient = transport.NewClient(target, options)
	}
	client := &Client{
		endpoint:     endpoint,
		apiKey:       config.APIKey,
		httpVersion:  version,
		httpClient:   config.HTTPClient,
		checkpoints:  NewCheckpointStore(),
		session:      NewSession(version),
		blobs:        NewBlobStore(),
		continuation: config.Continuation,
	}
	client.checkpoints.BindBlobs(client.blobs)
	return client, nil
}

func (s *stream) blobIDs() [][]byte {
	if s == nil || s.blobs == nil {
		return nil
	}
	return s.blobs.IDs()
}

func applyCheckpointTurns(compiled *RunRequest, payload []byte) {
	if compiled == nil || len(payload) == 0 {
		return
	}
	suffix := toolResultSuffix(compiled.Turns)
	compiled.Turns = append([]RunTurn{{Role: "checkpoint", Text: string(payload)}}, suffix...)
}

func toolResultSuffix(turns []RunTurn) []RunTurn {
	if !lastTurnIsTool(turns) {
		return nil
	}
	start := 0
	for i := len(turns) - 1; i >= 0; i-- {
		if turns[i].Role == "user" {
			start = i
			break
		}
	}
	out := make([]RunTurn, len(turns)-start)
	copy(out, turns[start:])
	return out
}

func (c *Client) Open(ctx context.Context, dispatch providers.DispatchRequest) (providers.EventStream, error) {
	if c == nil {
		return nil, fmt.Errorf("cursor client is required")
	}
	if _, err := PromoteActiveImages(dispatch.Parsed); err != nil {
		return nil, err
	}
	for _, message := range dispatch.Parsed.Context.Messages {
		if message.Role != protocol.RoleToolResult {
			continue
		}
		_ = NormalizeToolResult(joinCursorText(message.Content), nil, message.IsError, false)
	}
	compiled, err := CompileRun(dispatch.Parsed)
	if err != nil {
		return nil, err
	}
	if err := c.blobs.PutConversationTurn(compiled, dispatch.Turn); err != nil {
		return nil, err
	}
	bound, identity := c.bindContinuation(dispatch, compiled)
	if bound != nil {
		defer bound.Release()
		if bound.Hit {
			if payload := bound.ProviderPayload(); len(payload) > 0 {
				applyCheckpointTurns(&compiled, payload)
			}
		} else if cp, ok := c.checkpoints.Lookup(identity, bound.Owner.Credential, compiled.Model, compiled.Digest); ok {
			applyCheckpointTurns(&compiled, cp.Bytes)
		}
	} else if cp, ok := c.checkpoints.Lookup("local", "default", compiled.Model, compiled.Digest); ok {
		applyCheckpointTurns(&compiled, cp.Bytes)
	}
	payload := EncodeRunPayload(compiled)
	frame, err := EncodeConnectFrame(payload, false)
	if err != nil {
		return nil, err
	}
	if dispatch.Turn != nil {
		if _, err := dispatch.Turn.Reserve(resourcebudget.ClassTranslator, int64(len(frame))); err != nil {
			return nil, err
		}
	}
	capture := checkpointCaptureAllowed(dispatch)
	if c.httpVersion == HTTPVersion1Dot1 {
		opened, err := c.openHTTP1(ctx, frame, compiled.Model, compiled.Digest)
		if err != nil {
			return nil, err
		}
		if cs, ok := opened.(*stream); ok {
			cs.capture = capture
			cs.attachContinuation(c, bound, identity)
		}
		return opened, nil
	}
	out, err := c.openHTTP2(ctx, frame, compiled.Model, compiled.Digest, dispatch.Turn)
	if err != nil {
		return nil, err
	}
	out.capture = capture
	out.attachContinuation(c, bound, identity)
	return out, nil
}

func (c *Client) bindContinuation(dispatch providers.DispatchRequest, compiled RunRequest) (*continuation.Bound, string) {
	if c == nil || c.continuation == nil {
		return nil, "local"
	}
	thread := strings.TrimSpace(dispatch.Parsed.PreviousResponseID)
	if thread == "" {
		thread = compiled.Digest
	}
	request := dispatch.Parsed
	if strings.TrimSpace(request.PreviousResponseID) == "" {
		request.PreviousResponseID = thread
	}
	bound, err := c.continuation.Bind(continuation.BindRequest{
		Identity: continuation.PhysicalIdentity{
			Provider:    "cursor",
			Destination: c.endpoint,
			Adapter:     "cursor",
			Model:       compiled.Model,
			AuthClass:   "api-key",
			Secret:      []byte(c.apiKey),
		},
		Thread:              thread,
		Turn:                dispatch.Turn,
		Request:             request,
		StripPreviousOnMiss: true,
	})
	if err != nil {
		return nil, "local"
	}
	return &bound, thread
}

func (s *stream) attachContinuation(c *Client, bound *continuation.Bound, identity string) {
	if s == nil {
		return
	}
	s.identity = identity
	if bound == nil {
		return
	}
	s.owner = bound.Owner
	s.durable = bound.Durable
	s.authority = c.continuation
}

type stream struct {
	body        io.ReadCloser
	buf         []byte
	checkpoints *CheckpointStore
	blobs       *BlobStore
	model       string
	digest      string
	kvReplies   [][]byte
	http        *http.Client
	ctx         context.Context
	endpoint    string
	token       string
	requestID   string
	session     *Session
	writer      pipeWriter
	writeMu     sync.Mutex
	closeMu     sync.Mutex
	terminal    bool
	writeErr    error
	turn        *resourcebudget.Turn
	duplex      bool
	capture     bool
	identity    string
	owner       continuation.Owner
	durable     bool
	authority   *continuation.Authority
}

func (s *stream) Next() (protocol.Event, error) {
	for {
		frames, rem, decErr := DecodeConnectFrames(s.buf)
		if decErr != nil {
			return protocol.Event{}, decErr
		}
		s.buf = rem
		for _, frame := range frames {
			if frame.EndStream {
				s.closeWriter(nil)
				if err := ParseConnectEndStream(frame.Payload); err != nil {
					if s.checkpoints != nil && isInvalidArgument(err) {
						s.checkpoints.Forget(firstNonEmpty(s.identity, "local"), firstNonEmpty(s.owner.Credential, "default"), s.model, s.digest)
					}
					return protocol.Event{}, err
				}
				return protocol.Event{}, io.EOF
			}
			if len(frame.Payload) == 0 {
				continue
			}
			if id, blobID, ok := ParseGetBlobArgs(frame.Payload); ok {
				data, found := s.blobs.Get(blobID)
				if !found {
					return protocol.Event{Type: protocol.EventError, Message: errBlobMissing.Error()}, nil
				}
				reply := EncodeGetBlobResult(id, data)
				if err := s.writeKV(reply); err != nil {
					return protocol.Event{Type: protocol.EventError, Message: err.Error()}, nil
				}
				s.kvReplies = append(s.kvReplies, reply)
				continue
			}
			if cp, ok := CaptureSafeCheckpoint(frame.Payload, firstNonEmpty(s.identity, "local"), firstNonEmpty(s.owner.Credential, "default"), s.model, s.digest, s.capture); ok {
				cp.BlobIDs = s.blobIDs()
				s.checkpoints.Remember(cp)
				if s.authority != nil && s.owner.Credential != "" {
					_ = s.authority.RememberProviderState(s.owner, s.durable, firstNonEmpty(s.identity, s.digest), cp.Bytes)
				}
				continue
			}
			if ev, ok := DecodeServerFrame(frame.Payload); ok {
				return ev, nil
			}
			return protocol.Event{Type: protocol.EventTextDelta, Text: string(frame.Payload)}, nil
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
			s.closeWriter(err)
			return protocol.Event{}, err
		}
	}
}

func (s *stream) Close() error {
	if s == nil {
		return nil
	}
	s.closeWriter(nil)
	if s.body == nil {
		return nil
	}
	return s.body.Close()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func joinCursorText(parts []protocol.ContentPart) string {
	var b strings.Builder
	for _, part := range parts {
		if part.Type == protocol.ContentText {
			b.WriteString(part.Text)
		}
	}
	return b.String()
}
