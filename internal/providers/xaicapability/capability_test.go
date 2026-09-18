package xaicapability

import (
	"net/http"
	"testing"
)

func TestAllowsPriorityOnlyOnCanonicalAPIKeyTransport(t *testing.T) {
	if !AllowsPriority("api-key", "https://api.x.ai/v1/chat/completions") {
		t.Fatal("canonical API-key was denied")
	}
	if AllowsPriority("forward", "https://api.x.ai/v1/chat/completions") {
		t.Fatal("forward auth received Priority")
	}
	if AllowsPriority("api-key", "https://api.x.ai.example/v1") {
		t.Fatal("lookalike host received Priority")
	}
	if AllowsPriority("api-key", "https://api.x.ai/v1?hijack=1") {
		t.Fatal("query-mutated URL received Priority")
	}
}

func TestApplyPriorityFailsClosedOffCanonicalTransport(t *testing.T) {
	tier := "priority"
	if _, err := ApplyPriority(&tier, "api-key", "https://api.x.ai.example/v1"); err == nil {
		t.Fatal("noncanonical Priority was allowed")
	}
	got, err := ApplyPriority(&tier, "api-key", "https://api.x.ai/v1")
	if err != nil || got == nil || *got != "priority" {
		t.Fatalf("canonical=%v %v", got, err)
	}
	if AllowsPriority("api-key", OAuthChatEndpoint()) {
		t.Fatal("OAuth proxy received API-key Priority")
	}
}

func TestUsesOAuthProxySeparatesOAuthModeFromAPIKeyMaterial(t *testing.T) {
	if !UsesOAuthProxy("oauth", true, true) {
		t.Fatal("oauth mode did not select CLI proxy")
	}
	if UsesOAuthProxy("key", true, false) {
		t.Fatal("API key selected CLI proxy")
	}
	if UsesOAuthProxy("", false, true) {
		t.Fatal("stored API-key credentialRef selected CLI proxy")
	}
	if !UsesOAuthProxy("", false, false) {
		t.Fatal("xAI oauth-only slot did not select CLI proxy")
	}
}

func TestApplyCLIHeadersStayOnOAuthProxyIdentity(t *testing.T) {
	if !IsOAuthProxyEndpoint(OAuthChatEndpoint()) || IsOAuthProxyEndpoint(CanonicalAPI+"/chat/completions") {
		t.Fatal("proxy endpoint identity")
	}
	header := make(http.Header)
	ApplyCLIHeaders(header)
	if header.Get("x-xai-token-auth") != CLITokenAuth ||
		header.Get("x-grok-client-identifier") != CLIClientID ||
		header.Get("x-authenticateresponse") != CLIAuthenticateResponse ||
		header.Get("x-grok-req-id") == "" ||
		header.Get("x-grok-conv-id") == "" ||
		header.Get("x-grok-session-id") == "" {
		t.Fatalf("headers=%v", header)
	}
	if header.Get("x-grok-conv-id") != header.Get("x-grok-session-id") {
		t.Fatalf("conv/session mismatch: %v", header)
	}
	ApplyCLIHeaders(header)
	if header.Get("x-grok-req-id") == "" {
		t.Fatal("retry identity was dropped")
	}
}

func TestApplyPriorityFailsClosedOnOAuthProxy(t *testing.T) {
	tier := "priority"
	if _, err := ApplyPriority(&tier, AuthClassAPIKey, OAuthChatEndpoint()); err == nil {
		t.Fatal("OAuth proxy accepted API-key Priority")
	}
}
