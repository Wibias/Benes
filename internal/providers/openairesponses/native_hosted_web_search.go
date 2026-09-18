package openairesponses

import (
	"net/url"
	"strings"
)

func (c *Client) SupportsNativeHostedWebSearch(string) bool {
	return c != nil && nativeHostedWebSearchDestination(c.endpoint)
}

func (c *ForwardClient) SupportsNativeHostedWebSearch(string) bool {
	return c != nil && nativeHostedWebSearchDestination(c.endpoint)
}

func SupportsNativeHostedWebSearchDestination(destination string) bool {
	return nativeHostedWebSearchDestination(destination)
}

func nativeHostedWebSearchDestination(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Port() != "" {
		return false
	}
	if !strings.EqualFold(parsed.Scheme, "https") {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	path := strings.TrimRight(parsed.EscapedPath(), "/")
	switch host {
	case "api.openai.com":
		return path == "/v1/responses"
	case "chatgpt.com":
		return path == "/backend-api/codex/responses"
	case "opencode.ai":
		return path == "/zen/go/v1/responses"
	default:
		return false
	}
}
