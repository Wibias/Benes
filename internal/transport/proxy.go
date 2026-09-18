package transport

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

type ProxyMode string

const (
	ProxyModeDirect      ProxyMode = "direct"
	ProxyModeFixed       ProxyMode = "fixed"
	ProxyModeEnvironment ProxyMode = "environment"
	ProxyModeSystem      ProxyMode = "system"
)

type ProxyPolicy struct {
	Mode    ProxyMode
	URL     string
	NoProxy []string

	once     sync.Once
	fixedURL *url.URL
	fixedErr error
	system   systemProxySnapshot

	systemMu     sync.Mutex
	systemAt     time.Time
	systemOK     bool
	lookupSystem func() (string, []string, error)
	now          func() time.Time
	systemTTL    time.Duration
}

type systemProxySnapshot struct {
	URL     string
	NoProxy []string
	Err     error
}

func DefaultNoProxy() []string {
	return []string{"localhost", "127.0.0.1", "::1", ".localhost"}
}
func PolicyFromConfig(rawURL, rawNoProxy string) *ProxyPolicy {
	rawURL = strings.TrimSpace(rawURL)
	policy := &ProxyPolicy{
		Mode:    ProxyModeEnvironment,
		NoProxy: mergeNoProxy(DefaultNoProxy(), splitNoProxy(rawNoProxy), envNoProxy()),
	}
	if rawURL == "" {
		return policy
	}
	if envRefName(rawURL) != "" {
		return policy
	}
	if strings.EqualFold(rawURL, "system") || strings.EqualFold(rawURL, "auto") {
		policy.Mode = ProxyModeSystem
		return policy
	}
	policy.Mode = ProxyModeFixed
	policy.URL = rawURL
	return policy
}

func (p *ProxyPolicy) Func() ProxyFunc {
	if p == nil {
		return nil
	}
	return p.Select
}

func (p *ProxyPolicy) Select(request *http.Request) (*url.URL, error) {
	if p == nil || request == nil || request.URL == nil {
		return nil, nil
	}
	mode := p.Mode
	if mode == "" {
		mode = ProxyModeEnvironment
	}
	if MatchNoProxy(request.URL, p.noProxyEntries()) {
		return nil, nil
	}
	switch mode {
	case ProxyModeDirect:
		return nil, nil
	case ProxyModeFixed:
		return p.fixed()
	case ProxyModeSystem:
		snap := p.refreshSystem()
		if snap.Err != nil || strings.TrimSpace(snap.URL) == "" {
			return http.ProxyFromEnvironment(request)
		}
		if MatchNoProxy(request.URL, mergeNoProxy(p.noProxyEntries(), snap.NoProxy)) {
			return nil, nil
		}
		return parseProxyURL(snap.URL)
	default:
		return http.ProxyFromEnvironment(request)
	}
}

func (p *ProxyPolicy) fixed() (*url.URL, error) {
	p.once.Do(func() {
		p.fixedURL, p.fixedErr = parseProxyURL(p.URL)
	})
	return p.fixedURL, p.fixedErr
}

func (p *ProxyPolicy) refreshSystem() systemProxySnapshot {
	p.systemMu.Lock()
	defer p.systemMu.Unlock()
	now := time.Now()
	if p.now != nil {
		now = p.now()
	}
	ttl := p.systemTTL
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	if p.systemOK && now.Sub(p.systemAt) < ttl {
		return p.system
	}
	lookup := lookupSystemProxy
	if p.lookupSystem != nil {
		lookup = p.lookupSystem
	}
	raw, extra, err := lookup()
	if err != nil {
		if p.systemOK {
			return p.system
		}
		p.system = systemProxySnapshot{Err: err}
		p.systemAt = now
		return p.system
	}
	p.system = systemProxySnapshot{URL: raw, NoProxy: extra}
	p.systemAt = now
	p.systemOK = true
	return p.system
}

func (p *ProxyPolicy) noProxyEntries() []string {
	if p == nil {
		return DefaultNoProxy()
	}
	if len(p.NoProxy) == 0 {
		return mergeNoProxy(DefaultNoProxy(), envNoProxy())
	}
	return p.NoProxy
}

func parseProxyURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https" && parsed.Scheme != "socks5") {
		return nil, fmt.Errorf("invalid proxy url")
	}
	if parsed.User != nil {
		if _, has := parsed.User.Password(); has {
			// Keep userinfo for CONNECT, but never require callers to log it.
		}
	}
	return parsed, nil
}

func MatchNoProxy(target *url.URL, entries []string) bool {
	if target == nil {
		return false
	}
	hostname := normalizeProxyHostname(target.Hostname())
	if hostname == "" {
		return false
	}
	port := target.Port()
	if port == "" {
		if target.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	if isLoopbackHost(hostname) {
		return true
	}
	for _, raw := range entries {
		entry := strings.TrimSpace(strings.ToLower(raw))
		if entry == "" {
			continue
		}
		if entry == "*" {
			return true
		}
		entry = strings.TrimPrefix(strings.TrimPrefix(entry, "https://"), "http://")
		if slash := strings.IndexByte(entry, '/'); slash >= 0 {
			entry = entry[:slash]
		}
		entryHost, entryPort := splitHostPort(entry)
		if entryPort != "" && entryPort != port {
			continue
		}
		entryHost = strings.TrimPrefix(entryHost, "*.")
		entryHost = strings.TrimPrefix(entryHost, ".")
		entryHost = normalizeProxyHostname(entryHost)
		if entryHost == "" {
			continue
		}
		if hostname == entryHost || strings.HasSuffix(hostname, "."+entryHost) {
			return true
		}
	}
	return false
}

func splitHostPort(entry string) (string, string) {
	if strings.HasPrefix(entry, "[") {
		close := strings.IndexByte(entry, ']')
		if close > 0 {
			host := entry[1:close]
			rest := entry[close+1:]
			if strings.HasPrefix(rest, ":") {
				return host, rest[1:]
			}
			return host, ""
		}
	}
	if strings.Count(entry, ":") == 1 {
		host, port, err := net.SplitHostPort(entry)
		if err == nil {
			return host, port
		}
	}
	return entry, ""
}

func normalizeProxyHostname(hostname string) string {
	hostname = strings.TrimSpace(strings.ToLower(hostname))
	hostname = strings.TrimSuffix(hostname, ".")
	if strings.HasPrefix(hostname, "[") && strings.HasSuffix(hostname, "]") {
		hostname = hostname[1 : len(hostname)-1]
	}
	return hostname
}

func isLoopbackHost(hostname string) bool {
	switch hostname {
	case "localhost", "127.0.0.1", "::1", "0:0:0:0:0:0:0:1":
		return true
	}
	if ip := net.ParseIP(hostname); ip != nil {
		return ip.IsLoopback()
	}
	return strings.HasSuffix(hostname, ".localhost")
}

func splitNoProxy(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func envNoProxy() []string {
	return mergeNoProxy(splitNoProxy(os.Getenv("NO_PROXY")), splitNoProxy(os.Getenv("no_proxy")))
}

func mergeNoProxy(groups ...[]string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0)
	for _, group := range groups {
		for _, entry := range group {
			key := strings.ToLower(strings.TrimSpace(entry))
			if key == "" {
				continue
			}
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, entry)
		}
	}
	return out
}

func envRefName(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "${") && strings.HasSuffix(raw, "}") {
		return strings.TrimSpace(raw[2 : len(raw)-1])
	}
	if strings.HasPrefix(raw, "$") && !strings.Contains(raw, "://") {
		return strings.TrimSpace(raw[1:])
	}
	return ""
}
