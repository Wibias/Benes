package sessions

import (
	"strings"
	"unicode/utf8"
)

const (
	ProtocolResponses = "responses"
	ProtocolChat      = "chat_completions"
	ProtocolAnthropic = "anthropic_messages"

	NamespaceCodexParentThread = "codex/parent-thread"
	NamespaceCodexThread       = "codex/thread"
	NamespaceGrokConversation  = "grok/conversation"
	NamespaceKiroConversation  = "kiro/conversation"
	NamespaceResponsesChain    = "responses/chain"
	NamespaceResponsesPrevious = "responses/previous_response"

	upstreamKiroStateKey = "kiro"

	maxExternalIDLen = 256
)

// Identity is the canonical session key. Namespace prevents collisions when
// different clients reuse the same external identifier.
type Identity struct {
	Namespace  string
	ExternalID string
	Kind       string
}

func (id Identity) Groupable() bool {
	return strings.TrimSpace(id.Namespace) != "" && strings.TrimSpace(id.ExternalID) != ""
}

type HeaderGetter interface {
	Get(name string) string
}

type IdentifyInput struct {
	Protocol string
	Header   HeaderGetter
	Metadata map[string]any
}

// Identify extracts a client- or protocol-supplied conversation identity.
// It never groups by time, model, provider, IP, prompt similarity, or request order.
func Identify(in IdentifyInput) Identity {
	protocol := sanitizeProtocol(in.Protocol)
	if h := in.Header; h != nil {
		if protocol == ProtocolResponses {
			if id := headerIdentity(h.Get("x-codex-parent-thread-id"), NamespaceCodexParentThread, "header_parent_thread"); id.Groupable() {
				return id
			}
			if id := headerIdentity(h.Get("thread-id"), NamespaceCodexThread, "header_thread"); id.Groupable() {
				return id
			}
		}
		if id := headerIdentity(firstNonEmpty(h.Get("session-id"), h.Get("session_id")), protocol+"/session", "header_session"); id.Groupable() {
			return id
		}
		if protocol == ProtocolChat || protocol == ProtocolResponses {
			if id := headerIdentity(firstNonEmpty(h.Get("x-grok-conv-id"), h.Get("x-grok-session-id")), NamespaceGrokConversation, "header_grok"); id.Groupable() {
				return id
			}
		}
	}
	if protocol == ProtocolChat {
		if id := metadataIdentity(protocol, in.Metadata); id.Groupable() {
			return id
		}
	}
	return Identity{}
}

func headerIdentity(raw, namespace, kind string) Identity {
	external := sanitizeExternalID(raw)
	if external == "" || strings.TrimSpace(namespace) == "" {
		return Identity{}
	}
	return Identity{Namespace: namespace, ExternalID: external, Kind: kind}
}

func metadataIdentity(protocol string, metadata map[string]any) Identity {
	if len(metadata) == 0 {
		return Identity{}
	}
	for _, key := range []string{"conversation_id", "session_id", "thread_id"} {
		external := sanitizeExternalID(metadataString(metadata, key))
		if external == "" {
			continue
		}
		return Identity{
			Namespace:  protocol + "/metadata/" + key,
			ExternalID: external,
			Kind:       "metadata_" + key,
		}
	}
	return Identity{}
}

func UpstreamConversation(stateKey, conversationID string) Identity {
	key := strings.ToLower(strings.TrimSpace(stateKey))
	if key != upstreamKiroStateKey {
		return Identity{}
	}
	external := sanitizeExternalID(conversationID)
	if external == "" {
		return Identity{}
	}
	return Identity{
		Namespace:  NamespaceKiroConversation,
		ExternalID: external,
		Kind:       "upstream_conversation",
	}
}

func sanitizeProtocol(raw string) string {
	switch strings.TrimSpace(raw) {
	case ProtocolResponses, ProtocolChat, ProtocolAnthropic:
		return strings.TrimSpace(raw)
	default:
		return "unknown"
	}
}

func sanitizeExternalID(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || utf8.RuneCountInString(trimmed) > maxExternalIDLen {
		return ""
	}
	for _, r := range trimmed {
		if r < 32 || r == 127 {
			return ""
		}
	}
	return trimmed
}

func metadataString(metadata map[string]any, key string) string {
	value, ok := metadata[key]
	if !ok || value == nil {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return text
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
