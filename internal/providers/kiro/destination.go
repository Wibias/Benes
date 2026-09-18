package kiro

import (
	"fmt"
	"net/url"
	"strings"
)

const DefaultRegion = "us-east-1"

var ErrInvalidDestination = fmt.Errorf("Kiro destination is not a validated HTTPS runtime origin")

func RuntimeURL(region string) string {
	got, err := allowlistedRuntimeURL(region)
	if err != nil {
		return ""
	}
	return got
}

func NormalizeRegion(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func allowlistedRuntimeURL(region string) (string, error) {
	if NormalizeRegion(region) == "" {
		region = DefaultRegion
	}
	allowed, err := RequireRegion(region)
	if err != nil {
		return "", err
	}
	return "https://runtime." + allowed + ".kiro.dev", nil
}

func ResolveDestination(raw, region string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return allowlistedRuntimeURL(region)
	}
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", ErrInvalidDestination
	}
	if !strings.EqualFold(parsed.Scheme, "https") {
		return "", ErrInvalidDestination
	}
	if port := parsed.Port(); port != "" && port != "443" {
		return "", ErrInvalidDestination
	}
	host := strings.ToLower(parsed.Hostname())
	if !strings.HasPrefix(host, "runtime.") || !strings.HasSuffix(host, ".kiro.dev") {
		return "", ErrInvalidDestination
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(host, "runtime."), ".kiro.dev")
	if _, err := RequireRegion(inner); err != nil {
		return "", ErrInvalidDestination
	}
	return "https://" + host, nil
}

func ManagementURL(region string) (string, error) {
	if NormalizeRegion(region) == "" {
		region = DefaultRegion
	}
	allowed, err := RequireRegion(region)
	if err != nil {
		return "", err
	}
	return "https://management." + allowed + ".kiro.dev/", nil
}

func RegionFromRuntimeURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	if !strings.HasPrefix(host, "runtime.") || !strings.HasSuffix(host, ".kiro.dev") {
		return ""
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(host, "runtime."), ".kiro.dev")
	allowed, err := RequireRegion(inner)
	if err != nil {
		return ""
	}
	return allowed
}
