package transport

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strings"
	"testing"
)

type staticResolver struct {
	addrs []netip.Addr
	err   error
}

func (r staticResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return r.addrs, r.err
}

func TestResolveTargetRejectsEmbeddedCredentials(t *testing.T) {
	_, err := ResolveTarget(context.Background(), "https://user:secret@example.com/v1", DestinationPolicy{
		Resolver:            staticResolver{addrs: []netip.Addr{netip.MustParseAddr("203.0.113.10")}},
		AllowPrivateNetwork: true,
	})
	if err == nil || !strings.Contains(err.Error(), "credentials") {
		t.Fatalf("ResolveTarget() error = %v, want credential rejection", err)
	}
}

func TestResolveTargetRejectsLoopbackByDefault(t *testing.T) {
	_, err := ResolveTarget(context.Background(), "http://provider.test:8080/v1", DestinationPolicy{
		Resolver: staticResolver{addrs: []netip.Addr{netip.MustParseAddr("127.0.0.1")}},
	})
	if err == nil || !strings.Contains(err.Error(), "private") {
		t.Fatalf("ResolveTarget() error = %v, want private-network rejection", err)
	}
}

func TestResolveTargetRejectsMixedPublicAndPrivateDNSAnswers(t *testing.T) {
	_, err := ResolveTarget(context.Background(), "https://provider.test/v1", DestinationPolicy{
		Resolver: staticResolver{addrs: []netip.Addr{
			netip.MustParseAddr("8.8.8.8"),
			netip.MustParseAddr("127.0.0.1"),
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "private") {
		t.Fatalf("ResolveTarget() error = %v, want mixed-answer rejection", err)
	}
}

func TestResolveTargetAllowsExplicitLoopbackAndPinsPort(t *testing.T) {
	target, err := ResolveTarget(context.Background(), "http://provider.test:4321/v1", DestinationPolicy{
		Resolver:            staticResolver{addrs: []netip.Addr{netip.MustParseAddr("127.0.0.1")}},
		AllowPrivateNetwork: true,
	})
	if err != nil {
		t.Fatalf("ResolveTarget(): %v", err)
	}
	if target.DialAddress() != "127.0.0.1:4321" {
		t.Fatalf("DialAddress() = %q", target.DialAddress())
	}
	if target.ServerName != "provider.test" {
		t.Fatalf("ServerName = %q, want provider.test", target.ServerName)
	}
}

func TestResolveTargetRejectsUnsupportedScheme(t *testing.T) {
	_, err := ResolveTarget(context.Background(), "file:///etc/passwd", DestinationPolicy{})
	if err == nil || !strings.Contains(err.Error(), "scheme") {
		t.Fatalf("ResolveTarget() error = %v, want scheme rejection", err)
	}
}

func TestResolveTargetRejectsThisNetworkRange(t *testing.T) {
	_, err := ResolveTarget(context.Background(), "http://0.1.2.3:8080", DestinationPolicy{})
	if err == nil || !strings.Contains(err.Error(), "private") {
		t.Fatalf("ResolveTarget() error = %v, want special-network rejection", err)
	}
}

func TestResolveTargetDefersTypedDNSFailureOnlyWhenProxyResolutionAllowed(t *testing.T) {
	target, err := ResolveTarget(context.Background(), "https://proxy-only.internal.example/v1", DestinationPolicy{
		Resolver:             staticResolver{err: &net.DNSError{Err: "no such host", Name: "proxy-only.internal.example", IsNotFound: true}},
		AllowProxyResolution: true,
	})
	if err != nil {
		t.Fatalf("ResolveTarget(): %v", err)
	}
	if !target.ResolutionDeferred || len(target.Addresses) != 0 {
		t.Fatalf("target = %#v, want deferred DNS resolution", target)
	}
}

func TestResolveTargetDoesNotDeferUntypedResolverFailure(t *testing.T) {
	_, err := ResolveTarget(context.Background(), "https://provider.test/v1", DestinationPolicy{
		Resolver:             staticResolver{err: errors.New("resolver contract broken")},
		AllowProxyResolution: true,
	})
	if err == nil || !strings.Contains(err.Error(), "resolver contract broken") {
		t.Fatalf("ResolveTarget() error = %v", err)
	}
}

func TestResolveTargetRejectsOutOfRangePort(t *testing.T) {
	_, err := ResolveTarget(context.Background(), "https://provider.test:70000/v1", DestinationPolicy{
		Resolver: staticResolver{addrs: []netip.Addr{netip.MustParseAddr("8.8.8.8")}},
	})
	if err == nil || !strings.Contains(err.Error(), "port") {
		t.Fatalf("ResolveTarget() error = %v, want port rejection", err)
	}
}

func TestReservedIPv6TranslationFormsAreNotPublic(t *testing.T) {
	// These prefixes encode IPv4 inside IPv6. netip.Unmap only collapses
	// ordinary IPv4-mapped addresses, so IsGlobalUnicast can otherwise treat
	// the IPv6 form as a public destination.
	cases := []struct {
		name string
		addr string
	}{
		{name: "nat64-well-known-loopback", addr: "64:ff9b::7f00:1"},
		{name: "nat64-local-private", addr: "64:ff9b:1::c0a8:1"},
		{name: "6to4-loopback", addr: "2002:7f00:1::1"},
		{name: "teredo-prefix", addr: "2001:0:4136:e378:8000:63bf:3fff:fdd2"},
		{name: "ipv4-translated-loopback", addr: "::ffff:0:7f00:1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			addr := netip.MustParseAddr(tc.addr)
			if isPublicAddress(addr) {
				t.Fatalf("%s classified as public after Unmap=%s", addr, addr.Unmap())
			}
			_, err := ResolveTarget(context.Background(), destinationURL(addr), DestinationPolicy{})
			if err == nil || !strings.Contains(err.Error(), "private") {
				t.Fatalf("ResolveTarget(%s) error = %v, want reserved rejection", addr, err)
			}
		})
	}
}

func TestPublicAndMappedDestinationsStayAllowed(t *testing.T) {
	cases := []string{
		"8.8.8.8",
		"2001:4860:4860::8888",
		"::ffff:8.8.8.8",
	}
	for _, raw := range cases {
		addr := netip.MustParseAddr(raw)
		if !isPublicAddress(addr) {
			t.Fatalf("%s should remain public", raw)
		}
		target, err := ResolveTarget(context.Background(), destinationURL(addr), DestinationPolicy{})
		if err != nil {
			t.Fatalf("ResolveTarget(%s): %v", raw, err)
		}
		if target.PrivateNetwork {
			t.Fatalf("ResolveTarget(%s) marked private", raw)
		}
	}
}

func destinationURL(addr netip.Addr) string {
	if addr.Is4() {
		return "https://" + addr.String() + "/v1"
	}
	return "https://[" + addr.String() + "]/v1"
}
