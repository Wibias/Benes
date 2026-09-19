package providers

import (
	"context"
	"net/http"
	"strings"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/resourcebudget"
)

type EventStream interface {
	Next() (protocol.Event, error)
	Close() error
}

type OpenError struct {
	StatusCode int
	ErrorType  string
	Code       string
	Message    string
	Retryable  *bool
}

func (e *OpenError) Error() string {
	if e == nil {
		return "provider request failed"
	}
	if message := strings.TrimSpace(e.Message); message != "" {
		return message
	}
	return "provider request failed"
}

func (e *OpenError) Event() protocol.Event {
	if e == nil {
		return protocol.Event{Type: protocol.EventError, Message: "provider request failed"}
	}
	return protocol.Event{
		Type:       protocol.EventError,
		Message:    e.Error(),
		Retryable:  e.Retryable,
		HTTPStatus: e.StatusCode,
		ErrorType:  e.ErrorType,
		Code:       e.Code,
	}
}

var forwardHeaderNames = [...]string{
	"authorization",
	"chatgpt-account-id",
	"openai-beta",
	"originator",
	"session_id",
	"session-id",
	"thread-id",
	"user-agent",
	"x-client-request-id",
	"x-codex-beta-features",
	"x-codex-installation-id",
	"x-codex-parent-thread-id",
	"x-codex-turn-metadata",
	"x-codex-turn-state",
	"x-codex-window-id",
	"x-oai-attestation",
	"x-openai-subagent",
	"x-responsesapi-include-timing-metrics",
}

var forwardHeaderSet = func() map[string]struct{} {
	values := make(map[string]struct{}, len(forwardHeaderNames))
	for _, name := range forwardHeaderNames {
		values[name] = struct{}{}
	}
	return values
}()

type ForwardHeaders struct {
	values               map[string]string
	blockedAuthorization bool
}

func NewForwardHeaders(values map[string]string) ForwardHeaders {
	return newForwardHeaders(values, false)
}

func NewForwardHeadersWithBlockedAuthorization(values map[string]string, blockedAuthorization bool) ForwardHeaders {
	return newForwardHeaders(values, blockedAuthorization)
}

func newForwardHeaders(values map[string]string, blockedAuthorization bool) ForwardHeaders {
	filtered := make(map[string]string)
	for name, value := range values {
		normalized := strings.ToLower(name)
		if normalized == "authorization" && blockedAuthorization {
			continue
		}
		if normalized == "user-agent" {
			value = NormalizeUserAgent(value)
		}
		if _, allowed := forwardHeaderSet[normalized]; !allowed || value == "" {
			continue
		}
		filtered[normalized] = value
	}
	if len(filtered) == 0 {
		filtered = nil
	}
	return ForwardHeaders{values: filtered, blockedAuthorization: blockedAuthorization}
}

func (h ForwardHeaders) Get(name string) string {
	return h.values[strings.ToLower(name)]
}

func (h ForwardHeaders) Values() map[string]string {
	if len(h.values) == 0 {
		return nil
	}
	values := make(map[string]string, len(h.values))
	for name, value := range h.values {
		values[name] = value
	}
	return values
}

func (h ForwardHeaders) BlockedAuthorization() bool {
	return h.blockedAuthorization
}

func ForwardHeaderNames() []string {
	names := make([]string, len(forwardHeaderNames))
	copy(names, forwardHeaderNames[:])
	return names
}

// PhysicalPin is the smallest in-process per-turn ownership handle for
// provider-native continuation state: destination/endpoint + credential/account
// ref (with adapter/protocol + model carried on the bound provider). It must not
// contain secrets and must not be persisted into Fabric events/leases.
type PhysicalPin struct {
	Destination   string
	CredentialRef string
	AuthClass     string
}

type DispatchRequest struct {
	Parsed            protocol.ParsedRequest
	ForwardHeaders    ForwardHeaders
	CodexAccountID    string
	Turn              *resourcebudget.Turn
	openCodeGoSession string
	// PreferCommitted marks a post-commit reopen (Fabric continuation / recovery).
	// Provider clients must not hop credential/account/destination after this.
	PreferCommitted bool
	// PhysicalPin, when set, forces the exact credential/account + destination
	// that produced provider-native continuation state on the primary turn.
	PhysicalPin *PhysicalPin
	// ConfiguredServiceTier is the Benes request-policy service tier admitted once for
	// this logical request. Retries, failovers, combos, and continuations copy it through
	// CloneDispatch; the provider boundary must never re-read live settings.
	ConfiguredServiceTier string
}

func WithOpenCodeGoSession(dispatch DispatchRequest, value string) DispatchRequest {
	dispatch.openCodeGoSession = value
	return dispatch
}

func ApplyOpenCodeGoSessionHeader(header http.Header, dispatch DispatchRequest) {
	if header == nil {
		return
	}
	if value := strings.TrimSpace(dispatch.openCodeGoSession); value != "" {
		header.Set("x-opencode-session", value)
	}
}

type Responses interface {
	Open(context.Context, DispatchRequest) (EventStream, error)
}

// ProtocolReporter exposes the physical adapter/protocol that served a turn.
// Fabric continuation gates on this — never on route-name substrings / connection IDs.
type ProtocolReporter interface {
	Protocol() string
}

type ImageRelayKind string

const (
	ImageRelayGenerations ImageRelayKind = "generations"
	ImageRelayEdits       ImageRelayKind = "edits"
)

type ImageRelayRequest struct {
	Kind        ImageRelayKind
	Body        []byte
	ContentType string
}

type ImageRelay interface {
	SupportsImageRelay() bool
	RelayImage(context.Context, DispatchRequest, ImageRelayRequest) (int, http.Header, []byte, error)
}
