package zcode

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

const placeholderKey = "benes-zcode"

func ProviderFragment(origin string) (map[string]any, error) {
	parsed, err := url.Parse(strings.TrimSpace(origin))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("ZCode proxy origin is invalid")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("ZCode proxy origin must be http or https")
	}
	host := parsed.Hostname()
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return nil, fmt.Errorf("ZCode integration is loopback-only")
	}
	base := strings.TrimRight(parsed.String(), "/") + "/v1"
	return map[string]any{
		"kind":    "openai-compatible",
		"baseURL": base,
		"apiKey":  placeholderKey,
	}, nil
}
