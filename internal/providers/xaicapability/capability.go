package xaicapability

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const (
	CanonicalAPI            = "https://api.x.ai/v1"
	CanonicalOAuthAPI       = "https://cli-chat-proxy.grok.com/v1"
	CLIClientVersion        = "0.2.93"
	CLIClientID             = "grok-shell"
	CLITokenAuth            = "xai-grok-cli"
	CLIAuthenticateResponse = "authenticate-response"
	oauthProxyHost          = "cli-chat-proxy.grok.com"
	AuthClassOAuth          = "oauth"
	AuthClassAPIKey         = "api-key"
)

func UsesOAuthProxy(authMode string, hasAPIKey, hasCredentialRef bool) bool {
	if strings.EqualFold(strings.TrimSpace(authMode), AuthClassOAuth) {
		return true
	}
	return !hasAPIKey && !hasCredentialRef
}

func AuthClass(usesOAuth bool) string {
	if usesOAuth {
		return AuthClassOAuth
	}
	return AuthClassAPIKey
}

func OAuthChatEndpoint() string {
	return strings.TrimRight(CanonicalOAuthAPI, "/") + "/chat/completions"
}

func OAuthResponsesEndpoint() string {
	return strings.TrimRight(CanonicalOAuthAPI, "/") + "/responses"
}

func IsOAuthProxyEndpoint(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	return strings.EqualFold(parsed.Hostname(), oauthProxyHost)
}

func ApplyCLIHeaders(header http.Header) {
	if header == nil {
		return
	}
	header.Set("x-xai-token-auth", CLITokenAuth)
	header.Set("x-authenticateresponse", CLIAuthenticateResponse)
	header.Set("x-grok-client-version", CLIClientVersion)
	header.Set("x-grok-client-identifier", CLIClientID)
	header.Set("User-Agent", "xai-grok-cli")
	ApplyRequestIdentity(header)
}

func ApplyRequestIdentity(header http.Header) {
	if header == nil {
		return
	}
	if header.Get("x-grok-req-id") == "" {
		header.Set("x-grok-req-id", newGrokID())
	}
	if header.Get("x-grok-conv-id") == "" {
		conv := newGrokID()
		header.Set("x-grok-conv-id", conv)
		if header.Get("x-grok-session-id") == "" {
			header.Set("x-grok-session-id", conv)
		}
	} else if header.Get("x-grok-session-id") == "" {
		header.Set("x-grok-session-id", header.Get("x-grok-conv-id"))
	}
}

func newGrokID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "benes-grok-id"
	}
	return hex.EncodeToString(buf[:])
}

func AllowsPriority(authClass, endpoint string) bool {
	if !strings.EqualFold(strings.TrimSpace(authClass), "api-key") {
		return false
	}
	return canonicalXAIEndpoint(endpoint)
}

func ApplyPriority(serviceTier *string, authClass, endpoint string) (*string, error) {
	if serviceTier == nil || !strings.EqualFold(strings.TrimSpace(*serviceTier), "priority") {
		return serviceTier, nil
	}
	if !xaiHost(endpoint) {
		return serviceTier, nil
	}
	if AllowsPriority(authClass, endpoint) {
		value := "priority"
		return &value, nil
	}
	return nil, fmt.Errorf("xAI Priority requires the canonical API-key transport")
}

func xaiHost(raw string) bool {
	if IsOAuthProxyEndpoint(raw) {
		return true
	}
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return strings.Contains(strings.ToLower(raw), "x.ai")
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "api.x.ai" || strings.HasSuffix(host, ".x.ai") || strings.Contains(host, "x.ai")
}

func canonicalXAIEndpoint(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	if !strings.EqualFold(parsed.Scheme, "https") || !strings.EqualFold(parsed.Hostname(), "api.x.ai") {
		return false
	}
	if port := parsed.Port(); port != "" && port != "443" {
		return false
	}
	path := strings.TrimRight(parsed.EscapedPath(), "/")
	return path == "/v1" || strings.HasPrefix(path, "/v1/")
}
