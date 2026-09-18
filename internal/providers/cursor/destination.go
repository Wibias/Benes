package cursor

import (
	"fmt"
	"net/url"
	"strings"
)

const DefaultAPI = "https://api2.cursor.sh"

type HTTPVersion string

const (
	HTTPVersion2     HTTPVersion = "2"
	HTTPVersion1Dot1 HTTPVersion = "1.1"
)

var ErrInvalidDestination = fmt.Errorf("Cursor destination is not a validated HTTPS origin")

func ResolveDestination(raw string, version HTTPVersion) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = DefaultAPI
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" || parsed.User != nil {
		return "", ErrInvalidDestination
	}
	if !strings.EqualFold(parsed.Scheme, "https") {
		return "", ErrInvalidDestination
	}
	if version == HTTPVersion1Dot1 && !strings.EqualFold(parsed.Scheme, "https") {
		return "", ErrInvalidDestination
	}
	host := strings.ToLower(parsed.Hostname())
	if port := parsed.Port(); port != "" && port != "443" {
		return "", ErrInvalidDestination
	}
	return "https://" + host, nil
}

func DefaultHTTPVersion(pin string) HTTPVersion {
	if strings.TrimSpace(pin) == "1.1" {
		return HTTPVersion1Dot1
	}
	return HTTPVersion2
}
