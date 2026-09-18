package antigravity

import (
	"fmt"
	"net/url"
	"strings"
)

const (
	DailyAPI = "https://daily-cloudcode-pa.googleapis.com"
	ProdAPI  = "https://cloudcode-pa.googleapis.com"
)

var ErrInvalidDestination = fmt.Errorf("Cloud Code Assist destination is not a validated HTTPS peer")

func ResolveDestination(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return DailyAPI, nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", ErrInvalidDestination
	}
	host := strings.ToLower(parsed.Hostname())
	if host != dailyHost() && host != prodHost() {
		return "", ErrInvalidDestination
	}
	if !strings.EqualFold(parsed.Scheme, "https") {
		return peerForHost(host), nil
	}
	if port := parsed.Port(); port != "" && port != "443" {
		return "", ErrInvalidDestination
	}
	return "https://" + host, nil
}

func Peer(destination string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(destination))
	if err != nil {
		return "", false
	}
	switch strings.ToLower(parsed.Hostname()) {
	case dailyHost():
		return ProdAPI, true
	case prodHost():
		return DailyAPI, true
	default:
		return "", false
	}
}

func dailyHost() string { return "daily-cloudcode-pa.googleapis.com" }
func prodHost() string  { return "cloudcode-pa.googleapis.com" }

func peerForHost(host string) string {
	if host == dailyHost() {
		return DailyAPI
	}
	return ProdAPI
}
