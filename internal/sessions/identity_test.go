package sessions

import (
	"net/http"
	"testing"
)

func TestIdentifySameNativeIDSameNamespace(t *testing.T) {
	header := http.Header{}
	header.Set("thread-id", "thread-a")
	first := Identify(IdentifyInput{Protocol: ProtocolResponses, Header: header})
	second := Identify(IdentifyInput{Protocol: ProtocolResponses, Header: header})
	if !first.Groupable() || first.Namespace != NamespaceCodexThread || first.ExternalID != "thread-a" {
		t.Fatalf("first=%#v", first)
	}
	if first != second {
		t.Fatalf("expected identical identity got %#v vs %#v", first, second)
	}
}

func TestIdentifySameExternalIDDifferentNamespaces(t *testing.T) {
	thread := http.Header{}
	thread.Set("thread-id", "shared")
	session := http.Header{}
	session.Set("session-id", "shared")
	a := Identify(IdentifyInput{Protocol: ProtocolResponses, Header: thread})
	b := Identify(IdentifyInput{Protocol: ProtocolChat, Header: session})
	if a.ExternalID != "shared" || b.ExternalID != "shared" {
		t.Fatalf("external ids %#v %#v", a, b)
	}
	if a.Namespace == b.Namespace {
		t.Fatalf("namespaces collided: %#v", a)
	}
}

func TestIdentifyMissingIdentifier(t *testing.T) {
	got := Identify(IdentifyInput{Protocol: ProtocolChat, Header: http.Header{}})
	if got.Groupable() {
		t.Fatalf("fabricated identity %#v", got)
	}
}

func TestIdentifyMalformedIdentifier(t *testing.T) {
	header := http.Header{}
	header.Set("thread-id", "bad\x00id")
	if got := Identify(IdentifyInput{Protocol: ProtocolResponses, Header: header}); got.Groupable() {
		t.Fatalf("control char accepted %#v", got)
	}
	header.Set("thread-id", "")
	if got := Identify(IdentifyInput{Protocol: ProtocolResponses, Header: header}); got.Groupable() {
		t.Fatalf("empty accepted %#v", got)
	}
	header.Set("thread-id", "   ")
	if got := Identify(IdentifyInput{Protocol: ProtocolResponses, Header: header}); got.Groupable() {
		t.Fatalf("whitespace accepted %#v", got)
	}
	header.Set("thread-id", string(make([]rune, maxExternalIDLen+1)))
	if got := Identify(IdentifyInput{Protocol: ProtocolResponses, Header: header}); got.Groupable() {
		t.Fatalf("oversized accepted %#v", got)
	}
}

func TestIdentifyPrefersCodexParentThread(t *testing.T) {
	header := http.Header{}
	header.Set("x-codex-parent-thread-id", "parent")
	header.Set("thread-id", "child")
	header.Set("session-id", "session")
	got := Identify(IdentifyInput{Protocol: ProtocolResponses, Header: header})
	if got.Namespace != NamespaceCodexParentThread || got.ExternalID != "parent" {
		t.Fatalf("got %#v", got)
	}
}

func TestIdentifyDoesNotUseUserOrWindow(t *testing.T) {
	header := http.Header{}
	header.Set("x-codex-window-id", "win")
	header.Set("x-codex-installation-id", "inst")
	got := Identify(IdentifyInput{
		Protocol: ProtocolResponses,
		Header:   header,
		Metadata: map[string]any{"user_id": "user-1", "user": "u"},
	})
	if got.Groupable() {
		t.Fatalf("heuristic identity %#v", got)
	}
}

func TestIdentifyMetadataConversationID(t *testing.T) {
	got := Identify(IdentifyInput{
		Protocol: ProtocolChat,
		Metadata: map[string]any{"conversation_id": "conv-1", "session_id": "sess-1"},
	})
	if got.Namespace != "chat_completions/metadata/conversation_id" || got.ExternalID != "conv-1" {
		t.Fatalf("got %#v", got)
	}
}

func TestIdentifyMetadataIgnoredOnResponsesAndAnthropic(t *testing.T) {
	meta := map[string]any{"conversation_id": "conv-1", "session_id": "sess-1", "thread_id": "th-1"}
	if got := Identify(IdentifyInput{Protocol: ProtocolResponses, Metadata: meta}); got.Groupable() {
		t.Fatalf("responses metadata grouped %#v", got)
	}
	if got := Identify(IdentifyInput{Protocol: ProtocolAnthropic, Metadata: meta}); got.Groupable() {
		t.Fatalf("anthropic metadata grouped %#v", got)
	}
}

func TestIdentifySessionHeaderUsesProtocolNamespace(t *testing.T) {
	header := http.Header{}
	header.Set("session-id", "sess-1")
	got := Identify(IdentifyInput{Protocol: ProtocolAnthropic, Header: header})
	if got.Namespace != ProtocolAnthropic+"/session" || got.ExternalID != "sess-1" {
		t.Fatalf("got %#v", got)
	}
}

func TestIdentifyIgnoresAnthropicUserID(t *testing.T) {
	got := Identify(IdentifyInput{
		Protocol: ProtocolAnthropic,
		Metadata: map[string]any{"user_id": "user-1"},
	})
	if got.Groupable() {
		t.Fatalf("user_id grouped %#v", got)
	}
}

func TestIdentifyGrokInboundHeaders(t *testing.T) {
	header := http.Header{}
	header.Set("x-grok-conv-id", "grok-conv")
	got := Identify(IdentifyInput{Protocol: ProtocolChat, Header: header})
	if got.Namespace != NamespaceGrokConversation || got.ExternalID != "grok-conv" {
		t.Fatalf("got %#v", got)
	}
	responses := Identify(IdentifyInput{Protocol: ProtocolResponses, Header: header})
	if responses.Namespace != NamespaceGrokConversation || responses.ExternalID != "grok-conv" {
		t.Fatalf("responses %#v", responses)
	}
}

func TestIdentifyCodexHeadersAreResponsesOnly(t *testing.T) {
	header := http.Header{}
	header.Set("x-codex-parent-thread-id", "parent")
	header.Set("thread-id", "thread-a")
	for _, protocol := range []string{ProtocolChat, ProtocolAnthropic} {
		got := Identify(IdentifyInput{Protocol: protocol, Header: header})
		if got.Groupable() {
			t.Fatalf("protocol %s accepted Codex identity %#v", protocol, got)
		}
	}
	got := Identify(IdentifyInput{Protocol: ProtocolResponses, Header: header})
	if got.Namespace != NamespaceCodexParentThread || got.ExternalID != "parent" {
		t.Fatalf("responses %#v", got)
	}
}

func TestIdentifySameThreadIDDoesNotJoinChatToCodex(t *testing.T) {
	header := http.Header{}
	header.Set("thread-id", "shared-thread")
	chat := Identify(IdentifyInput{Protocol: ProtocolChat, Header: header})
	anthropic := Identify(IdentifyInput{Protocol: ProtocolAnthropic, Header: header})
	responses := Identify(IdentifyInput{Protocol: ProtocolResponses, Header: header})
	if chat.Groupable() || anthropic.Groupable() {
		t.Fatalf("chat=%#v anthropic=%#v", chat, anthropic)
	}
	if responses.Namespace != NamespaceCodexThread || responses.ExternalID != "shared-thread" {
		t.Fatalf("responses %#v", responses)
	}
}

func TestIdentifyGrokHeadersIgnoredOnAnthropic(t *testing.T) {
	header := http.Header{}
	header.Set("x-grok-conv-id", "grok-conv")
	header.Set("x-grok-session-id", "grok-sess")
	got := Identify(IdentifyInput{Protocol: ProtocolAnthropic, Header: header})
	if got.Groupable() {
		t.Fatalf("anthropic accepted Grok identity %#v", got)
	}
}

func TestUpstreamConversation(t *testing.T) {
	got := UpstreamConversation("kiro", "c-1")
	if got.Namespace != NamespaceKiroConversation || got.ExternalID != "c-1" {
		t.Fatalf("got %#v", got)
	}
	if UpstreamConversation("kiro", "\x01").Groupable() {
		t.Fatal("malformed upstream id")
	}
	if UpstreamConversation("cursor", "c-1").Groupable() {
		t.Fatal("unproven cursor conversationId")
	}
	if UpstreamConversation("unknown", "c-1").Groupable() {
		t.Fatal("unproven state key")
	}
}
