package providers

import (
	"fmt"
	"net/url"
	"strings"
)

func ImageURL(endpoint string, kind ImageRelayKind) (string, error) {
	if kind != ImageRelayGenerations && kind != ImageRelayEdits {
		return "", fmt.Errorf("unsupported image relay kind %q", kind)
	}
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", fmt.Errorf("invalid image upstream endpoint")
	}
	if parsed.User != nil {
		return "", fmt.Errorf("image upstream endpoint credentials are not allowed")
	}
	path := strings.TrimRight(parsed.EscapedPath(), "/")
	switch {
	case strings.HasSuffix(path, "/responses"):
		path = strings.TrimSuffix(path, "/responses") + "/images/" + string(kind)
	case strings.HasSuffix(path, "/chat/completions"):
		path = strings.TrimSuffix(path, "/chat/completions") + "/images/" + string(kind)
	case path == "/v1" || strings.HasSuffix(path, "/v1"):
		path = path + "/images/" + string(kind)
	default:
		return "", fmt.Errorf("cannot derive image endpoint from %q", endpoint)
	}
	parsed.Path = path
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func ImageCapable(endpoint, authClass string) bool {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	path := strings.ToLower(parsed.EscapedPath())
	switch host {
	case "api.openai.com":
		return true
	case "chatgpt.com", "chat.openai.com":
		return strings.Contains(path, "/backend-api/codex")
	default:
		_ = authClass
		return false
	}
}
