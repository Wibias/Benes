package transport

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

type Resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

type DestinationPolicy struct {
	Resolver             Resolver
	AllowPrivateNetwork  bool
	AllowProxyResolution bool
}

type Target struct {
	Scheme             string
	ServerName         string
	Port               string
	Addresses          []netip.Addr
	PrivateNetwork     bool
	ResolutionDeferred bool
}

func (t Target) DialAddress() string {
	if len(t.Addresses) == 0 {
		return ""
	}
	return net.JoinHostPort(t.Addresses[0].String(), t.Port)
}

func (t Target) matches(rawURL *url.URL) bool {
	if rawURL == nil || !strings.EqualFold(rawURL.Scheme, t.Scheme) {
		return false
	}
	if !strings.EqualFold(rawURL.Hostname(), t.ServerName) {
		return false
	}
	return effectivePort(rawURL) == t.Port
}

func ResolveTarget(ctx context.Context, rawURL string, policy DestinationPolicy) (Target, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return Target{}, fmt.Errorf("parse destination: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return Target{}, fmt.Errorf("unsupported destination scheme %q", parsed.Scheme)
	}
	if parsed.User != nil {
		return Target{}, fmt.Errorf("destination URL credentials are not allowed")
	}
	host := parsed.Hostname()
	if host == "" {
		return Target{}, fmt.Errorf("destination host is required")
	}
	port, err := validatedPort(parsed)
	if err != nil {
		return Target{}, err
	}

	var addresses []netip.Addr
	if literal, parseErr := netip.ParseAddr(host); parseErr == nil {
		addresses = []netip.Addr{literal.Unmap()}
	} else {
		resolver := policy.Resolver
		if resolver == nil {
			resolver = net.DefaultResolver
		}
		addresses, err = resolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			var dnsErr *net.DNSError
			if policy.AllowProxyResolution && errors.As(err, &dnsErr) {
				return Target{
					Scheme:             parsed.Scheme,
					ServerName:         host,
					Port:               port,
					ResolutionDeferred: true,
				}, nil
			}
			return Target{}, fmt.Errorf("resolve destination %q: %w", host, err)
		}
	}
	if len(addresses) == 0 {
		return Target{}, fmt.Errorf("resolve destination %q: no addresses", host)
	}

	normalized := make([]netip.Addr, 0, len(addresses))
	privateNetwork := false
	for _, address := range addresses {
		address = address.Unmap()
		if !address.IsValid() || address.IsUnspecified() || address.IsMulticast() {
			return Target{}, fmt.Errorf("destination %q resolved to unusable address %s", host, address)
		}
		if !isPublicAddress(address) {
			privateNetwork = true
			if !policy.AllowPrivateNetwork {
				return Target{}, fmt.Errorf("destination %q resolved to private or reserved address %s", host, address)
			}
		}
		normalized = append(normalized, address)
	}

	return Target{
		Scheme:         parsed.Scheme,
		ServerName:     host,
		Port:           port,
		Addresses:      normalized,
		PrivateNetwork: privateNetwork,
	}, nil
}

func validatedPort(parsed *url.URL) (string, error) {
	port := effectivePort(parsed)
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return "", fmt.Errorf("invalid destination port %q", port)
	}
	return port, nil
}

func effectivePort(parsed *url.URL) string {
	if port := parsed.Port(); port != "" {
		return port
	}
	if parsed.Scheme == "https" {
		return "443"
	}
	return "80"
}

var reservedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001:db8::/32"),
	// IPv4-in-IPv6 translation forms that netip.Unmap does not collapse.
	netip.MustParsePrefix("2001:0::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("::ffff:0:0:0/96"),
}

func isPublicAddress(address netip.Addr) bool {
	address = address.Unmap()
	if !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() {
		return false
	}
	for _, prefix := range reservedPrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}
