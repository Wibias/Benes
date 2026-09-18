package codexrouting

import (
	"encoding/json"
	"net"
	"net/url"
	"regexp"
	"strings"
)

const Marker = "# Auto-injected by benes"

type Kind string

const (
	KindNative       Kind = "native"
	KindBenesLocal   Kind = "benes-local"
	KindCustomLocal  Kind = "custom-local"
	KindCustomRemote Kind = "custom-remote"
	KindUnknown      Kind = "unknown"
)

func Classify(content string) Kind {
	if base := RootTomlString(content, "openai_base_url"); base != "" {
		endpoint := classifyEndpoint(base)
		if endpoint == "unknown" {
			return KindUnknown
		}
		if HasInjectedBaseURL(content) {
			return KindBenesLocal
		}
		if endpoint == "local" {
			return KindCustomLocal
		}
		return KindCustomRemote
	}
	if provider := RootTomlString(content, "model_provider"); provider != "" {
		if base := ProviderTableString(content, provider, "base_url"); base != "" {
			endpoint := classifyEndpoint(base)
			if endpoint == "unknown" {
				return KindUnknown
			}
			if provider == "benes" {
				return KindBenesLocal
			}
			if endpoint == "local" {
				return KindCustomLocal
			}
			return KindCustomRemote
		}
		if provider == "benes" || ProviderTableExists(content, provider) || provider != "openai" {
			return KindUnknown
		}
	}
	return KindNative
}

func PublicRoute(kind Kind) string {
	switch kind {
	case KindBenesLocal:
		return "benes"
	case KindNative:
		return "native"
	case KindCustomLocal, KindCustomRemote:
		return "other"
	default:
		return "unknown"
	}
}

func EffectiveBaseURL(content string) string {
	if base := RootTomlString(content, "openai_base_url"); base != "" {
		return base
	}
	if provider := RootTomlString(content, "model_provider"); provider != "" {
		return ProviderTableString(content, provider, "base_url")
	}
	return ""
}

func HasInjectedBaseURL(content string) bool {
	lines := strings.Split(content, "\n")
	rootEnd := len(lines)
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "[") {
			rootEnd = i
			break
		}
	}
	for i := 1; i < rootEnd; i++ {
		if isRootBaseURLLine(lines[i]) && strings.Contains(lines[i-1], Marker) {
			return true
		}
	}
	return false
}

func isRootBaseURLLine(line string) bool {
	return rootBaseURLLine.MatchString(line)
}

var rootBaseURLLine = regexp.MustCompile(`^\s*openai_base_url\s*=`)

func classifyEndpoint(value string) string {
	parsed, err := url.Parse(value)
	if err != nil {
		return "unknown"
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "unknown"
	}
	host := strings.ToLower(parsed.Hostname())
	host = strings.TrimPrefix(host, "[")
	host = strings.TrimSuffix(host, "]")
	host = strings.TrimSuffix(host, ".")
	if host == "" {
		return "unknown"
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return "local"
	}
	if host == "::" || host == "::1" || host == "0.0.0.0" {
		return "local"
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() || ip.IsUnspecified() {
			return "local"
		}
		if ip.To4() != nil {
			return "remote"
		}
		if strings.HasPrefix(host, "::ffff:") {
			return "unknown"
		}
		return "remote"
	}
	if strings.HasPrefix(host, "::ffff:") {
		return "unknown"
	}
	return "remote"
}

var rootTomlAssign = regexp.MustCompile(`(?m)^\s*([A-Za-z0-9_]+)\s*=\s*("(?:\\.|[^"])*"|'[^']*')`)

func RootTomlString(content, key string) string {
	end := strings.Index(content, "[")
	root := content
	if end >= 0 {
		root = content[:end]
	}
	for _, match := range rootTomlAssign.FindAllStringSubmatch(root, -1) {
		if match[1] != key {
			continue
		}
		raw := match[2]
		if strings.HasPrefix(raw, `"`) {
			var value string
			if json.Unmarshal([]byte(raw), &value) == nil {
				return strings.TrimSpace(value)
			}
		}
		return strings.TrimSpace(strings.Trim(raw, "'"))
	}
	return ""
}

func ProviderTableExists(content, provider string) bool {
	header := "[model_providers." + provider + "]"
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSpace(line) == header {
			return true
		}
	}
	return false
}

func ProviderTableString(content, provider, key string) string {
	header := "[model_providers." + provider + "]"
	lines := strings.Split(content, "\n")
	in := false
	body := ""
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "[") {
			if in {
				break
			}
			in = trim == header
			continue
		}
		if in {
			body += line + "\n"
		}
	}
	if !in && body == "" {
		return ""
	}
	return RootTomlString(body, key)
}
